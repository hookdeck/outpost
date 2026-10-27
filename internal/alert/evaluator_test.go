package alert_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

func failedAttempt(destID, tenantID, attemptID string) alert.Attempt {
	return alert.Attempt{
		TenantID:      tenantID,
		DestinationID: destID,
		AttemptID:     attemptID,
		Number:        1,
		Success:       false,
	}
}

func successAttempt(destID, tenantID string) alert.Attempt {
	return alert.Attempt{
		TenantID:      tenantID,
		DestinationID: destID,
		AttemptID:     "att_success",
		Number:        1,
		Success:       true,
	}
}

// crossedLevels runs failed attempts (attempt IDs att_<from>..att_<to>) and
// collects the threshold levels crossed, in order.
func crossedLevels(t *testing.T, ctx context.Context, e *alert.Evaluator, destID, tenantID string, from, to int) []int {
	t.Helper()
	var levels []int
	for i := from; i <= to; i++ {
		eval, err := e.Evaluate(ctx, failedAttempt(destID, tenantID, fmt.Sprintf("att_%d", i)))
		require.NoError(t, err)
		if sig := eval.ConsecutiveFailure; sig != nil {
			levels = append(levels, sig.Level)
		}
	}
	return levels
}

func TestEvaluator_ThresholdCrossings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	levels := crossedLevels(t, ctx, e, "dest_1", "tenant_1", 1, 20)
	assert.Equal(t, []int{50, 66, 90, 100}, levels, "each configured threshold crosses exactly once on the way to 100%")
}

func TestEvaluator_AboveMaxKeepsCrossing(t *testing.T) {
	// Past the 100% count, every further failure reports the 100% threshold
	// (>= match), so the caller keeps disabling/alerting.
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 70, 90, 100}),
	)

	levels := crossedLevels(t, ctx, e, "dest_above", "tenant_above", 1, 25)
	assert.Equal(t, []int{50, 70, 90, 100, 100, 100, 100, 100, 100}, levels)
}

func TestEvaluator_CountAndMaxFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(4),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	eval, err := e.Evaluate(ctx, failedAttempt("dest_cm", "tenant_cm", "att_1"))
	require.NoError(t, err)
	assert.Nil(t, eval.ConsecutiveFailure, "1/4 crosses nothing")

	eval, err = e.Evaluate(ctx, failedAttempt("dest_cm", "tenant_cm", "att_2"))
	require.NoError(t, err)
	require.NotNil(t, eval.ConsecutiveFailure, "2/4 = 50%")
	assert.Equal(t, alert.ConsecutiveFailureSignal{Failures: 2, Max: 4, Level: 50}, *eval.ConsecutiveFailure)
}

func TestEvaluator_SuccessResets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(20),
		alert.WithAlertThresholds([]int{50, 66, 90, 100}),
	)

	levels := crossedLevels(t, ctx, e, "dest_reset", "tenant_reset", 1, 14)
	require.Equal(t, []int{50, 66}, levels)

	eval, err := e.Evaluate(ctx, successAttempt("dest_reset", "tenant_reset"))
	require.NoError(t, err)
	assert.Equal(t, alert.Evaluation{}, eval, "success reports nothing")

	// The count restarts: the same thresholds cross again.
	levels = crossedLevels(t, ctx, e, "dest_reset", "tenant_reset", 15, 28)
	assert.Equal(t, []int{50, 66}, levels)
}

func TestEvaluator_ReplayedAttemptDoesNotDoubleCount(t *testing.T) {
	// Counting is idempotent per attempt ID. A replayed attempt reports the
	// same verdict again — replay dedup of downstream effects is the delivery
	// layer's job, not the tracker's.
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(2),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	first, err := e.Evaluate(ctx, failedAttempt("dest_replay", "tenant_replay", "att_1"))
	require.NoError(t, err)
	require.NotNil(t, first.ConsecutiveFailure)
	require.Equal(t, alert.ConsecutiveFailureSignal{Failures: 1, Max: 2, Level: 50}, *first.ConsecutiveFailure)

	replay, err := e.Evaluate(ctx, failedAttempt("dest_replay", "tenant_replay", "att_1"))
	require.NoError(t, err)
	assert.Equal(t, first, replay, "replay reports the same verdict without double-counting")
}

