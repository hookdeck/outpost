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

type testMonitor struct {
	store     *mockAlertStore
	evaluator *mockAlertEvaluator
	notifier  *mockAlertNotifier
	disabler  *mockDestinationDisabler
	monitor   alert.AlertMonitor
}

func newTestMonitor(opts ...alert.AlertOption) *testMonitor {
	store := &mockAlertStore{}
	evaluator := &mockAlertEvaluator{}
	notifier := &mockAlertNotifier{}
	disabler := &mockDestinationDisabler{}

	// Create default config
	config := alert.AlertConfig{
		DebouncingIntervalMS:    1000,
		AutoDisableFailureCount: 20,
		CallbackURL:             "http://test",
		AlertThresholds:         []int{50, 66, 90, 100},
	}

	// Apply options
	for _, opt := range opts {
		opt(&config)
	}

	// Create monitor with mocked dependencies
	monitor := alert.NewAlertMonitorWithDeps(store, evaluator, notifier, disabler, config)

	return &testMonitor{
		store:     store,
		evaluator: evaluator,
		notifier:  notifier,
		disabler:  disabler,
		monitor:   monitor,
	}
}

func defaultTestOptions() []alert.AlertOption {
	return []alert.AlertOption{
		alert.WithDebouncingInterval(1000),
		alert.WithAutoDisableFailureCount(20),
		alert.WithCallbackURL("http://test"),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	}
}

func TestAlertMonitor(t *testing.T) {
	t.Parallel()

	t.Run("success resets failures", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultTestOptions()...)

		dest := &models.Destination{ID: "dest_1", TenantID: "tenant_1"}
		attempt := alert.DeliveryAttempt{
			Success:     true,
			Destination: dest,
		}

		tm.store.On("ResetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(nil)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		tm.store.AssertExpectations(t)
	})

	t.Run("failure triggers alert", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultTestOptions()...)

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

		tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true)
		tm.notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.Topic == event.Topic &&
				alert.ConsecutiveFailures == alertState.FailureCount &&
				alert.Destination == dest &&
				alert.Response == attempt.Response
		})).Return(nil)
		tm.store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 50).Return(nil)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
		tm.notifier.AssertExpectations(t)
	})

	t.Run("failure below threshold", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultTestOptions()...)

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

		tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(0, false)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultTestOptions()...)

		dest := &models.Destination{ID: "dest_4", TenantID: "tenant_4"}
		attempt := alert.DeliveryAttempt{
			Success:     false,
			Destination: dest,
		}

		expectedErr := assert.AnError
		tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alert.AlertState{}, expectedErr)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		assert.ErrorIs(t, err, expectedErr)
		tm.store.AssertExpectations(t)
	})

	t.Run("notifier error", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultTestOptions()...)

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
		tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true)
		tm.notifier.On("Notify", mock.Anything, mock.Anything).Return(expectedErr)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		assert.ErrorIs(t, err, expectedErr)
		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
		tm.notifier.AssertExpectations(t)
	})

	t.Run("alert debouncing - suppress alerts within window and trigger after", func(t *testing.T) {
		t.Parallel()

		// Use real Redis and evaluator, only mock notifier
		redisClient := testutil.CreateTestRedisClient(t)
		store := alert.NewRedisAlertStore(redisClient)
		notifier := &mockAlertNotifier{}
		disabler := &mockDestinationDisabler{}

		config := alert.AlertConfig{
			DebouncingIntervalMS:    1000, // 1 second
			AutoDisableFailureCount: 100,  // This means 1% = 1 failure
			CallbackURL:             "http://test",
			AlertThresholds:         []int{1, 2, 100},
		}

		monitor := alert.NewAlertMonitorWithDeps(store, alert.NewAlertEvaluator(config), notifier, disabler, config)

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

		config := alert.AlertConfig{
			DebouncingIntervalMS:    1000, // 1 second
			AutoDisableFailureCount: 100,  // This means 1% = 1 failure
			CallbackURL:             "http://test",
			AlertThresholds:         []int{1, 100}, // Alert at first failure and at disable threshold
		}

		monitor := alert.NewAlertMonitorWithDeps(store, alert.NewAlertEvaluator(config), notifier, disabler, config)

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
		tm := newTestMonitor() // Use no options to test defaults

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
			tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil).Once()

			if i == 5 { // At 50% threshold (5 failures with default AutoDisableFailureCount=10)
				tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true).Once()
				tm.notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
					return alert.ConsecutiveFailures == alertState.FailureCount
				})).Return(nil).Once()
				tm.store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 50).Return(nil).Once()
			} else {
				tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(0, false).Once()
			}
		}

		// Trigger 6 failures
		for i := 0; i < 6; i++ {
			err := tm.monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
		tm.notifier.AssertExpectations(t)
		tm.disabler.AssertExpectations(t)
	})
}

func TestAlertMonitor_AutoDisable(t *testing.T) {
	t.Parallel()

	t.Run("disables destination at 100%", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultTestOptions()...)

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

		tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(100, true)
		tm.notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.ConsecutiveFailures == alertState.FailureCount
		})).Return(nil)
		tm.store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 100).Return(nil)
		tm.disabler.On("DisableDestination", mock.Anything, dest.TenantID, dest.ID).Return(nil)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)

		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
		tm.notifier.AssertExpectations(t)
		tm.disabler.AssertExpectations(t)
	})

	t.Run("handles disable error", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultTestOptions()...)

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
		tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil)
		tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(100, true)
		tm.notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.Topic == event.Topic &&
				alert.ConsecutiveFailures == alertState.FailureCount &&
				alert.Destination == dest &&
				alert.Response == attempt.Response
		})).Return(nil)
		tm.store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 100).Return(nil)
		tm.disabler.On("DisableDestination", mock.Anything, dest.TenantID, dest.ID).Return(expectedErr)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		assert.ErrorIs(t, err, expectedErr)
		assert.ErrorContains(t, err, "failed to disable destination")

		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
		tm.notifier.AssertExpectations(t)
		tm.disabler.AssertExpectations(t)
	})

	t.Run("uses default thresholds when none provided", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor() // Use no options to test defaults

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
			tm.store.On("IncrementAndGetAlertState", mock.Anything, dest.TenantID, dest.ID).Return(alertState, nil).Once()

			if i == 5 { // At 50% threshold (5 failures with default AutoDisableFailureCount=10)
				tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(50, true).Once()
				tm.notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
					return alert.ConsecutiveFailures == alertState.FailureCount
				})).Return(nil).Once()
				tm.store.On("UpdateLastAlert", mock.Anything, dest.TenantID, dest.ID, mock.Anything, 50).Return(nil).Once()
			} else {
				tm.evaluator.On("ShouldAlert", alertState.FailureCount, alertState.LastAlertTime, alertState.LastAlertLevel).Return(0, false).Once()
			}
		}

		// Trigger 6 failures
		for i := 0; i < 6; i++ {
			err := tm.monitor.HandleAttempt(context.Background(), attempt)
			require.NoError(t, err)
		}

		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
		tm.notifier.AssertExpectations(t)
		tm.disabler.AssertExpectations(t)
	})
}
