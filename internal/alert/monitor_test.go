package alert_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

type mockAlertNotifier struct {
	mock.Mock
}

func (m *mockAlertNotifier) Notify(ctx context.Context, alert alert.Alert) error {
	args := m.Called(ctx, alert)
	return args.Error(0)
}

type mockDestinationDisabler struct {
	mock.Mock
}

func (m *mockDestinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) (models.Destination, error) {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Get(0).(models.Destination), args.Error(1)
}

func TestAlertMonitor_ConsecutiveFailures_MaxFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)
	disabledAt := time.Now()
	disabledDest := models.Destination{
		ID:         "dest_1",
		TenantID:   "tenant_1",
		DisabledAt: &disabledAt,
	}
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(disabledDest, nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		alert.WithNotifier(notifier),
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}), // use 66% to test rounding logic
	)

	dest := &alert.AlertDestination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:           "attempt_1",
			Status:       "failed",
			Code:         "500",
			ResponseData: map[string]interface{}{"error": "test error"},
			Time:         time.Now(),
		},
	}

	// Send 20 consecutive failures
	for i := 1; i <= 20; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// Verify consecutive failure notifications were sent at correct thresholds
	var consecutiveFailureCount int
	for _, call := range notifier.Calls {
		if call.Method == "Notify" {
			if cfAlert, ok := call.Arguments.Get(1).(alert.ConsecutiveFailureAlert); ok {
				consecutiveFailureCount++
				cf := cfAlert.Data.ConsecutiveFailures
				require.Contains(t, []int{10, 14, 18, 20}, cf.Current, "Alert should be sent at 50%, 66%, 90%, and 100% thresholds")
				require.Equal(t, dest.ID, cfAlert.Data.Destination.ID)
				require.Equal(t, dest.TenantID, cfAlert.Data.Destination.TenantID)
				if cf.Threshold == 100 {
					require.NotNil(t, cfAlert.Data.Destination.DisabledAt, "Destination should have DisabledAt at threshold 100")
				} else {
					require.Nil(t, cfAlert.Data.Destination.DisabledAt, "Destination should not have DisabledAt below threshold 100")
				}
				require.Equal(t, "alert.destination.consecutive_failure", cfAlert.Topic)
				require.Equal(t, "attempt_1", cfAlert.Data.Attempt.ID)
				require.Equal(t, 20, cf.Max)
			}
		}
	}
	require.Equal(t, 4, consecutiveFailureCount, "Should have sent exactly 4 consecutive failure notifications")

	// Verify destination was disabled exactly once at 100%
	var disableCallCount int
	for _, call := range disabler.Calls {
		if call.Method == "DisableDestination" {
			disableCallCount++
			require.Equal(t, dest.TenantID, call.Arguments.Get(1))
			require.Equal(t, dest.ID, call.Arguments.Get(2))
		}
	}
	require.Equal(t, 1, disableCallCount, "Should have disabled destination exactly once")
}

func TestAlertMonitor_ConsecutiveFailures_Reset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)
	disabledAt := time.Now()
	disabledDest := models.Destination{
		ID:         "dest_1",
		TenantID:   "tenant_1",
		DisabledAt: &disabledAt,
	}
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(disabledDest, nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		alert.WithNotifier(notifier),
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}
	failedAttempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:           "attempt_reset",
			Status:       "failed",
			Code:         "500",
			ResponseData: map[string]interface{}{"error": "test error"},
			Time:         time.Now(),
		},
	}

	// Send 14 failures (should trigger 50% and 66% alerts)
	for i := 1; i <= 14; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, failedAttempt))
	}

	// Verify we got exactly 2 notifications (50% and 66%)
	require.Equal(t, 2, len(notifier.Calls))

	// Send a success to reset the counter
	successAttempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt:     &models.Attempt{Status: models.AttemptStatusSuccess},
	}
	require.NoError(t, monitor.HandleAttempt(ctx, successAttempt))

	// Clear the mock calls to start fresh
	notifier.Calls = nil

	// Send 14 more failures
	for i := 1; i <= 14; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, failedAttempt))
	}

	// Verify we got exactly 2 notifications again (50% and 66%)
	require.Equal(t, 2, len(notifier.Calls))

	// Verify the notifications were at the right thresholds
	var seenCounts []int
	for _, call := range notifier.Calls {
		if cfAlert, ok := call.Arguments.Get(1).(alert.ConsecutiveFailureAlert); ok {
			seenCounts = append(seenCounts, cfAlert.Data.ConsecutiveFailures.Current)
		}
	}
	assert.Contains(t, seenCounts, 10, "Should have alerted at 50% (10 failures)")
	assert.Contains(t, seenCounts, 14, "Should have alerted at 66% (14 failures)")

	// Verify the destination was never disabled
	disabler.AssertNotCalled(t, "DisableDestination")
}