func TestEvaluator_RetriesExhausted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	retryMaxLimit := 3
	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		retryMaxLimit,
		alert.WithAutoDisableFailureCount(100), // high so cf thresholds don't interfere
	)

	// Attempts 1-3: within retry budget.
	for i := 1; i <= 3; i++ {
		a := failedAttempt("dest_er", "tenant_er", fmt.Sprintf("att_%d", i))
		a.Number = i
		a.EligibleForRetry = true
		eval, err := e.Evaluate(ctx, a)
		require.NoError(t, err)
		assert.False(t, eval.RetriesExhausted, "attempt %d is within the retry budget", i)
	}

	// Attempt 4: exceeds retryMaxLimit=3.
	a := failedAttempt("dest_er", "tenant_er", "att_4")
	a.Number = 4
	a.EligibleForRetry = true
	eval, err := e.Evaluate(ctx, a)
	require.NoError(t, err)
	assert.True(t, eval.RetriesExhausted)
}

func TestEvaluator_SignalsEnabled(t *testing.T) {
	t.Parallel()
	off := []alert.Option{alert.WithConsecutiveFailureEnabled(false)}
	assert.False(t, alert.NewEvaluator(nil, 0, off...).SignalsEnabled())
	assert.True(t, alert.NewEvaluator(nil, 3, off...).SignalsEnabled())
	assert.True(t, alert.NewEvaluator(nil, 0, append(off, alert.WithPerAttemptRetryLimits())...).SignalsEnabled())
	assert.False(t, alert.NewEvaluator(nil, 0, append(off, alert.WithPerAttemptRetryLimits(), alert.WithExhaustedRetriesEnabled(false))...).SignalsEnabled())
}

func TestEvaluator_RetriesExhausted_NotEligible(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		3,
		alert.WithAutoDisableFailureCount(100),
	)

	a := failedAttempt("dest_ne", "tenant_ne", "att_4")
	a.Number = 4
	a.EligibleForRetry = false
	eval, err := e.Evaluate(ctx, a)
	require.NoError(t, err)
	assert.False(t, eval.RetriesExhausted, "no exhaustion when the event is not eligible for retry")
}

func TestEvaluator_RetriesExhausted_RetriesDisabled(t *testing.T) {
	// retryMaxLimit=0 means retries are disabled — there is no exhausted state.
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		0,
		alert.WithAutoDisableFailureCount(100),
	)

	a := failedAttempt("dest_rd", "tenant_rd", "att_1")
	a.Number = 5
	a.EligibleForRetry = true
	eval, err := e.Evaluate(ctx, a)
	require.NoError(t, err)
	assert.False(t, eval.RetriesExhausted)
}

func TestEvaluator_ConsecutiveFailure_Disabled(t *testing.T) {
	// With consecutive-failure tracking disabled, failures never count and
	// never cross thresholds.
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(5),
		alert.WithConsecutiveFailureEnabled(false),
	)

	for i := 1; i <= 10; i++ {
		eval, err := e.Evaluate(ctx, failedAttempt("dest_cf_off", "tenant_cf_off", fmt.Sprintf("att_%d", i)))
		require.NoError(t, err)
		assert.Nil(t, eval.ConsecutiveFailure)
	}

	// Success has nothing to reset and reports nothing.
	eval, err := e.Evaluate(ctx, successAttempt("dest_cf_off", "tenant_cf_off"))
	require.NoError(t, err)
	assert.Equal(t, alert.Evaluation{}, eval)
}

