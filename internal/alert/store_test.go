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
		store := alert.NewRedisAlertStore(redisClient)

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
		store := alert.NewRedisAlertStore(redisClient)

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
