package alert_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertStore(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	store := alert.NewRedisAlertStore(redisClient)
	ctx := context.Background()

	t.Run("increment and get state", func(t *testing.T) {
		t.Parallel()
		tenantID := "tenant_1"
		destID := "dest_1"

		// First increment should return 1 and no last alert
		state, err := store.IncrementAndGetFailureState(ctx, tenantID, destID)
		require.NoError(t, err)
		assert.Equal(t, int64(1), state.FailureCount)
		assert.True(t, state.LastAlertTime.IsZero())

		// Set last alert time
		now := time.Now().UTC()
		err = store.UpdateLastAlertTime(ctx, tenantID, destID, now)
		require.NoError(t, err)

		// Second increment should return 2 and the last alert time
		state, err = store.IncrementAndGetFailureState(ctx, tenantID, destID)
		require.NoError(t, err)
		assert.Equal(t, int64(2), state.FailureCount)
		assert.WithinDuration(t, now, state.LastAlertTime, time.Second)
	})

	t.Run("reset failures", func(t *testing.T) {
		t.Parallel()
		tenantID := "tenant_2"
		destID := "dest_2"

		// Increment a few times
		_, err := store.IncrementAndGetFailureState(ctx, tenantID, destID)
		require.NoError(t, err)
		_, err = store.IncrementAndGetFailureState(ctx, tenantID, destID)
		require.NoError(t, err)

		// Reset should succeed
		err = store.ResetFailures(ctx, tenantID, destID)
		require.NoError(t, err)

		// Next increment should start from 1
		state, err := store.IncrementAndGetFailureState(ctx, tenantID, destID)
		require.NoError(t, err)
		assert.Equal(t, int64(1), state.FailureCount)
	})

	t.Run("multiple destinations", func(t *testing.T) {
		t.Parallel()
		tenantID := "tenant_3"
		dest1 := "dest_3a"
		dest2 := "dest_3b"

		// Increment dest1 twice
		state, err := store.IncrementAndGetFailureState(ctx, tenantID, dest1)
		require.NoError(t, err)
		assert.Equal(t, int64(1), state.FailureCount)

		state, err = store.IncrementAndGetFailureState(ctx, tenantID, dest1)
		require.NoError(t, err)
		assert.Equal(t, int64(2), state.FailureCount)

		// Increment dest2 once - should be independent
		state, err = store.IncrementAndGetFailureState(ctx, tenantID, dest2)
		require.NoError(t, err)
		assert.Equal(t, int64(1), state.FailureCount)
	})

	t.Run("large failure counts", func(t *testing.T) {
		t.Parallel()
		tenantID := "tenant_4"
		destID := "dest_4"

		// Increment many times
		var lastState alert.FailureState
		var err error
		for i := 0; i < 1000; i++ {
			lastState, err = store.IncrementAndGetFailureState(ctx, tenantID, destID)
			require.NoError(t, err)
		}
		assert.Equal(t, int64(1000), lastState.FailureCount)
	})

	t.Run("redis errors", func(t *testing.T) {
		t.Parallel()
		// Create a new miniredis instance
		mr := miniredis.NewMiniRedis()
		require.NoError(t, mr.Start())
		defer mr.Close()

		// Create client connected to miniredis
		client := redis.NewClient(&redis.Options{
			Addr: mr.Addr(),
		})
		store := alert.NewRedisAlertStore(client)

		// Stop miniredis to simulate connection issues
		mr.Close()

		// Operations should fail
		_, err := store.IncrementAndGetFailureState(ctx, "tenant", "dest")
		assert.Error(t, err)

		err = store.ResetFailures(ctx, "tenant", "dest")
		assert.Error(t, err)

		err = store.UpdateLastAlertTime(ctx, "tenant", "dest", time.Now())
		assert.Error(t, err)
	})
}
