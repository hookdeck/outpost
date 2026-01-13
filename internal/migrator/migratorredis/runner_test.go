package migratorredis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/redis"
	r "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMigration is a test migration that can be configured for different scenarios
type mockMigration struct {
	name         string
	version      int
	description  string
	autoRunnable bool

	// Callbacks to track and control behavior
	planCalled   bool
	applyCalled  bool
	verifyCalled bool

	planFunc   func(ctx context.Context) (*Plan, error)
	applyFunc  func(ctx context.Context, plan *Plan) (*State, error)
	verifyFunc func(ctx context.Context, state *State) (*VerificationResult, error)
}

func newMockMigration(name string, version int, autoRunnable bool) *mockMigration {
	return &mockMigration{
		name:         name,
		version:      version,
		description:  "Mock migration: " + name,
		autoRunnable: autoRunnable,
	}
}

func (m *mockMigration) Name() string        { return m.name }
func (m *mockMigration) Version() int        { return m.version }
func (m *mockMigration) Description() string { return m.description }
func (m *mockMigration) AutoRunnable() bool  { return m.autoRunnable }

func (m *mockMigration) Plan(ctx context.Context) (*Plan, error) {
	m.planCalled = true
	if m.planFunc != nil {
		return m.planFunc(ctx)
	}
	return &Plan{
		MigrationName:  m.name,
		Description:    m.description,
		EstimatedItems: 10,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockMigration) Apply(ctx context.Context, plan *Plan) (*State, error) {
	m.applyCalled = true
	if m.applyFunc != nil {
		return m.applyFunc(ctx, plan)
	}
	now := time.Now()
	return &State{
		MigrationName: m.name,
		Phase:         "applied",
		StartedAt:     now,
		CompletedAt:   &now,
		Progress: Progress{
			TotalItems:     plan.EstimatedItems,
			ProcessedItems: plan.EstimatedItems,
		},
	}, nil
}

func (m *mockMigration) Verify(ctx context.Context, state *State) (*VerificationResult, error) {
	m.verifyCalled = true
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, state)
	}
	return &VerificationResult{Valid: true, ChecksRun: 1, ChecksPassed: 1}, nil
}

func (m *mockMigration) PlanCleanup(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockMigration) Cleanup(ctx context.Context, state *State) error {
	return nil
}

// redisTestClient wraps miniredis client to implement our Client interface
type redisTestClient struct {
	*r.Client
}

func (c *redisTestClient) Close() error {
	return c.Client.Close()
}

func (c *redisTestClient) Pipeline() redis.Pipeliner {
	return c.Client.Pipeline()
}

func setupTestRunner(t *testing.T) (*Runner, *miniredis.Miniredis, func()) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := r.NewClient(&r.Options{Addr: mr.Addr()})
	testClient := &redisTestClient{Client: client}

	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)

	runner := NewRunner(testClient, logger)

	cleanup := func() {
		client.Close()
		mr.Close()
	}

	return runner, mr, cleanup
}

// simulateExistingData adds data to Redis to simulate an existing installation
func simulateExistingData(mr *miniredis.Miniredis) {
	mr.Set("outpost:tenant:test123:tenant", "data")
}

func TestRunner_GetPendingMigrations(t *testing.T) {
	t.Run("all migrations pending when none applied", func(t *testing.T) {
		runner, _, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		mig1 := newMockMigration("001_first", 1, true)
		mig2 := newMockMigration("002_second", 2, false)
		mig3 := newMockMigration("003_third", 3, true)
		runner.RegisterMigration(mig1)
		runner.RegisterMigration(mig2)
		runner.RegisterMigration(mig3)

		pending := runner.GetPendingMigrations(ctx)

		assert.Len(t, pending, 3)
	})

	t.Run("returns pending migrations with correct metadata", func(t *testing.T) {
		runner, _, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		mig := newMockMigration("001_test", 1, false)
		mig.description = "Test description"
		runner.RegisterMigration(mig)

		pending := runner.GetPendingMigrations(ctx)

		require.Len(t, pending, 1)
		assert.Equal(t, "001_test", pending[0].Name)
		assert.Equal(t, "Test description", pending[0].Description)
		assert.False(t, pending[0].AutoRunnable)
	})

	t.Run("excludes applied migrations after Run", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		// Simulate existing data so it's not fresh
		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1, true)
		mig2 := newMockMigration("002_second", 2, true)
		runner.RegisterMigration(mig1)
		runner.RegisterMigration(mig2)

		// Run migrations (they're auto-runnable)
		err := runner.Run(ctx)
		require.NoError(t, err)

		// Now check pending - should be empty
		pending := runner.GetPendingMigrations(ctx)
		assert.Empty(t, pending)
	})
}

