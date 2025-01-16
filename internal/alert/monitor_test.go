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

func (m *mockAlertStore) IncrementAndGetFailureState(ctx context.Context, tenantID, destinationID string) (alert.FailureState, error) {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Get(0).(alert.FailureState), args.Error(1)
}

func (m *mockAlertStore) ResetFailures(ctx context.Context, tenantID, destinationID string) error {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Error(0)
}

func (m *mockAlertStore) UpdateLastAlertTime(ctx context.Context, tenantID, destinationID string, t time.Time) error {
	args := m.Called(ctx, tenantID, destinationID, t)
	return args.Error(0)
}

type mockAlertEvaluator struct {
	mock.Mock
}

func (m *mockAlertEvaluator) ShouldAlert(failures int64, lastAlertTime time.Time) bool {
	args := m.Called(failures, lastAlertTime)
	return args.Bool(0)
}

func (m *mockAlertEvaluator) GetAlertLevel(failures int64) (int, bool) {
	args := m.Called(failures)
	return args.Int(0), args.Bool(1)
}

type mockAlertNotifier struct {
	mock.Mock
}

func (m *mockAlertNotifier) Notify(ctx context.Context, alert alert.Alert) error {
	args := m.Called(ctx, alert)
	return args.Error(0)
}

type testMonitor struct {
	store     *mockAlertStore
	evaluator *mockAlertEvaluator
	notifier  *mockAlertNotifier
	monitor   alert.AlertMonitor
}

func newTestMonitor(config alert.AlertConfig) *testMonitor {
	store := &mockAlertStore{}
	evaluator := &mockAlertEvaluator{}
	notifier := &mockAlertNotifier{}

	monitor := alert.NewAlertMonitorWithDeps(store, evaluator, notifier, config)

	return &testMonitor{
		store:     store,
		evaluator: evaluator,
		notifier:  notifier,
		monitor:   monitor,
	}
}

func defaultConfig() alert.AlertConfig {
	return alert.AlertConfig{
		DebouncingIntervalMS:    1000,
		AutoDisableFailureCount: 20,
		CallbackURL:             "http://test",
		AlertThresholds:         []int{50, 66, 90, 100},
	}
}

func TestAlertMonitor(t *testing.T) {
	t.Parallel()

	t.Run("success resets failures", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultConfig())

		dest := &models.Destination{ID: "dest_1", TenantID: "tenant_1"}
		attempt := alert.DeliveryAttempt{
			Success:     true,
			Destination: dest,
		}

		tm.store.On("ResetFailures", mock.Anything, dest.TenantID, dest.ID).Return(nil)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		tm.store.AssertExpectations(t)
	})

	t.Run("failure triggers alert", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultConfig())

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

		failureState := alert.FailureState{
			FailureCount:  5,
			LastAlertTime: now.Add(-time.Hour), // Last alert was an hour ago
		}

		tm.store.On("IncrementAndGetFailureState", mock.Anything, dest.TenantID, dest.ID).Return(failureState, nil)
		tm.evaluator.On("ShouldAlert", failureState.FailureCount, failureState.LastAlertTime).Return(true)
		tm.evaluator.On("GetAlertLevel", failureState.FailureCount).Return(50, true)
		tm.notifier.On("Notify", mock.Anything, mock.MatchedBy(func(alert alert.Alert) bool {
			return alert.Topic == event.Topic &&
				alert.ConsecutiveFailures == failureState.FailureCount &&
				alert.Destination == dest &&
				alert.Response == attempt.Response
		})).Return(nil)
		tm.store.On("UpdateLastAlertTime", mock.Anything, dest.TenantID, dest.ID, attempt.Timestamp).Return(nil)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
		tm.notifier.AssertExpectations(t)
	})

	t.Run("failure below threshold", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultConfig())

		dest := &models.Destination{ID: "dest_3", TenantID: "tenant_3"}
		attempt := alert.DeliveryAttempt{
			Success:     false,
			Destination: dest,
		}

		failureState := alert.FailureState{
			FailureCount:  2,
			LastAlertTime: time.Now(),
		}

		tm.store.On("IncrementAndGetFailureState", mock.Anything, dest.TenantID, dest.ID).Return(failureState, nil)
		tm.evaluator.On("ShouldAlert", failureState.FailureCount, failureState.LastAlertTime).Return(false)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		require.NoError(t, err)
		tm.store.AssertExpectations(t)
		tm.evaluator.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultConfig())

		dest := &models.Destination{ID: "dest_4", TenantID: "tenant_4"}
		attempt := alert.DeliveryAttempt{
			Success:     false,
			Destination: dest,
		}

		expectedErr := assert.AnError
		tm.store.On("IncrementAndGetFailureState", mock.Anything, dest.TenantID, dest.ID).Return(alert.FailureState{}, expectedErr)

		err := tm.monitor.HandleAttempt(context.Background(), attempt)
		assert.ErrorIs(t, err, expectedErr)
		tm.store.AssertExpectations(t)
	})

	t.Run("notifier error", func(t *testing.T) {
		t.Parallel()
		tm := newTestMonitor(defaultConfig())

		dest := &models.Destination{ID: "dest_5", TenantID: "tenant_5"}
		event := &models.Event{Topic: "test.event"}
		deliveryEvent := &models.DeliveryEvent{Event: *event}
		attempt := alert.DeliveryAttempt{
			Success:       false,
			DeliveryEvent: deliveryEvent,
			Destination:   dest,
		}

		failureState := alert.FailureState{
			FailureCount:  5,
			LastAlertTime: time.Now().Add(-time.Hour),
		}

		expectedErr := assert.AnError
		tm.store.On("IncrementAndGetFailureState", mock.Anything, dest.TenantID, dest.ID).Return(failureState, nil)
		tm.evaluator.On("ShouldAlert", failureState.FailureCount, failureState.LastAlertTime).Return(true)
		tm.evaluator.On("GetAlertLevel", failureState.FailureCount).Return(50, true)
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

		config := alert.AlertConfig{
			DebouncingIntervalMS:    1000, // 1 second
			AutoDisableFailureCount: 10,
			CallbackURL:             "http://test",
			AlertThresholds:         []int{1, 2, 100},
		}

		monitor := alert.NewAlertMonitorWithDeps(store, alert.NewAlertEvaluator(config), notifier, config)

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
}
