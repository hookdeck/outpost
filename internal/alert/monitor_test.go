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

func (m *mockDestinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Error(0)
}

func TestAlertMonitor_ConsecutiveFailures_MaxFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	notifier := &mockAlertNotifier{}
	notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

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

	// Send 20 consecutive failures (each with a unique attempt ID)
	for i := 1; i <= 20; i++ {
		attempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:           fmt.Sprintf("att_%d", i),
				Status:       "failed",
				Code:         "500",
				ResponseData: map[string]interface{}{"error": "test error"},
				Time:         time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, attempt))
	}

	// Verify consecutive failure notifications were sent at correct thresholds
	var consecutiveFailureCount int
	for _, call := range notifier.Calls {
		if call.Method == "Notify" {
			if cfAlert, ok := call.Arguments.Get(1).(alert.ConsecutiveFailureAlert); ok {
				consecutiveFailureCount++
				require.Contains(t, []int{10, 14, 18, 20}, cfAlert.Data.ConsecutiveFailures, "Alert should be sent at 50%, 66%, 90%, and 100% thresholds")
				require.Equal(t, dest.ID, cfAlert.Data.Destination.ID)
				require.Equal(t, dest.TenantID, cfAlert.Data.Destination.TenantID)
				require.Equal(t, "alert.consecutive_failure", cfAlert.Topic)
				require.Equal(t, 20, cfAlert.Data.MaxConsecutiveFailures)
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
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

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

	// Send 14 failures (should trigger 50% and 66% alerts)
	for i := 1; i <= 14; i++ {
		failedAttempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:           fmt.Sprintf("att_%d", i),
				Status:       "failed",
				Code:         "500",
				ResponseData: map[string]interface{}{"error": "test error"},
				Time:         time.Now(),
			},
		}
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

	// Send 14 more failures (new attempt IDs)
	for i := 15; i <= 28; i++ {
		failedAttempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:           fmt.Sprintf("att_%d", i),
				Status:       "failed",
				Code:         "500",
				ResponseData: map[string]interface{}{"error": "test error"},
				Time:         time.Now(),
			},
		}
		require.NoError(t, monitor.HandleAttempt(ctx, failedAttempt))
	}

	// Verify we got exactly 2 notifications again (50% and 66%)
	require.Equal(t, 2, len(notifier.Calls))

	// Verify the notifications were at the right thresholds
	var seenCounts []int
	for _, call := range notifier.Calls {
		if cfAlert, ok := call.Arguments.Get(1).(alert.ConsecutiveFailureAlert); ok {
			seenCounts = append(seenCounts, cfAlert.Data.ConsecutiveFailures)
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
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

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

	// Send 25 consecutive failures (5 more than the threshold)
	for i := 1; i <= 25; i++ {
		attempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:           fmt.Sprintf("att_%d", i),
				Status:       "failed",
				Code:         "500",
				ResponseData: map[string]interface{}{"error": "test error"},
				Time:         time.Now(),
			},
		}
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
				if cfAlert.Data.ConsecutiveFailures >= 20 {
					disableNotifyCount++
					require.True(t, cfAlert.Data.WillDisable, "WillDisable should be true at and above max")
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
