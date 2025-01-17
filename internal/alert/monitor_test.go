package alert_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockAlertStore struct {
	mock.Mock
}

func (m *mockAlertStore) IncrementAndGetAlertState(ctx context.Context, tenantID, destinationID string) (alert.AlertState, error) {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Get(0).(alert.AlertState), args.Error(1)
}

func (m *mockAlertStore) ResetAlertState(ctx context.Context, tenantID, destinationID string) error {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Error(0)
}

func (m *mockAlertStore) UpdateLastAlert(ctx context.Context, tenantID, destinationID string, t time.Time, level int) error {
	args := m.Called(ctx, tenantID, destinationID, t, level)
	return args.Error(0)
}

func (m *mockAlertStore) UpdateLastAlertTime(ctx context.Context, tenantID, destinationID string, t time.Time) error {
	args := m.Called(ctx, tenantID, destinationID, t)
	return args.Error(0)
}

func (m *mockAlertStore) UpdateLastAlertLevel(ctx context.Context, tenantID, destinationID string, level int) error {
	args := m.Called(ctx, tenantID, destinationID, level)
	return args.Error(0)
}

type mockAlertEvaluator struct {
	mock.Mock
}

func (m *mockAlertEvaluator) ShouldAlert(failures int64, lastAlertTime time.Time, lastAlertLevel int) (int, bool) {
	args := m.Called(failures, lastAlertTime, lastAlertLevel)
	return args.Int(0), args.Bool(1)
}

func (m *mockAlertEvaluator) GetAlertLevel(failures int64) int {
	args := m.Called(failures)
	return args.Int(0)
}

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

