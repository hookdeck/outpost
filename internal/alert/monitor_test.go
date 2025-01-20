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
	m.Called(ctx, alert)
	return nil
}

type mockDestinationDisabler struct {
	mock.Mock
}

func (m *mockDestinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	m.Called(ctx, tenantID, destinationID)
	return nil
}

func TestAlertMonitor_ConsecutiveFailures_MaxFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		redisClient,
		alert.WithNotifier(notifier),
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}), // use 66% to test rounding logic
	)

	dest := &models.Destination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}
	deliveryEvent := &models.DeliveryEvent{Event: *event}
	attempt := alert.DeliveryAttempt{
		Success:       false,
		DeliveryEvent: deliveryEvent,
		Destination:   dest,
		Data: map[string]interface{}{
			"status": "500",
			"data":   map[string]any{"error": "test error"},
		},
		Timestamp: time.Now(),
	}

	// Send 20 consecutive failures
	for i := 1; i <= 20; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// Verify notifications were sent at correct thresholds
	var notifyCallCount int
	for _, call := range notifier.Calls {
		if call.Method == "Notify" {
			notifyCallCount++
			alert := call.Arguments.Get(1).(alert.Alert)
			failures := alert.ConsecutiveFailures
			require.Contains(t, []int{10, 14, 18, 20}, failures, "Alert should be sent at 50%, 66%, 90%, and 100% thresholds")
			require.Equal(t, dest, alert.Destination)
			require.Equal(t, event.Topic, alert.Topic)
			require.Equal(t, attempt.Data, alert.Data)
			require.Equal(t, 20, alert.DisableThreshold)
		}
	}
	require.Equal(t, 4, notifyCallCount, "Should have sent exactly 4 notifications")

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
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		redisClient,
		alert.WithNotifier(notifier),
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	dest := &models.Destination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}
	deliveryEvent := &models.DeliveryEvent{Event: *event}
	failedAttempt := alert.DeliveryAttempt{
		Success:       false,
		DeliveryEvent: deliveryEvent,
		Destination:   dest,
		Data: map[string]interface{}{
			"status": "500",
			"data":   map[string]any{"error": "test error"},
		},
		Timestamp: time.Now(),
	}

	// Send 14 failures (should trigger 50% and 66% alerts)
	for i := 1; i <= 14; i++ {
		require.NoError(t, monitor.HandleAttempt(ctx, failedAttempt))
	}

	// Verify we got exactly 2 notifications (50% and 66%)
	require.Equal(t, 2, len(notifier.Calls))

	// Send a success to reset the counter
	successAttempt := failedAttempt
	successAttempt.Success = true
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
		alert := call.Arguments.Get(1).(alert.Alert)
		seenCounts = append(seenCounts, alert.ConsecutiveFailures)
	}
	assert.Contains(t, seenCounts, 10, "Should have alerted at 50% (10 failures)")
	assert.Contains(t, seenCounts, 14, "Should have alerted at 66% (14 failures)")

	// Verify the destination was never disabled
	disabler.AssertNotCalled(t, "DisableDestination")
}
