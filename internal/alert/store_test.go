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
		count, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Second increment
		count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("reset consecutive failures", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient, "")

		// Set up initial failures
		count, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2")
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Reset failures
		err = store.ResetConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2")
		require.NoError(t, err)

		// Verify counter is reset by incrementing again
		count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_2", "dest_2")
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

func TestRedisAlertStore_WithDeploymentID(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	store := alert.NewRedisAlertStore(redisClient, "dp_test_001")

	// Test increment with deployment ID
	count, err := store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Second increment
	count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Test reset with deployment ID
	err = store.ResetConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
	require.NoError(t, err)

	// Verify counter is reset
	count, err = store.IncrementConsecutiveFailureCount(context.Background(), "tenant_1", "dest_1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
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
	count1, err := store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, 1, count1)

	count1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, 2, count1)

	// Increment in store2 - should start at 1 (isolated from store1)
	count2, err := store2.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, 1, count2, "Store 2 should have its own counter")

	// Increment store1 again - should continue from 2
	count1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, 3, count1, "Store 1 counter should be unaffected by store 2")

	// Reset store1 - should not affect store2
	err = store1.ResetConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)

	// Verify store1 is reset
	count1, err = store1.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, 1, count1, "Store 1 should be reset")

	// Verify store2 is unaffected
	count2, err = store2.IncrementConsecutiveFailureCount(context.Background(), tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, 2, count2, "Store 2 should be unaffected by store 1 reset")
}
