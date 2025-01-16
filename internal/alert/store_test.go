package alert_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisAlertStore(t *testing.T) {
	t.Parallel()

	t.Run("increment and get alert state - no previous state", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient)

		state, err := store.IncrementAndGetAlertState(context.Background(), "tenant_1", "dest_1")
		require.NoError(t, err)

		assert.Equal(t, int64(1), state.FailureCount)
		assert.True(t, state.LastAlertTime.IsZero())
		assert.Equal(t, 0, state.LastAlertLevel)
	})

	t.Run("increment and get alert state - with previous state", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient)

		// Set up initial state
		now := time.Now().UTC()
		err := store.UpdateLastAlert(context.Background(), "tenant_2", "dest_2", now, 50)
		require.NoError(t, err)

		// First increment
		state, err := store.IncrementAndGetAlertState(context.Background(), "tenant_2", "dest_2")
		require.NoError(t, err)
		assert.Equal(t, int64(1), state.FailureCount)
		assert.Equal(t, now.Unix(), state.LastAlertTime.Unix())
		assert.Equal(t, 50, state.LastAlertLevel)

		// Second increment
		state, err = store.IncrementAndGetAlertState(context.Background(), "tenant_2", "dest_2")
		require.NoError(t, err)
		assert.Equal(t, int64(2), state.FailureCount)
		assert.Equal(t, now.Unix(), state.LastAlertTime.Unix())
		assert.Equal(t, 50, state.LastAlertLevel)
	})

	t.Run("reset alert state", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient)

		// Set up initial state with failures
		_, err := store.IncrementAndGetAlertState(context.Background(), "tenant_3", "dest_3")
		require.NoError(t, err)

		// Reset state
		err = store.ResetAlertState(context.Background(), "tenant_3", "dest_3")
		require.NoError(t, err)

		// Verify state is reset
		state, err := store.IncrementAndGetAlertState(context.Background(), "tenant_3", "dest_3")
		require.NoError(t, err)
		assert.Equal(t, int64(1), state.FailureCount) // First increment after reset
	})

	t.Run("update last alert", func(t *testing.T) {
		t.Parallel()
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient)

		// Update alert state
		now := time.Now().UTC()
		err := store.UpdateLastAlert(context.Background(), "tenant_4", "dest_4", now, 66)
		require.NoError(t, err)

		// Verify state was updated
		state, err := store.IncrementAndGetAlertState(context.Background(), "tenant_4", "dest_4")
		require.NoError(t, err)
		assert.Equal(t, now.Unix(), state.LastAlertTime.Unix())
		assert.Equal(t, 66, state.LastAlertLevel)
	})
}
