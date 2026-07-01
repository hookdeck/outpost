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
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

type mockDestinationDisabler struct {
	mock.Mock
}

func (m *mockDestinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	args := m.Called(ctx, tenantID, destinationID)
	return args.Error(0)
}

// evalCommit evaluates one attempt and runs the returned Commit (as the delivery
// layer does), returning the events the monitor decided to deliver. Running
// Commit reproduces the mark-evaluated step that drives the replay short-circuit.
func evalCommit(t *testing.T, ctx context.Context, m alert.AlertMonitor, attempt alert.DeliveryAttempt) []opevents.Event {
	t.Helper()
	eval, err := m.Evaluate(ctx, attempt)
	require.NoError(t, err)
	if eval.Commit != nil {
		require.NoError(t, eval.Commit(ctx))
	}
	return eval.Events
}

// countTopic counts events of a topic in a slice.
func countTopic(events []opevents.Event, topic string) int {
	count := 0
	for _, ev := range events {
		if ev.Topic == topic {
			count++
		}
	}
	return count
}

func TestAlertMonitor_ConsecutiveFailures_MaxFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		10,
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}

	var events []opevents.Event
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
		events = append(events, evalCommit(t, ctx, monitor, attempt)...)
	}

	// cf alerts at 50%, 66%, 90%, 100%.
	require.Equal(t, 4, countTopic(events, opevents.TopicAlertConsecutiveFailure), "Should emit 4 cf alerts")
	// disabled alert emitted once at 100%.
	require.Equal(t, 1, countTopic(events, opevents.TopicAlertDestinationDisabled), "Should emit 1 disabled alert at 100%")

	// Verify disabled alert data.
	for _, ev := range events {
		if ev.Topic == opevents.TopicAlertDestinationDisabled {
			data := ev.Data.(alert.DestinationDisabledData)
			assert.Equal(t, dest.ID, data.Destination.ID)
			assert.Equal(t, "consecutive_failure", data.Reason)
			assert.NotNil(t, data.Destination.DisabledAt)
		}
	}

	// Destination disabled exactly once.
	disabler.AssertNumberOfCalls(t, "DisableDestination", 1)
}

func TestAlertMonitor_ConsecutiveFailures_Reset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		10,
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_1", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}

	// 14 failures (triggers 50% and 66%).
	var events []opevents.Event
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
		events = append(events, evalCommit(t, ctx, monitor, failedAttempt)...)
	}
	require.Equal(t, 2, countTopic(events, opevents.TopicAlertConsecutiveFailure))

	// A success resets the count.
	successAttempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt:     &models.Attempt{Status: models.AttemptStatusSuccess},
	}
	require.Empty(t, evalCommit(t, ctx, monitor, successAttempt), "success emits nothing")

	// 14 more failures (new IDs) trigger 50% and 66% again.
	events = nil
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
		events = append(events, evalCommit(t, ctx, monitor, failedAttempt)...)
	}
	require.Equal(t, 2, countTopic(events, opevents.TopicAlertConsecutiveFailure))
}

func TestAlertMonitor_ConsecutiveFailures_AboveThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		10,
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 70, 90, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_above", TenantID: "tenant_above"}
	event := &models.Event{Topic: "test.event"}

	var events []opevents.Event
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
		events = append(events, evalCommit(t, ctx, monitor, attempt)...)
	}

	// 4 at thresholds + 5 above = 9 cf alerts.
	require.Equal(t, 9, countTopic(events, opevents.TopicAlertConsecutiveFailure))
	// 6 disabled alerts (failures 20-25).
	require.Equal(t, 6, countTopic(events, opevents.TopicAlertDestinationDisabled))
	// 6 disable calls.
	disabler.AssertNumberOfCalls(t, "DisableDestination", 6)
}

func TestAlertMonitor_NoDisabler(t *testing.T) {
	// Without a disabler, 100% threshold still emits cf alert but no disabled alert.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		10,
		alert.WithAutoDisableFailureCount(10),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_no_disable", TenantID: "tenant_1"}
	event := &models.Event{Topic: "test.event"}

	var events []opevents.Event
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
		events = append(events, evalCommit(t, ctx, monitor, attempt)...)
	}

	require.Equal(t, 2, countTopic(events, opevents.TopicAlertConsecutiveFailure), "Should emit cf at 50% and 100%")
	require.Equal(t, 0, countTopic(events, opevents.TopicAlertDestinationDisabled), "No disabled alert without disabler")
}