func TestAlertMonitor(t *testing.T) {
	t.Parallel()

	t.Run("success resets failures", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),
			alert.WithAutoDisableFailureCount(20),
			alert.WithAlertThresholds([]int{50, 66, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_1", TenantID: "tenant_1"}
		attempt := alert.DeliveryAttempt{
			Success:     true,
			Destination: dest,
		}

		store.On("ResetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(nil)

		err := monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		store.AssertExpectations(t)
	})

	t.Run("failure triggers alert", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),
			alert.WithAutoDisableFailureCount(20),
			alert.WithAlertThresholds([]int{50, 66, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_2", TenantID: "tenant_2"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		now := time.Now()
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
			Response: &alert.Response{
				Status: "500",
				Data:   map[string]any{"error": "test error"},
			},
			Timestamp: now,
		}

		alertState := alert.AlertState{
			FailureCount:   5,
			LastAlertTime:  now.Add(-time.Hour), // Last alert was an hour ago
			LastAlertLevel: 0,                   // No previous alert level
		}

		store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true)
		notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.Topic == event.Topic &&
				alert.ConsecutiveFailures == alertState.FailureCount &&
				alert.Destination == dest &&
				alert.Response == attempt.Response
		})).Return(nil)
		store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 50).Return(nil)

		err := monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		notifier.AssertExpectations(t)
	})

	t.Run("failure below threshold", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),
			alert.WithAutoDisableFailureCount(20),
			alert.WithAlertThresholds([]int{50, 66, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_3", TenantID: "tenant_3"}
		attempt := alert.DeliveryAttempt{
			Success:     false,
			Destination: dest,
		}

		alertState := alert.AlertState{
			FailureCount:   2,
			LastAlertTime:  time.Now(),
			LastAlertLevel: 0,
		}

		store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(0, false)

		err := monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),
			alert.WithAutoDisableFailureCount(20),
			alert.WithAlertThresholds([]int{50, 66, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_4", TenantID: "tenant_4"}
		attempt := alert.DeliveryAttempt{
			Success:     false,
			Destination: dest,
		}

		expectedErr := assert.AnError
		store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alert.AlertState{}, expectedErr)

		err := monitor.HandleAttempt(context.Background(), attempt)
		assert.ErrorIs(t, err, expectedErr)
		store.AssertExpectations(t)
	})

	t.Run("notifier error", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),
			alert.WithAutoDisableFailureCount(20),
			alert.WithAlertThresholds([]int{50, 66, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_5", TenantID: "tenant_5"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		alertState := alert.AlertState{
			FailureCount:   5,
			LastAlertTime:  time.Now().Add(-time.Hour),
			LastAlertLevel: 0,
		}

		expectedErr := assert.AnError
		store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true)
		notifier.On("Notify", mock.Anything, mock.Anything).Return(expectedErr)

		err := monitor.HandleAttempt(context.Background(), attempt)
		assert.ErrorIs(t, err, expectedErr)
		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		notifier.AssertExpectations(t)
	})

	t.Run("alert debouncing - suppress alerts within window and trigger after", func(t *testing.T) {
		t.Parallel()

		// Use real Redis and evaluator, only mock notifier
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient)
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			redisClient,
			alert.WithStore(store),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),     // 1 second
			alert.WithAutoDisableFailureCount(100), // This means 1% = 1 failure
			alert.WithAlertThresholds([]int{1, 2, 100}),
		)

		dest := &models.Destination{ID: "dest_debounce", TenantID: "tenant_debounce"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		now := time.Now()

		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
			Response: &alert.Response{
				Status: "500",
				Data:   map[string]any{"error": "test error"},
			},
			Timestamp: now,
		}

		// Set up mock expectations
		notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)

		err := monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		// Assert first alert was sent
		notifier.AssertCalled(t, "Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.ConsecutiveFailures == 1
		}))

		// Second failure within debounce window should not trigger alert
		err = monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		// Third failure within debounce window should not trigger alert
		err = monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		// Wait for debounce window to pass
		time.Sleep(1100 * time.Millisecond)

		// Next failure should trigger alert
		attempt.Timestamp = time.Now()
		err = monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		// Verify exactly two alerts were sent with correct failure counts
		require.Equal(t, 2, len(notifier.Calls))
		firstCall := notifier.Calls[0]
		secondCall := notifier.Calls[1]

		alert1 := firstCall.Arguments.Get(1).(alert.Alert)
		alert2 := secondCall.Arguments.Get(1).(alert.Alert)

		assert.Equal(t, int64(1), alert1.ConsecutiveFailures)
		assert.Equal(t, int64(4), alert2.ConsecutiveFailures)
	})

	t.Run("alert reset on success", func(t *testing.T) {
		t.Parallel()

		// Use real Redis and evaluator, only mock notifier
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient)
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			redisClient,
			alert.WithStore(store),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),       // 1 second
			alert.WithAutoDisableFailureCount(100),   // This means 1% = 1 failure
			alert.WithAlertThresholds([]int{1, 100}), // Alert at first failure and at disable threshold
		)

		dest := &models.Destination{ID: "dest_reset", TenantID: "tenant_reset"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}

		failureAttempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
			Response: &alert.Response{
				Status: "500",
				Data:   map[string]any{"error": "test error"},
			},
		}

		successAttempt := alert.DeliveryAttempt{
			Success:     true,
			Destination: dest,
		}

		// Set up mock expectations
		notifier.On("Notify", mock.Anything, mock.Anything).Return(nil)

		// First failure should trigger alert
		err := monitor.HandleAttempt(context.Background(), failureAttempt)
		require.NoError(t, err)

		// Verify first alert
		notifier.AssertCalled(t, "Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.ConsecutiveFailures == 1
		}))

		// Success should reset failure count
		err = monitor.HandleAttempt(context.Background(), successAttempt)
		require.NoError(t, err)

		// Next failure should trigger another alert since count was reset
		err = monitor.HandleAttempt(context.Background(), failureAttempt)
		require.NoError(t, err)

		// Verify exactly two alerts were sent
		require.Equal(t, 2, len(notifier.Calls))
		firstCall := notifier.Calls[0]
		secondCall := notifier.Calls[1]

		alert1 := firstCall.Arguments.Get(1).(alert.Alert)
		alert2 := secondCall.Arguments.Get(1).(alert.Alert)

		// Both alerts should be at count=1 since success reset the counter
		assert.Equal(t, int64(1), alert1.ConsecutiveFailures)
		assert.Equal(t, int64(1), alert2.ConsecutiveFailures)
	})

	t.Run("uses default thresholds when none provided", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithAutoDisableFailureCount(10),
			alert.WithAlertThresholds([]int{50, 70, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_default", TenantID: "tenant_default"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		// Set up expectations for 6 attempts
		for i := 1; i <= 6; i++ {
			alertState := alert.AlertState{
				FailureCount:   int64(i),
				LastAlertTime:  time.Time{},
				LastAlertLevel: 0,
			}
			store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil).Once()

			if i == 5 { // At 50% threshold (5 failures with default AutoDisableFailureCount=10)
				evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true).Once()
				notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
					return alert.ConsecutiveFailures == alertState.FailureCount
				})).Return(nil).Once()
				store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 50).Return(nil).Once()
			} else {
				evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(0, false).Once()
			}
		}

		// Trigger 6 failures
		for i := 0; i < 6; i++ {
			err := monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		notifier.AssertExpectations(t)
		disabler.AssertExpectations(t)
	})

	t.Run("returns no-op monitor when both alert and disable are disabled", func(t *testing.T) {
		t.Parallel()

		// Create monitor with no notifier or disabler
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithAutoDisableFailureCount(10),
			alert.WithAlertThresholds([]int{50, 70, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_noop", TenantID: "tenant_noop"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		// Success attempt should not call store
		successAttempt := alert.DeliveryAttempt{
			Success:     true,
			Destination: dest,
		}
		err := monitor.HandleAttempt(context.Background(), successAttempt)
		require.NoError(t, err)

		// Failure attempt should not trigger any alerts or disables
		err = monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		// Trigger many failures to ensure no side effects at any threshold
		for i := 0; i < 20; i++ {
			err := monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		// Verify no dependencies were called
		store.AssertNotCalled(t, "IncrementAndGetAlertState")
		store.AssertNotCalled(t, "ResetAlertState")
		store.AssertNotCalled(t, "UpdateLastAlert")
		evaluator.AssertNotCalled(t, "ShouldAlert")
		evaluator.AssertNotCalled(t, "GetAlertLevel")
	})
}

func TestAlertMonitor_AutoDisable(t *testing.T) {
	t.Parallel()

	t.Run("disables destination at 100%", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),
			alert.WithAutoDisableFailureCount(20),
			alert.WithAlertThresholds([]int{50, 66, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_auto_disable", TenantID: "tenant_auto_disable"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		now := time.Now()
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
			Response: &alert.Response{
				Status: "500",
				Data:   map[string]any{"error": "test error"},
			},
			Timestamp: now,
		}

		alertState := alert.AlertState{
			FailureCount:   20, // At auto-disable threshold
			LastAlertTime:  now.Add(-time.Hour),
			LastAlertLevel: 90,
		}

		store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(100, true)
		notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.ConsecutiveFailures == alertState.FailureCount
		})).Return(nil)
		store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 100).Return(nil)
		disabler.On("DisableDestination", mock.Anything, dest.TenantID, dest.ID).Return(nil)

		err := monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		notifier.AssertExpectations(t)
		disabler.AssertExpectations(t)
	})

	t.Run("handles disable error", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithDebouncingInterval(1000),
			alert.WithAutoDisableFailureCount(20),
			alert.WithAlertThresholds([]int{50, 66, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_disable_error", TenantID: "tenant_disable_error"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
			Response: &alert.Response{
				Status: "500",
				Data:   map[string]any{"error": "test error"},
			},
			Timestamp: time.Now(),
		}

		alertState := alert.AlertState{
			FailureCount:   20, // At auto-disable threshold
			LastAlertTime:  time.Now().Add(-time.Hour),
			LastAlertLevel: 90,
		}

		expectedErr := assert.AnError
		store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(100, true)
		notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.Topic == event.Topic &&
				alert.ConsecutiveFailures == alertState.FailureCount &&
				alert.Destination == dest &&
				alert.Response == attempt.Response
		})).Return(nil)
		store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 100).Return(nil)
		disabler.On("DisableDestination", mock.Anything, dest.TenantID, dest.ID).Return(expectedErr)

		err := monitor.HandleAttempt(context.Background(), attempt)
		assert.ErrorIs(t, err, expectedErr)
		assert.ErrorContains(t, err, "failed to disable destination")

		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		notifier.AssertExpectations(t)
		disabler.AssertExpectations(t)
	})

	t.Run("uses default thresholds when none provided", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithDisabler(disabler),
			alert.WithAutoDisableFailureCount(10),
			alert.WithAlertThresholds([]int{50, 70, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_default", TenantID: "tenant_default"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		// Set up expectations for 6 attempts
		for i := 1; i <= 6; i++ {
			alertState := alert.AlertState{
				FailureCount:   int64(i),
				LastAlertTime:  time.Time{},
				LastAlertLevel: 0,
			}
			store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil).Once()

			if i == 5 { // At 50% threshold (5 failures with default AutoDisableFailureCount=10)
				evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true).Once()
				notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
					return alert.ConsecutiveFailures == alertState.FailureCount
				})).Return(nil).Once()
				store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 50).Return(nil).Once()
			} else {
				evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(0, false).Once()
			}
		}

		// Trigger 6 failures
		for i := 0; i < 6; i++ {
			err := monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		notifier.AssertExpectations(t)
		disabler.AssertExpectations(t)
	})
}

