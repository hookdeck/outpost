package alert_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

type mockAlertEmitter struct {
	mock.Mock
}

func (m *mockAlertEmitter) Emit(ctx context.Context, topic string, tenantID string, data any) error {
	args := m.Called(ctx, topic, tenantID, data)
	return args.Error(0)
}

func TestAlertMonitor_ConsecutiveFailures_MaxFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	emitter := &mockAlertEmitter{}
	emitter.On("Emit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}

	for i := 1; i <= 20; i++ {
		attempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:     fmt.Sprintf("att_%d", i),
				Status: "failed",
				Code:   "500",
				Time:   time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// Verify consecutive failure alerts emitted at correct thresholds
	var cfCount int
	for _, call := range emitter.Calls {
		if call.Arguments.Get(1) == "alert.destination.consecutive_failure" {
			cfCount++
			data := call.Arguments.Get(3).(alert.ConsecutiveFailureData)
			require.Contains(t, []int{10, 14, 18, 20}, data.ConsecutiveFailures.Current)
			require.Equal(t, dest.ID, data.Destination.ID)
			require.Equal(t, 20, data.ConsecutiveFailures.Max)
		}
	}
	require.Equal(t, 4, cfCount, "Should have emitted exactly 4 consecutive failure alerts")
}

func TestAlertMonitor_ConsecutiveFailures_Reset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	emitter := &mockAlertEmitter{}
	emitter.On("Emit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}

	// Send 14 failures (triggers 50% and 66%)
	for i := 1; i <= 14; i++ {
		failedAttempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:     fmt.Sprintf("att_%d", i),
				Status: "failed",
				Code:   "500",
				Time:   time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, failedAttempt))
	}

	cfCalls := countEmitCalls(emitter, "alert.destination.consecutive_failure")
	require.Equal(t, 2, cfCalls)

	// Send a success to reset
	successAttempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt:     &models.Attempt{Status: models.AttemptStatusSuccess},
	}
	require.NoError(t, monitor.HandleAttempt(ctx, successAttempt))

	// Clear calls
	emitter.Calls = nil

	// Send 14 more failures (new IDs)
	for i := 15; i <= 28; i++ {
		failedAttempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:     fmt.Sprintf("att_%d", i),
				Status: "failed",
				Code:   "500",
				Time:   time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, failedAttempt))
	}

	// Should get 2 more cf alerts (50% and 66% again)
	cfCalls = countEmitCalls(emitter, "alert.destination.consecutive_failure")
	require.Equal(t, 2, cfCalls)
}

func TestAlertMonitor_ConsecutiveFailures_AboveThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	emitter := &mockAlertEmitter{}
	emitter.On("Emit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 70, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_above", TenantID: "tenant_above"}
	event := &models.Event{Topic: "test.event"}

	// Send 25 consecutive failures (5 more than threshold)
	for i := 1; i <= 25; i++ {
		attempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:     fmt.Sprintf("att_%d", i),
				Status: "failed",
				Code:   "500",
				Time:   time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// 4 alerts at thresholds (10, 14, 18, 20) + 5 for 21-25 = 9 cf alerts
	cfCalls := countEmitCalls(emitter, "alert.destination.consecutive_failure")
	require.Equal(t, 9, cfCalls)
}

func countEmitCalls(emitter *mockAlertEmitter, topic string) int {
	count := 0
	for _, call := range emitter.Calls {
		if call.Method == "Emit" && call.Arguments.Get(1) == topic {
			count++
		}
	}
	return count
}