func TestAlertMonitor_ExhaustedRetries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	retryMaxLimit := 3
	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		retryMaxLimit,
		alert.WithAutoDisableFailureCount(100), // high so cf thresholds don't interfere
	)

	dest := &alert.AlertDestination{ID: "dest_er", TenantID: "tenant_er"}
	event := &models.Event{ID: "evt_er", Topic: "test.event", EligibleForRetry: true}

	// Attempts 1-3: within retry budget, no exhausted_retries.
	var events []opevents.Event
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
		events = append(events, evalCommit(t, ctx, monitor, attempt)...)
	}
	require.Equal(t, 0, countTopic(events, opevents.TopicAlertExhaustedRetries), "No exhausted_retries within retry budget")

	// Attempt 4: exceeds retryMaxLimit=3, should produce exhausted_retries.
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
	exhausted := evalCommit(t, ctx, monitor, attempt)

	require.Equal(t, 1, countTopic(exhausted, opevents.TopicAlertExhaustedRetries), "Should emit exhausted_retries when attempt exceeds retry limit")
	for _, ev := range exhausted {
		if ev.Topic == opevents.TopicAlertExhaustedRetries {
			data := ev.Data.(alert.ExhaustedRetriesData)
			assert.Equal(t, dest.ID, data.Destination.ID)
			assert.Equal(t, dest.TenantID, data.TenantID)
			assert.Equal(t, event.Topic, data.Event.Topic)
			assert.Equal(t, 4, data.Attempt.AttemptNumber)
		}
	}
}

func TestAlertMonitor_ExhaustedRetries_NotEligible(t *testing.T) {
	// Events not eligible for retry should not produce exhausted_retries.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		3,
		alert.WithAutoDisableFailureCount(100),
	)

	dest := &alert.AlertDestination{ID: "dest_ne", TenantID: "tenant_ne"}
	event := &models.Event{Topic: "test.event", EligibleForRetry: false}

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
	events := evalCommit(t, ctx, monitor, attempt)
	require.Equal(t, 0, countTopic(events, opevents.TopicAlertExhaustedRetries), "No exhausted_retries when event not eligible for retry")
}

func TestAlertMonitor_ExhaustedRetries_PerEvent(t *testing.T) {
	// Eval produces one exhausted_retries event per distinct event exhausting on
	// the same destination — no dedup here (suppression is the delivery layer's job).
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		3,
		alert.WithAutoDisableFailureCount(100),
	)

	dest := &alert.AlertDestination{ID: "dest_pe", TenantID: "tenant_pe"}

	var events []opevents.Event
	for i := 1; i <= 2; i++ {
		event := &models.Event{ID: fmt.Sprintf("evt_pe_%d", i), Topic: "test.event", EligibleForRetry: true}
		attempt := alert.DeliveryAttempt{
			Event:       event,
			Destination: dest,
			Attempt: &models.Attempt{
				ID:            fmt.Sprintf("att_pe_%d", i),
				AttemptNumber: 4,
				Status:        "failed",
				Code:          "500",
				Time:          time.Now(),
			},
		}
		events = append(events, evalCommit(t, ctx, monitor, attempt)...)
	}

	require.Equal(t, 2, countTopic(events, opevents.TopicAlertExhaustedRetries), "each distinct event should produce its own exhausted_retries event")
}

func TestAlertMonitor_ReplayedAttempt_SkipsEvaluation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		10,
		alert.WithAutoDisableFailureCount(2),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_replay", TenantID: "tenant_replay"}
	event := &models.Event{Topic: "test.event"}
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:     "att_replay_1",
			Status: "failed",
			Code:   "500",
			Time:   time.Now(),
		},
	}

	// First delivery: count=1 → 50% threshold → cf event, marked evaluated.
	first := evalCommit(t, ctx, monitor, attempt)
	require.Equal(t, 1, countTopic(first, opevents.TopicAlertConsecutiveFailure))

	// Replay (MQ redelivery / producer re-publish): fully evaluated → skipped.
	replay := evalCommit(t, ctx, monitor, attempt)
	assert.Empty(t, replay, "replayed attempt should not re-emit the alert")
}