func TestAlertMonitor_NoOp(t *testing.T) {
	t.Parallel()

	t.Run("returns no-op monitor when both alert and disable are disabled", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithAutoDisableFailureCount(10),
			alert.WithAlertThresholds([]int{50, 70, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_noop", TenantID: "tenant_noop"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		// Success attempt should not call store
		successAttempt := alert.DeliveryAttempt{
			Success:     true,
			Destination: dest,
		}
		err := monitor.HandleAttempt(context.Background(), successAttempt)
		require.NoError(t, err)

		// Failure attempt should not trigger any alerts or disables
		err = monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		// Trigger many failures to ensure no side effects at any threshold
		for i := 0; i < 20; i++ {
			err := monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		// Verify no dependencies were called
		store.AssertNotCalled(t, "IncrementAndGetAlertState")
		store.AssertNotCalled(t, "ResetAlertState")
		store.AssertNotCalled(t, "UpdateLastAlert")
		evaluator.AssertNotCalled(t, "ShouldAlert")
		evaluator.AssertNotCalled(t, "GetAlertLevel")
	})

	t.Run("works with only disabler", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		disabler := &mockDestinationDisabler{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithDisabler(disabler),
			alert.WithAutoDisableFailureCount(10),
			alert.WithAlertThresholds([]int{50, 70, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_no_alert", TenantID: "tenant_no_alert"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		// Set up expectations for 11 attempts (10 to reach threshold + 1 to trigger disable)
		for i := 1; i <= 11; i++ {
			alertState := alert.AlertState{
				FailureCount:   int64(i),
				LastAlertTime:  time.Time{},
				LastAlertLevel: 0,
			}
			store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil).Once()
			evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(i*10, true).Once()
		}

		// Last attempt should trigger disable
		disabler.On("DisableDestination", mock.Anything, dest.TenantID, dest.ID).Return(nil).Once()

		// Trigger enough failures to hit 100% threshold
		for i := 0; i < 11; i++ {
			err := monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		disabler.AssertExpectations(t)
	})

	t.Run("works with only notifier", func(t *testing.T) {
		t.Parallel()
		store := &mockAlertStore{}
		evaluator := &mockAlertEvaluator{}
		notifier := &mockAlertNotifier{}

		monitor := alert.NewAlertMonitor(
			nil,
			alert.WithStore(store),
			alert.WithEvaluator(evaluator),
			alert.WithNotifier(notifier),
			alert.WithAutoDisableFailureCount(10),
			alert.WithAlertThresholds([]int{50, 70, 90, 100}),
		)

		dest := &models.Destination{ID: "dest_no_disable", TenantID: "tenant_no_disable"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		// Set up expectations for 10 attempts
		for i := 1; i <= 10; i++ {
			alertState := alert.AlertState{
				FailureCount:   int64(i),
				LastAlertTime:  time.Time{},
				LastAlertLevel: 0,
			}
			store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil).Once()
			evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(i*10, true).Once()

			// Each failure should trigger an alert
			notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
				return alert.ConsecutiveFailures == int64(i)
			})).Return(nil).Once()
			store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, i*10).Return(nil).Once()
		}

		// Trigger enough failures to hit 100% threshold
		for i := 0; i < 10; i++ {
			err := monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		store.AssertExpectations(t)
		evaluator.AssertExpectations(t)
		notifier.AssertExpectations(t)
	})
}
