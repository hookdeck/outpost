package alert_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

type mockDestinationDisabler struct {
	mock.Mock
}

func (m *mockDestinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Error(0)
}

func TestAlertMonitor_ConsecutiveFailures_MaxFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	emitter := &mockAlertEmitter{}
	emitter.On("Emit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		alert.WithDisabler(disabler),
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

	// Verify cf alerts at correct thresholds
	cfCalls := countEmitCalls(emitter, "alert.destination.consecutive_failure")
	require.Equal(t, 4, cfCalls, "Should emit 4 cf alerts (50%, 66%, 90%, 100%)")

	// Verify disabled alert emitted once at 100%
	disabledCalls := countEmitCalls(emitter, "alert.destination.disabled")
	require.Equal(t, 1, disabledCalls, "Should emit 1 disabled alert at 100%")

	// Verify disabled alert data
	for _, call := range emitter.Calls {
		if call.Arguments.Get(1) == "alert.destination.disabled" {
			data := call.Arguments.Get(3).(alert.DestinationDisabledData)
			assert.Equal(t, dest.ID, data.Destination.ID)
			assert.Equal(t, "consecutive_failure", data.Reason)
			assert.NotNil(t, data.Destination.DisabledAt)
		}
	}

	// Verify destination was disabled exactly once
	disabler.AssertNumberOfCalls(t, "DisableDestination", 1)
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
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 70, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_above", TenantID: "tenant_above"}
	event := &models.Event{Topic: "test.event"}

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

	// 4 at thresholds + 5 above = 9 cf alerts
	cfCalls := countEmitCalls(emitter, "alert.destination.consecutive_failure")
	require.Equal(t, 9, cfCalls)

	// 6 disabled alerts (failures 20-25)
	disabledCalls := countEmitCalls(emitter, "alert.destination.disabled")
	require.Equal(t, 6, disabledCalls)

	// 6 disable calls
	disabler.AssertNumberOfCalls(t, "DisableDestination", 6)
}

func TestAlertMonitor_NoDisabler(t *testing.T) {
	// Without a disabler, 100% threshold still emits cf alert but no disable/disabled alert
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
		alert.WithAutoDisableFailureCount(10),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_no_disable", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}

	for i := 1; i <= 10; i++ {
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

	cfCalls := countEmitCalls(emitter, "alert.destination.consecutive_failure")
	require.Equal(t, 2, cfCalls, "Should emit cf at 50% and 100%")

	disabledCalls := countEmitCalls(emitter, "alert.destination.disabled")
	require.Equal(t, 0, disabledCalls, "No disabled alert without disabler")
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
