package logretention

import (
	"context"
	"errors"
	"testing"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPolicyStore is a test implementation of policyStore.
type mockPolicyStore struct {
	ttl       int
	getErr    error
	setErr    error
	setCalled bool
	setTTL    int
}

func (m *mockPolicyStore) GetAppliedTTL(ctx context.Context) (int, error) {
	if m.getErr != nil {
		return 0, m.getErr
	}
	return m.ttl, nil
}

func (m *mockPolicyStore) SetAppliedTTL(ctx context.Context, ttlDays int) error {
	m.setCalled = true
	m.setTTL = ttlDays
	return m.setErr
}

// mockLogStoreTTL is a test implementation of logStoreTTL.
type mockLogStoreTTL struct {
	applyErr    error
	applyCalled bool
	appliedTTL  int
}

func (m *mockLogStoreTTL) ApplyTTL(ctx context.Context, ttlDays int) error {
	m.applyCalled = true
	m.appliedTTL = ttlDays
	return m.applyErr
}

func testLogger() *logging.Logger {
	l, _ := logging.NewLogger(logging.WithLogLevel("error"))
	return l
}

func TestSync(t *testing.T) {
	ctx := context.Background()

	t.Run("skip when TTL unchanged", func(t *testing.T) {
		ps := &mockPolicyStore{ttl: 30}
		ls := &mockLogStoreTTL{}

		require.NoError(t, sync(ctx, ps, ls, 30, testLogger()))
		assert.False(t, ls.applyCalled)
		assert.False(t, ps.setCalled)
	})

	t.Run("apply and persist when TTL changed", func(t *testing.T) {
		ps := &mockPolicyStore{ttl: 30}
		ls := &mockLogStoreTTL{}

		require.NoError(t, sync(ctx, ps, ls, 7, testLogger()))
		assert.Equal(t, 7, ls.appliedTTL)
		assert.Equal(t, 7, ps.setTTL)
	})

	t.Run("apply on first startup", func(t *testing.T) {
		ps := &mockPolicyStore{ttl: -1}
		ls := &mockLogStoreTTL{}

		require.NoError(t, sync(ctx, ps, ls, 30, testLogger()))
		assert.Equal(t, 30, ls.appliedTTL)
		assert.Equal(t, 30, ps.setTTL)
	})

	t.Run("skip on first startup when TTL is zero", func(t *testing.T) {
		ps := &mockPolicyStore{ttl: -1}
		ls := &mockLogStoreTTL{}

		require.NoError(t, sync(ctx, ps, ls, 0, testLogger()))
		assert.False(t, ls.applyCalled, "no TTL to apply on fresh table")
		assert.False(t, ps.setCalled, "nothing to persist")
	})

	t.Run("remove TTL when set to zero", func(t *testing.T) {
		ps := &mockPolicyStore{ttl: 30}
		ls := &mockLogStoreTTL{}

		require.NoError(t, sync(ctx, ps, ls, 0, testLogger()))
		assert.Equal(t, 0, ls.appliedTTL)
		assert.Equal(t, 0, ps.setTTL)
	})

	t.Run("reject negative TTL", func(t *testing.T) {
		require.Error(t, Apply(ctx, nil, nil, "", -1, testLogger()))
	})

	t.Run("fail when policy store read fails", func(t *testing.T) {
		ps := &mockPolicyStore{getErr: errors.New("redis down")}
		ls := &mockLogStoreTTL{}

		require.Error(t, sync(ctx, ps, ls, 30, testLogger()))
		assert.False(t, ls.applyCalled)
	})

	t.Run("fail when log store apply fails", func(t *testing.T) {
		ps := &mockPolicyStore{ttl: 7}
		ls := &mockLogStoreTTL{applyErr: errors.New("clickhouse down")}

		require.Error(t, sync(ctx, ps, ls, 30, testLogger()))
		assert.False(t, ps.setCalled, "should not persist if CH failed")
	})

	t.Run("fail when policy store write fails", func(t *testing.T) {
		ps := &mockPolicyStore{ttl: 7, setErr: errors.New("redis write failed")}
		ls := &mockLogStoreTTL{}

		require.Error(t, sync(ctx, ps, ls, 30, testLogger()))
		assert.True(t, ls.applyCalled, "CH should have been applied before Redis persist failed")
	})
}
