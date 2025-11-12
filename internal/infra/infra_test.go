package infra_test

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockInfraProvider implements InfraProvider for testing
type mockInfraProvider struct {
	mu             sync.Mutex
	exists         atomic.Bool
	declareCount   atomic.Int32
	declareCalls   []time.Time
	declareDelay   time.Duration
	existCallCount atomic.Int32
	declareError   error
	existError     error
}

func (m *mockInfraProvider) Exist(ctx context.Context) (bool, error) {
	m.existCallCount.Add(1)
	if m.existError != nil {
		return false, m.existError
	}
	return m.exists.Load(), nil
}

func (m *mockInfraProvider) Declare(ctx context.Context) error {
	m.declareCount.Add(1)

	m.mu.Lock()
	m.declareCalls = append(m.declareCalls, time.Now())
	m.mu.Unlock()

	if m.declareDelay > 0 {
		time.Sleep(m.declareDelay)
	}

	// After declaration, infrastructure exists
	m.exists.Store(true)

	return m.declareError
}

func (m *mockInfraProvider) Teardown(ctx context.Context) error {
	m.exists.Store(false)
	return nil
}

// Helper to create test infra with custom provider
func newTestInfra(t *testing.T, provider infra.InfraProvider, lockKey string, shouldManage bool) *infra.Infra {
	redisConfig := testutil.CreateTestRedisConfig(t)

	ctx := context.Background()
	client, err := redis.New(ctx, redisConfig)
	require.NoError(t, err)

	return newTestInfraWithRedis(t, provider, lockKey, client, shouldManage)
}

// Helper to create test infra with specific Redis client
func newTestInfraWithRedis(t *testing.T, provider infra.InfraProvider, lockKey string, client redis.Cmdable, shouldManage bool) *infra.Infra {
	lock := infra.NewRedisLock(client, infra.LockWithKey(lockKey))
	return infra.NewInfraWithProvider(lock, provider, shouldManage)
}

func TestInfra_SingleNode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := &mockInfraProvider{}
	lockKey := "test:lock:" + idgen.String()

	infra := newTestInfra(t, mockProvider, lockKey, true)

	// Infrastructure doesn't exist initially
	assert.False(t, mockProvider.exists.Load())

	// Declare infrastructure
	err := infra.Declare(ctx)
	require.NoError(t, err)

	// Verify declaration happened exactly once
	assert.Equal(t, int32(1), mockProvider.declareCount.Load())
	assert.True(t, mockProvider.exists.Load())
}

func TestInfra_InfrastructureAlreadyExists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := &mockInfraProvider{}
	mockProvider.exists.Store(true) // Infrastructure already exists
	lockKey := "test:lock:" + idgen.String()

	infra := newTestInfra(t, mockProvider, lockKey, true)

	// Declare should succeed without acquiring lock
	err := infra.Declare(ctx)
	require.NoError(t, err)

	// Verify no declaration happened
	assert.Equal(t, int32(0), mockProvider.declareCount.Load())
	assert.Equal(t, int32(1), mockProvider.existCallCount.Load())
}

func TestInfra_ConcurrentNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := &mockInfraProvider{
		declareDelay: 100 * time.Millisecond, // Simulate slow declaration
	}
	lockKey := "test:lock:" + idgen.String()

	redisConfig := testutil.CreateTestRedisConfig(t)
	client, err := redis.New(ctx, redisConfig)
	require.NoError(t, err)

	const numNodes = 10
	var wg sync.WaitGroup
	errors := make([]error, numNodes)

	// Launch concurrent nodes
	for i := 0; i < numNodes; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Each node gets its own Infra instance but shares the provider and Redis client
			nodeInfra := newTestInfraWithRedis(t, mockProvider, lockKey, client, true)
			errors[idx] = nodeInfra.Declare(ctx)
		}(i)
	}

	wg.Wait()

	// Verify all nodes succeeded
	for i, err := range errors {
		assert.NoError(t, err, "node %d failed", i)
	}

	// Verify only one declaration happened
	assert.Equal(t, int32(1), mockProvider.declareCount.Load())
	assert.True(t, mockProvider.exists.Load())

	// Verify multiple existence checks happened (at least numNodes)
	assert.GreaterOrEqual(t, mockProvider.existCallCount.Load(), int32(numNodes))
}

func TestInfra_LockExpiry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := &mockInfraProvider{}
	lockKey := "test:lock:" + idgen.String()

	mr := miniredis.RunT(t)
	t.Cleanup(func() {
		mr.Close()
	})

	port, _ := strconv.Atoi(mr.Port())
	redisConfig := &redis.RedisConfig{
		Host:     mr.Host(),
		Port:     port,
		Password: "",
		Database: 0,
	}
	client, err := redis.New(ctx, redisConfig)
	require.NoError(t, err)

	// Create and acquire lock with 1 second TTL
	shortLock := infra.NewRedisLock(client,
		infra.LockWithKey(lockKey),
		infra.LockWithTTL(1*time.Second),
	)
	locked, err := shortLock.AttemptLock(ctx)
	require.NoError(t, err)
	require.True(t, locked)

	// Wait for lock to expire (don't unlock it)
	mr.FastForward(1500 * time.Millisecond)

	// Now another node should be able to acquire and declare
	// Use the same Redis client
	nodeInfra := newTestInfraWithRedis(t, mockProvider, lockKey, client, true)
	err = nodeInfra.Declare(ctx)
	require.NoError(t, err)

	// Declaration should have succeeded
	assert.Equal(t, int32(1), mockProvider.declareCount.Load())
}

func TestInfra_Verify_InfrastructureExists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := &mockInfraProvider{}
	mockProvider.exists.Store(true) // Infrastructure exists
	lockKey := "test:lock:" + idgen.String()

	infra := newTestInfra(t, mockProvider, lockKey, false)

	// Verify should succeed when infrastructure exists (shouldManage=false)
	err := infra.Verify(ctx)
	require.NoError(t, err)

	// Verify no declaration happened
	assert.Equal(t, int32(0), mockProvider.declareCount.Load())
	assert.Equal(t, int32(1), mockProvider.existCallCount.Load())
}

func TestInfra_Verify_InfrastructureDoesNotExist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := &mockInfraProvider{}
	mockProvider.exists.Store(false) // Infrastructure does not exist
	lockKey := "test:lock:" + idgen.String()

	infraInstance := newTestInfra(t, mockProvider, lockKey, false)

	// Verify should fail with ErrInfraNotFound when shouldManage=false
	err := infraInstance.Verify(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, infra.ErrInfraNotFound)

	// Verify no declaration happened
	assert.Equal(t, int32(0), mockProvider.declareCount.Load())
	assert.Equal(t, int32(1), mockProvider.existCallCount.Load())
}

func TestInfra_Verify_ExistCheckError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockProvider := &mockInfraProvider{
		existError: assert.AnError,
	}
	lockKey := "test:lock:" + idgen.String()

	infra := newTestInfra(t, mockProvider, lockKey, false)

	// Verify should fail with wrapped error when shouldManage=false
	err := infra.Verify(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to verify infrastructure exists")

	// Verify no declaration happened
	assert.Equal(t, int32(0), mockProvider.declareCount.Load())
}