func TestRunner_Run_FreshInstallation(t *testing.T) {
	t.Run("marks all migrations as applied without running them", func(t *testing.T) {
		runner, _, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		mig1 := newMockMigration("001_first", 1, true)
		mig2 := newMockMigration("002_second", 2, false) // Even non-auto-runnable
		runner.RegisterMigration(mig1)
		runner.RegisterMigration(mig2)

		// Empty Redis = fresh installation
		err := runner.Run(ctx)
		require.NoError(t, err)

		// Migrations should NOT have been executed
		assert.False(t, mig1.applyCalled, "migration should not be applied on fresh install")
		assert.False(t, mig2.applyCalled, "migration should not be applied on fresh install")

		// But they should show as no longer pending
		pending := runner.GetPendingMigrations(ctx)
		assert.Empty(t, pending)
	})
}

func TestRunner_Run_ExistingInstallation(t *testing.T) {
	t.Run("runs auto-runnable migrations", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_auto", 1, true)
		runner.RegisterMigration(mig)

		err := runner.Run(ctx)
		require.NoError(t, err)

		assert.True(t, mig.planCalled, "Plan should be called")
		assert.True(t, mig.applyCalled, "Apply should be called")
	})

	t.Run("returns error for non-auto-runnable pending migrations", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_manual", 1, false)
		runner.RegisterMigration(mig)

		err := runner.Run(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "001_manual")
		assert.Contains(t, err.Error(), "manual run")
		assert.False(t, mig.applyCalled, "migration should not be applied")
	})

	t.Run("runs migrations in version order", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		var executionOrder []string

		mig3 := newMockMigration("003_third", 3, true)
		mig3.applyFunc = func(ctx context.Context, plan *Plan) (*State, error) {
			executionOrder = append(executionOrder, "003_third")
			now := time.Now()
			return &State{CompletedAt: &now}, nil
		}

		mig1 := newMockMigration("001_first", 1, true)
		mig1.applyFunc = func(ctx context.Context, plan *Plan) (*State, error) {
			executionOrder = append(executionOrder, "001_first")
			now := time.Now()
			return &State{CompletedAt: &now}, nil
		}

		mig2 := newMockMigration("002_second", 2, true)
		mig2.applyFunc = func(ctx context.Context, plan *Plan) (*State, error) {
			executionOrder = append(executionOrder, "002_second")
			now := time.Now()
			return &State{CompletedAt: &now}, nil
		}

		// Register in wrong order intentionally
		runner.RegisterMigration(mig3)
		runner.RegisterMigration(mig1)
		runner.RegisterMigration(mig2)

		err := runner.Run(ctx)
		require.NoError(t, err)

		assert.Equal(t, []string{"001_first", "002_second", "003_third"}, executionOrder)
	})

	t.Run("stops on migration failure", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1, true)
		mig1.applyFunc = func(ctx context.Context, plan *Plan) (*State, error) {
			return nil, errors.New("migration failed")
		}

		mig2 := newMockMigration("002_second", 2, true)

		runner.RegisterMigration(mig1)
		runner.RegisterMigration(mig2)

		err := runner.Run(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "001_first")
		assert.False(t, mig2.applyCalled, "second migration should not run after failure")
	})

	t.Run("skips already applied migrations", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1, true)
		mig2 := newMockMigration("002_second", 2, true)
		runner.RegisterMigration(mig1)
		runner.RegisterMigration(mig2)

		// Run first time - both should be applied
		err := runner.Run(ctx)
		require.NoError(t, err)
		assert.True(t, mig1.applyCalled)
		assert.True(t, mig2.applyCalled)

		// Reset flags
		mig1.applyCalled = false
		mig2.applyCalled = false

		// Run second time - neither should be applied
		err = runner.Run(ctx)
		require.NoError(t, err)
		assert.False(t, mig1.applyCalled, "should skip already applied migration")
		assert.False(t, mig2.applyCalled, "should skip already applied migration")
	})
}

func TestRunner_Run_DetectsExistingData(t *testing.T) {
	t.Run("detects outpost:* keys as existing installation", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		mr.Set("outpost:something", "data")

		mig := newMockMigration("001_test", 1, true)
		runner.RegisterMigration(mig)

		err := runner.Run(ctx)
		require.NoError(t, err)

		// Should have run the migration (not fresh install)
		assert.True(t, mig.applyCalled)
	})

	t.Run("detects tenant:* keys as existing installation (legacy)", func(t *testing.T) {
		runner, mr, cleanup := setupTestRunner(t)
		defer cleanup()
		ctx := context.Background()

		mr.Set("tenant:123", "data")

		mig := newMockMigration("001_test", 1, true)
		runner.RegisterMigration(mig)

		err := runner.Run(ctx)
		require.NoError(t, err)

		// Should have run the migration (not fresh install)
		assert.True(t, mig.applyCalled)
	})
}