func TestAlertMonitor_ReplayedAttempt_PartialFailureRetries(t *testing.T) {
	// When delivery fails after the attempt was counted, the caller nacks WITHOUT
	// running Commit — the replay must re-evaluate (re-produce the events), not
	// skip, or alerts would be silently dropped.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		10,
		alert.WithAutoDisableFailureCount(2),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	dest := &alert.AlertDestination{ID: "dest_partial", TenantID: "tenant_partial"}
	event := &models.Event{Topic: "test.event"}
	attempt := alert.DeliveryAttempt{
		Event:       event,
		Destination: dest,
		Attempt: &models.Attempt{
			ID:     "att_partial_1",
			Status: "failed",
			Code:   "500",
			Time:   time.Now(),
		},
	}

	// First delivery: counted, cf produced — but delivery fails, so Commit is NOT run.
	eval1, err := monitor.Evaluate(ctx, attempt)
	require.NoError(t, err)
	require.Equal(t, 1, countTopic(eval1.Events, opevents.TopicAlertConsecutiveFailure))
	// (no Commit — simulates a nacked message)

	// Replay: not marked evaluated → re-runs and re-produces the cf event.
	replay := evalCommit(t, ctx, monitor, attempt)
	require.Equal(t, 1, countTopic(replay, opevents.TopicAlertConsecutiveFailure), "replay after partial failure should re-run evaluation")

	// Second replay: now fully evaluated → skipped.
	assert.Empty(t, evalCommit(t, ctx, monitor, attempt))
}

func TestAlertMonitor_ConsecutiveFailures_GateDisabled(t *testing.T) {
	// With consecutive-failure alerting disabled, failures never produce cf/disabled
	// events and never auto-disable, even past the threshold.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	disabler := &mockDestinationDisabler{}
	disabler.On("DisableDestination", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		10,
		alert.WithDisabler(disabler),
		alert.WithAutoDisableFailureCount(5),
		alert.WithConsecutiveFailureEnabled(false),
	)

	dest := &alert.AlertDestination{ID: "dest_cf_off", TenantID: "tenant_cf_off"}
	event := &models.Event{Topic: "test.event"}

	var events []opevents.Event
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
		events = append(events, evalCommit(t, ctx, monitor, attempt)...)
	}

	require.Equal(t, 0, countTopic(events, opevents.TopicAlertConsecutiveFailure), "no consecutive_failure alerts when gate disabled")
	require.Equal(t, 0, countTopic(events, opevents.TopicAlertDestinationDisabled), "no disabled alerts when gate disabled")
	disabler.AssertNotCalled(t, "DisableDestination", mock.Anything, mock.Anything, mock.Anything)
}

func TestAlertMonitor_ExhaustedRetries_GateDisabled(t *testing.T) {
	// With exhausted-retries alerting disabled, exceeding the retry limit produces
	// nothing, even though retries are enabled and the event is eligible.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		3,
		alert.WithAutoDisableFailureCount(100),
		alert.WithExhaustedRetriesEnabled(false),
	)

	dest := &alert.AlertDestination{ID: "dest_er_off", TenantID: "tenant_er_off"}
	event := &models.Event{Topic: "test.event", EligibleForRetry: true}

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
	events := evalCommit(t, ctx, monitor, attempt)
	require.Equal(t, 0, countTopic(events, opevents.TopicAlertExhaustedRetries), "no exhausted_retries alerts when gate disabled")
}

func TestAlertMonitor_Gates_Independent(t *testing.T) {
	// Consecutive-failure gate off but exhausted-retries gate on: exhausted_retries
	// still fires while consecutive_failure stays silent.
	t.Parallel()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	monitor := alert.NewAlertMonitor(
		logger,
		redisClient,
		3,
		alert.WithAutoDisableFailureCount(5),
		alert.WithConsecutiveFailureEnabled(false),
		alert.WithExhaustedRetriesEnabled(true),
	)

	dest := &alert.AlertDestination{ID: "dest_mix", TenantID: "tenant_mix"}
	event := &models.Event{Topic: "test.event", EligibleForRetry: true}

	var events []opevents.Event
	for i := 1; i <= 6; i++ {
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
		events = append(events, evalCommit(t, ctx, monitor, attempt)...)
	}

	require.Equal(t, 0, countTopic(events, opevents.TopicAlertConsecutiveFailure), "consecutive_failure stays silent when its gate is off")
	require.Greater(t, countTopic(events, opevents.TopicAlertExhaustedRetries), 0, "exhausted_retries still fires when its gate is on")
}
