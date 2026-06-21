package alert_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisAlertStore(t *testing.T) {
	t.Parallel()

	t.Run("increment consecutive failures", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		// First increment
		res, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, res.Count)
		assert.True(t, res.NewlyCounted)

		// Second increment (different attempt)
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 2, res.Count)
		assert.True(t, res.NewlyCounted)
	})

	t.Run("reset consecutive failures", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		// Set up initial failures
		res, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, res.Count)

		// Reset failures
		err = store.ResetConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2")
		require.NoError(t, err)

		// Verify counter is reset by incrementing again
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 1, res.Count)
	})

	t.Run("idempotent on replay", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		// First call
		res, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, res.Count)
		assert.True(t, res.NewlyCounted)

		// Replay same attempt — count should not change
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, res.Count, "replaying the same attemptID should not increment the count")
		assert.False(t, res.NewlyCounted, "replayed attemptID should not be newly counted")

		// Different attempt should still increment
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 2, res.Count)
		assert.True(t, res.NewlyCounted)

		// Replay the second attempt — count should not change
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 2, res.Count, "replaying the same attemptID should not increment the count")
		assert.False(t, res.NewlyCounted)
	})

	t.Run("mark attempt evaluated", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		// Counted but not yet evaluated
		res, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_4", "dest_4", "att_1")
		require.NoError(t, err)
		assert.False(t, res.AlreadyEvaluated, "attempt should not be evaluated before MarkAttemptEvaluated")

		// Replay before marking — still not evaluated (partial-failure recovery path)
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_4", "dest_4", "att_1")
		require.NoError(t, err)
		assert.False(t, res.NewlyCounted)
		assert.False(t, res.AlreadyEvaluated)

		// Mark evaluated
		err = store.MarkAttemptEvaluated(context.Background(), "tenant_4", "dest_4", "att_1")
		require.NoError(t, err)

		// Replay after marking — evaluated
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_4", "dest_4", "att_1")
		require.NoError(t, err)
		assert.False(t, res.NewlyCounted)
		assert.True(t, res.AlreadyEvaluated, "attempt should be evaluated after MarkAttemptEvaluated")

		// Other attempts unaffected
		res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_4", "dest_4", "att_2")
		require.NoError(t, err)
		assert.True(t, res.NewlyCounted)
		assert.False(t, res.AlreadyEvaluated)
	})

	t.Run("reset clears evaluated markers", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		_, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_5", "dest_5", "att_1")
		require.NoError(t, err)
		require.NoError(t, store.MarkAttemptEvaluated(context.Background(), "tenant_5", "dest_5", "att_1"))

		err = store.ResetConsecutiveFailureCount(context.Background(), "tenant_5", "dest_5")
		require.NoError(t, err)

		res, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_5", "dest_5", "att_1")
		require.NoError(t, err)
		assert.True(t, res.NewlyCounted)
		assert.False(t, res.AlreadyEvaluated, "reset should clear evaluated markers")
	})
}

func TestRedisAlertStore_WithDeploymentID(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	store := alert.NewRedisAlertStore(redisClient, "dp_test_001")

	// Test increment with deployment ID
	res, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Count)

	// Second increment
	res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, res.Count)

	// Test reset with deployment ID
	err = store.ResetConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
	require.NoError(t, err)

	// Verify counter is reset
	res, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_3")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Count)
}

func TestAlertStoreTenantIsolation(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)

	// A single store (same deployment) shared by two tenants that happen to
	// use the same destination id. The create API accepts a caller-supplied
	// id, so distinct tenants can collide on a destination id like "prod".
	store := alert.NewRedisAlertStore(redisClient, "")

	destinationID := "dest_shared"
	tenantA := "tenant_a"
	tenantB := "tenant_b"

	// Increment for tenant A.
	resA, err := store.IncrementConsecutiveFailureCount(context.Background(), tenantA, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, resA.Count)

	resA, err = store.IncrementConsecutiveFailureCount(context.Background(), tenantA, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, resA.Count)

	// Tenant B uses the same destination id but must have its own counter.
	resB, err := store.IncrementConsecutiveFailureCount(context.Background(), tenantB, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, resB.Count, "tenant B should not inherit tenant A's failure count")

	// Evaluated markers are tenant-scoped too.
	require.NoError(t, store.MarkAttemptEvaluated(context.Background(), tenantA, destinationID, "att_1"))
	resB, err = store.IncrementConsecutiveFailureCount(context.Background(), tenantB, destinationID, "att_1")
	require.NoError(t, err)
	assert.False(t, resB.AlreadyEvaluated, "tenant B should not see tenant A's evaluated marker")

	// Resetting tenant A must not clear tenant B's counter.
	require.NoError(t, store.ResetConsecutiveFailureCount(context.Background(), tenantA, destinationID))

	resA, err = store.IncrementConsecutiveFailureCount(context.Background(), tenantA, destinationID, "att_3")
	require.NoError(t, err)
	assert.Equal(t, 1, resA.Count, "tenant A should be reset")

	resB, err = store.IncrementConsecutiveFailureCount(context.Background(), tenantB, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, resB.Count, "tenant B should be unaffected by tenant A reset")
}

func TestAlertStoreIsolation(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)

	// Create two stores with different deployment IDs
	store1 := alert.NewRedisAlertStore(redisClient, "dp_001")
	store2 := alert.NewRedisAlertStore(redisClient, "dp_002")

	// Use same tenant/destination IDs for both
	tenantID := "tenant_shared"
	destinationID := "dest_shared"

	// Increment in store1
	res1, err := store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, res1.Count)

	res1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, res1.Count)

	// Increment in store2 - should start at 1 (isolated from store1)
	res2, err := store2.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, res2.Count, "Store 2 should have its own counter")

	// Evaluated markers are isolated too
	require.NoError(t, store1.MarkAttemptEvaluated(context.Background(), tenantID, destinationID, "att_1"))
	res2, err = store2.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_1")
	require.NoError(t, err)
	assert.False(t, res2.AlreadyEvaluated, "Store 2 should not see store 1's evaluated marker")

	// Increment store1 again - should continue from 2
	res1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_3")
	require.NoError(t, err)
	assert.Equal(t, 3, res1.Count, "Store 1 counter should be unaffected by store 2")

	// Reset store1 - should not affect store2
	err = store1.ResetConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)

	// Verify store1 is reset
	res1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_4")
	require.NoError(t, err)
	assert.Equal(t, 1, res1.Count, "Store 1 should be reset")

	// Verify store2 is unaffected
	res2, err = store2.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, res2.Count, "Store 2 should be unaffected by store 1 reset")
}
