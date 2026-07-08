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
		count, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Second increment (different attempt)
		count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("reset consecutive failures", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		// Set up initial failures
		count, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Reset failures
		err = store.ResetConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2")
		require.NoError(t, err)

		// Verify counter is reset by incrementing again
		count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("idempotent on replay", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		// First call
		count, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Replay same attempt — count should not change
		count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_1")
		require.NoError(t, err)
		assert.Equal(t, 1, count, "replaying the same attemptID should not increment the count")

		// Different attempt should still increment
		count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// Replay the second attempt — count should not change
		count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_3", "dest_3", "att_2")
		require.NoError(t, err)
		assert.Equal(t, 2, count, "replaying the same attemptID should not increment the count")
	})
}

func TestRedisAlertStore_WithDeploymentID(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	store := alert.NewRedisAlertStore(redisClient, "dp_test_001")

	// Test increment with deployment ID
	count, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Second increment
	count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Test reset with deployment ID
	err = store.ResetConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
	require.NoError(t, err)

	// Verify counter is reset
	count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1", "att_3")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
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
	countA, err := store.IncrementConsecutiveFailureCount(context.Background(), tenantA, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, countA)

	countA, err = store.IncrementConsecutiveFailureCount(context.Background(), tenantA, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, countA)

	// Tenant B uses the same destination id but must have its own counter.
	countB, err := store.IncrementConsecutiveFailureCount(context.Background(), tenantB, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, countB, "tenant B should not inherit tenant A's failure count")

	// Resetting tenant A must not clear tenant B's counter.
	require.NoError(t, store.ResetConsecutiveFailureCount(context.Background(), tenantA, destinationID))

	countA, err = store.IncrementConsecutiveFailureCount(context.Background(), tenantA, destinationID, "att_3")
	require.NoError(t, err)
	assert.Equal(t, 1, countA, "tenant A should be reset")

	countB, err = store.IncrementConsecutiveFailureCount(context.Background(), tenantB, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, countB, "tenant B should be unaffected by tenant A reset")
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
	count1, err := store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, count1)

	count1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, count1)

	// Increment in store2 - should start at 1 (isolated from store1)
	count2, err := store2.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_1")
	require.NoError(t, err)
	assert.Equal(t, 1, count2, "Store 2 should have its own counter")

	// Increment store1 again - should continue from 2
	count1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_3")
	require.NoError(t, err)
	assert.Equal(t, 3, count1, "Store 1 counter should be unaffected by store 2")

	// Reset store1 - should not affect store2
	err = store1.ResetConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)

	// Verify store1 is reset
	count1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_4")
	require.NoError(t, err)
	assert.Equal(t, 1, count1, "Store 1 should be reset")

	// Verify store2 is unaffected
	count2, err = store2.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID, "att_2")
	require.NoError(t, err)
	assert.Equal(t, 2, count2, "Store 2 should be unaffected by store 1 reset")
}