func TestAlertMonitor_ConsecutiveFailures_AboveThreshold(t *testing.T) {
	// Tests that failures above the 100% threshold still trigger disable.
	// This ensures we don't miss the disable if concurrent processing
	// causes us to skip over the exact threshold count.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)
	disabledAt := time.Now()
	disabledDest := models.Destination{
		ID:         "dest_above",
		TenantID:   "tenant_above",
		DisabledAt: &disabledAt,
	}
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(disabledDest, nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		alert.WithNotifier(notifier),
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 70, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_above", TenantID: "tenant_above"}
	event := &models.Event{Topic: "test.event"}
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:           "attempt_above",
			Status:       "failed",
			Code:         "500",
			ResponseData: map[string]interface{}{"error": "test error"},
			Time:         time.Now(),
		},
	}

	// Send 25 consecutive failures (5 more than the threshold)
	for i := 1; i <= 25; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// Verify consecutive failure notifications at 50%, 70%, 90%, and 100% thresholds
	// Plus additional notifications for failures 21-25 (all at 100% level)
	var consecutiveFailureCount int
	var disableNotifyCount int
	for _, call := range notifier.Calls {
		if call.Method == "Notify" {
			if cfAlert, ok := call.Arguments.Get(1).(alert.ConsecutiveFailureAlert); ok {
				consecutiveFailureCount++
				if cfAlert.Data.ConsecutiveFailures.Current >= 20 {
					disableNotifyCount++
					require.Equal(t, 100, cfAlert.Data.ConsecutiveFailures.Threshold, "Threshold should be 100 at and above max")
				}
			}
		}
	}
	// 4 alerts at thresholds (10, 14, 18, 20) + 5 alerts for 21-25
	require.Equal(t, 9, consecutiveFailureCount, "Should have sent 9 consecutive failure notifications (4 at thresholds + 5 above)")
	require.Equal(t, 6, disableNotifyCount, "Should have 6 notifications at threshold 100 (20-25)")

	// Verify destination was disabled multiple times (once per failure >= 20)
	var disableCallCount int
	for _, call := range disabler.Calls {
		if call.Method == "DisableDestination" {
			disableCallCount++
		}
	}
	require.Equal(t, 6, disableCallCount, "Should have called disable 6 times (for failures 20-25)")
}

