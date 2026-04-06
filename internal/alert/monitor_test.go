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
	"github.com/hookdeck/outpost/internal/idempotence"
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
		10,
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
		10,
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
		10,
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
		10,
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

func TestAlertMonitor_ExhaustedRetries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	emitter := &mockAlertEmitter{}
	emitter.On("Emit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	retryMaxLimit := 3
	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		retryMaxLimit,
		alert.WithAutoDisableFailureCount(100), // high so cf thresholds don't interfere
	)

	dest := &alert.AlertDestination{ID: "dest_er", TenantID: "tenant_er"}
	event := &models.Event{Topic: "test.event", EligibleForRetry: true}

	// Attempts 1-3: within retry budget, no exhausted_retries
	for i := 1; i <= 3; i++ {
		attempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:            fmt.Sprintf("att_%d", i),
				AttemptNumber: i,
				Status:        "failed",
				Code:          "500",
				Time:          time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	erCalls := countEmitCalls(emitter, "alert.event.exhausted_retries")
	require.Equal(t, 0, erCalls, "No exhausted_retries within retry budget")

	// Attempt 4: exceeds retryMaxLimit=3, should emit exhausted_retries
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:            "att_4",
			AttemptNumber: 4,
			Status:        "failed",
			Code:          "500",
			Time:          time.Now(),
		},
	}
	require.NoError(t, monitor.HandleAttempt(ctx, attempt))

	erCalls = countEmitCalls(emitter, "alert.event.exhausted_retries")
	require.Equal(t, 1, erCalls, "Should emit exhausted_retries when attempt exceeds retry limit")

	// Verify data shape
	for _, call := range emitter.Calls {
		if call.Arguments.Get(1) == "alert.event.exhausted_retries" {
			data := call.Arguments.Get(3).(alert.ExhaustedRetriesData)
			assert.Equal(t, dest.ID, data.Destination.ID)
			assert.Equal(t, dest.TenantID, data.TenantID)
			assert.Equal(t, event.Topic, data.Event.Topic)
			assert.Equal(t, 4, data.Attempt.AttemptNumber)
		}
	}
}

func TestAlertMonitor_ExhaustedRetries_NotEligible(t *testing.T) {
	// Events not eligible for retry should not emit exhausted_retries
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
		3,
		alert.WithAutoDisableFailureCount(100),
	)

	dest := &alert.AlertDestination{ID: "dest_ne", TenantID: "tenant_ne"}
	event := &models.Event{Topic: "test.event", EligibleForRetry: false}

	// Attempt 4 exceeds limit but event is not eligible for retry
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:            "att_4",
			AttemptNumber: 4,
			Status:        "failed",
			Code:          "500",
			Time:          time.Now(),
		},
	}
	require.NoError(t, monitor.HandleAttempt(ctx, attempt))

	erCalls := countEmitCalls(emitter, "alert.event.exhausted_retries")
	require.Equal(t, 0, erCalls, "No exhausted_retries when event not eligible for retry")
}

func TestAlertMonitor_ExhaustedRetries_WindowSuppression(t *testing.T) {
	// With idempotence, only the first exhaustion per destination emits; subsequent are suppressed
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	emitter := &mockAlertEmitter{}
	emitter.On("Emit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	exhaustedIdemp := idempotence.New(redisClient,
		idempotence.WithSuccessfulTTL(10*time.Second),
	)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		3,
		alert.WithAutoDisableFailureCount(100),
		alert.WithExhaustedRetriesIdempotence(exhaustedIdemp),
	)

	dest := &alert.AlertDestination{ID: "dest_ws", TenantID: "tenant_ws"}

	// Two different events both exhaust retries for the same destination
	for i := 1; i <= 2; i++ {
		event := &models.Event{
			ID:               fmt.Sprintf("evt_%d", i),
			Topic:            "test.event",
			EligibleForRetry: true,
		}
		attempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:            fmt.Sprintf("att_exhaust_%d", i),
				AttemptNumber: 4,
				Status:        "failed",
				Code:          "500",
				Time:          time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	erCalls := countEmitCalls(emitter, "alert.event.exhausted_retries")
	require.Equal(t, 1, erCalls, "Only first exhaustion should emit; second suppressed by window")
}

func TestAlertMonitor_ExhaustedRetries_EmitFailureRetryClearsWindow(t *testing.T) {
	// When emit fails, idempotence clears the key so replay can retry
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	emitter := &mockAlertEmitter{}
	// CF alerts always succeed
	emitter.On("Emit", mock.Anything, "alert.destination.consecutive_failure", mock.Anything, mock.Anything).Return(nil)
	// Exhausted retries: fail first, succeed second
	emitter.On("Emit", mock.Anything, "alert.event.exhausted_retries", mock.Anything, mock.Anything).Return(fmt.Errorf("emit failed")).Once()
	emitter.On("Emit", mock.Anything, "alert.event.exhausted_retries", mock.Anything, mock.Anything).Return(nil).Once()

	exhaustedIdemp := idempotence.New(redisClient,
		idempotence.WithSuccessfulTTL(10*time.Second),
	)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		emitter,
		3,
		alert.WithAutoDisableFailureCount(100),
		alert.WithExhaustedRetriesIdempotence(exhaustedIdemp),
	)

	dest := &alert.AlertDestination{ID: "dest_ef", TenantID: "tenant_ef"}
	event := &models.Event{ID: "evt_1", Topic: "test.event", EligibleForRetry: true}

	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:            "att_exhaust_1",
			AttemptNumber: 4,
			Status:        "failed",
			Code:          "500",
			Time:          time.Now(),
		},
	}

	// First call: emit fails, HandleAttempt returns error (entry would be nacked)
	err := monitor.HandleAttempt(ctx, attempt)
	require.Error(t, err, "Should return error when emit fails")

	// Replay: emit succeeds (idempotence key was cleared on failure)
	err = monitor.HandleAttempt(ctx, attempt)
	require.NoError(t, err, "Replay should succeed after emit failure")

	// 2 total calls: 1 failed + 1 succeeded. The key point is that the
	// second call was NOT suppressed — idempotence cleared the key on failure.
	erCalls := countEmitCalls(emitter, "alert.event.exhausted_retries")
	require.Equal(t, 2, erCalls, "Should have 2 emit attempts (1 failed + 1 succeeded)")
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