func TestEvaluator_ExhaustedRetries_Disabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		3,
		alert.WithAutoDisableFailureCount(100),
		alert.WithExhaustedRetriesEnabled(false),
	)

	a := failedAttempt("dest_er_off", "tenant_er_off", "att_4")
	a.Number = 4
	a.EligibleForRetry = true
	eval, err := e.Evaluate(ctx, a)
	require.NoError(t, err)
	assert.False(t, eval.RetriesExhausted, "no exhaustion signal when disabled")
}

func TestEvaluator_Gates_Independent(t *testing.T) {
	// Consecutive-failure tracking off, exhausted-retries on: exhaustion still
	// fires while the count stays silent.
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		3,
		alert.WithAutoDisableFailureCount(5),
		alert.WithConsecutiveFailureEnabled(false),
		alert.WithExhaustedRetriesEnabled(true),
	)

	var exhausted int
	for i := 1; i <= 6; i++ {
		a := failedAttempt("dest_mix", "tenant_mix", fmt.Sprintf("att_%d", i))
		a.Number = i
		a.EligibleForRetry = true
		eval, err := e.Evaluate(ctx, a)
		require.NoError(t, err)
		assert.Nil(t, eval.ConsecutiveFailure, "consecutive_failure stays silent when its gate is off")
		if eval.RetriesExhausted {
			exhausted++
		}
	}
	assert.Greater(t, exhausted, 0, "exhausted_retries still fires when its gate is on")
}

func TestEvaluator_SkipFailureCount_LeavesStreakUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(4),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	eval, err := e.Evaluate(ctx, failedAttempt("dest_sk", "tenant_sk", "att_1"))
	require.NoError(t, err)
	assert.Nil(t, eval.ConsecutiveFailure)

	for i := 2; i <= 6; i++ {
		a := failedAttempt("dest_sk", "tenant_sk", fmt.Sprintf("att_%d", i))
		a.SkipFailureCount = true
		eval, err = e.Evaluate(ctx, a)
		require.NoError(t, err)
		assert.Nil(t, eval.ConsecutiveFailure, "skipped attempt %d is not counted", i)
	}

	// The next counted failure continues the old streak (2 of 4 = 50%).
	eval, err = e.Evaluate(ctx, failedAttempt("dest_sk", "tenant_sk", "att_7"))
	require.NoError(t, err)
	require.NotNil(t, eval.ConsecutiveFailure)
	assert.Equal(t, 2, eval.ConsecutiveFailure.Failures)
}

func TestEvaluator_RetriesExhausted_NonRetryable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	tests := []struct {
		name     string
		limit    int
		eligible bool
		want     bool
	}{
		{"eligible, first attempt", 10, true, true},
		{"not eligible", 10, false, false},
		{"retries disabled", 0, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := alert.NewEvaluator(
				alert.NewRedisAlertStore(redisClient, ""),
				tc.limit,
				alert.WithAutoDisableFailureCount(100),
			)
			a := failedAttempt("dest_nr", "tenant_nr", "att_1")
			a.EligibleForRetry = tc.eligible
			a.NonRetryable = true
			eval, err := e.Evaluate(ctx, a)
			require.NoError(t, err)
			assert.Equal(t, tc.want, eval.RetriesExhausted)
		})
	}
}

func TestEvaluator_RetriesExhausted_AttemptMaxOverridesLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(redisClient, ""),
		10,
		alert.WithAutoDisableFailureCount(100),
	)

	tests := []struct {
		name       string
		number     int
		attemptMax int
		want       bool
	}{
		{"before the final attempt", 2, 3, false},
		{"final allowed attempt", 3, 3, true},
		{"unset uses evaluator limit", 3, 0, false},
		{"unset final attempt", 11, 0, true},
	}
	for _, tc := range tests {
		a := failedAttempt("dest_am", "tenant_am", "att_"+tc.name)
		a.Number = tc.number
		a.AttemptMax = tc.attemptMax
		a.EligibleForRetry = true
		eval, err := e.Evaluate(ctx, a)
		require.NoError(t, err)
		assert.Equal(t, tc.want, eval.RetriesExhausted, tc.name)
	}
}