func TestAlertMonitor_SendsDestinationDisabledAlert(t *testing.T) {
	// This test verifies that when a destination is auto-disabled after reaching
	// the consecutive failure threshold, a DestinationDisabledAlert is sent via
	// the notifier with topic "alert.destination.disabled".
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)

	// Create a destination that will be returned by the disabler
	disabledAt := time.Now()
	modelsDest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_disabled_test"),
		testutil.DestinationFactory.WithTenantID("tenant_disabled_test"),
	)
	modelsDest.DisabledAt = &disabledAt
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(modelsDest, nil)

	autoDisableCount := 5
	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		alert.WithNotifier(notifier),
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(autoDisableCount),
		alert.WithAlertThresholds([]int{100}), // Only alert at 100% to simplify test
	)

	dest := alert.AlertDestinationFromDestination(&modelsDest)
	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("event_123"),
		testutil.EventFactory.WithTopic("test.event"),
	)
	testAttempt := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithID("attempt_disabled"),
		testutil.AttemptFactory.WithStatus("failed"),
		testutil.AttemptFactory.WithCode("500"),
	)
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt:     testAttempt,
	}

	// Send exactly autoDisableCount failures to trigger auto-disable
	for i := 1; i <= autoDisableCount; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// Verify destination was disabled
	disabler.AssertCalled(t, "DisableDestination", mock.Anything, dest.TenantID, dest.ID)

	// Find the DestinationDisabledAlert in the notifier calls
	var foundDestinationDisabledAlert bool
	var destinationDisabledAlert alert.DestinationDisabledAlert
	for _, call := range notifier.Calls {
		if call.Method == "Notify" {
			alertArg := call.Arguments.Get(1)
			if disabledAlert, ok := alertArg.(alert.DestinationDisabledAlert); ok {
				foundDestinationDisabledAlert = true
				destinationDisabledAlert = disabledAlert
				break
			}
		}
	}

	require.True(t, foundDestinationDisabledAlert, "Expected DestinationDisabledAlert to be sent when destination is disabled")

	// Verify the alert topic
	assert.Equal(t, "alert.destination.disabled", destinationDisabledAlert.Topic, "Alert should have topic 'alert.destination.disabled'")

	// Verify the alert data
	assert.Equal(t, dest.TenantID, destinationDisabledAlert.Data.TenantID, "TenantID should match")
	assert.Equal(t, dest.ID, destinationDisabledAlert.Data.Destination.ID, "Destination ID should match")
	assert.Equal(t, dest.TenantID, destinationDisabledAlert.Data.Destination.TenantID, "Destination TenantID should match")
	assert.NotNil(t, destinationDisabledAlert.Data.Destination.DisabledAt, "Destination DisabledAt should be set")
	assert.False(t, destinationDisabledAlert.Data.DisabledAt.IsZero(), "DisabledAt should be set")
	// Verify the alert's DisabledAt matches the destination's DisabledAt exactly
	assert.Equal(t, disabledAt, destinationDisabledAlert.Data.DisabledAt, "Alert DisabledAt should match destination's DisabledAt exactly")
	assert.Equal(t, disabledAt, *destinationDisabledAlert.Data.Destination.DisabledAt, "Alert Destination.DisabledAt should match destination's DisabledAt exactly")
	assert.Equal(t, "consecutive_failure", destinationDisabledAlert.Data.Reason, "Reason should be consecutive_failure")

	// Verify the attempt is included
	require.NotNil(t, destinationDisabledAlert.Data.Attempt, "Attempt should be set")
	assert.Equal(t, testAttempt.ID, destinationDisabledAlert.Data.Attempt.ID, "Attempt ID should match")

	// Verify the event is included
	require.NotNil(t, destinationDisabledAlert.Data.Event, "Event should be set")
	assert.Equal(t, event.ID, destinationDisabledAlert.Data.Event.ID, "Event ID should match")
	assert.Equal(t, event.Topic, destinationDisabledAlert.Data.Event.Topic, "Event Topic should match")
}

func TestAlertMonitor_ConsecutiveFailureAlert_ReflectsDisabledDestination(t *testing.T) {
	// Tests that the consecutive failure alert at threshold 100 includes
	// the destination with DisabledAt set, reflecting the post-disable state.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)

	disabledAt := time.Now()
	modelsDest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_reflect"),
		testutil.DestinationFactory.WithTenantID("tenant_reflect"),
	)
	modelsDest.DisabledAt = &disabledAt
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(modelsDest, nil)

	autoDisableCount := 5
	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		alert.WithNotifier(notifier),
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(autoDisableCount),
		alert.WithAlertThresholds([]int{100}),
	)

	dest := &alert.AlertDestination{ID: "dest_reflect", TenantID: "tenant_reflect"}
	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("event_reflect"),
	)
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID("attempt_reflect"),
			testutil.AttemptFactory.WithStatus("failed"),
			testutil.AttemptFactory.WithCode("500"),
		),
	}

	for i := 1; i <= autoDisableCount; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// Find the consecutive failure alert at threshold 100
	var found bool
	for _, call := range notifier.Calls {
		if call.Method == "Notify" {
			if cfAlert, ok := call.Arguments.Get(1).(alert.ConsecutiveFailureAlert); ok {
				if cfAlert.Data.ConsecutiveFailures.Threshold == 100 {
					found = true
					// The destination in the alert should reflect the disabled state
					require.NotNil(t, cfAlert.Data.Destination.DisabledAt, "Destination in consecutive failure alert at threshold 100 should have DisabledAt set")
					assert.Equal(t, disabledAt, *cfAlert.Data.Destination.DisabledAt, "DisabledAt should match")
					break
				}
			}
		}
	}
	require.True(t, found, "Should have found a consecutive failure alert at threshold 100")
}
