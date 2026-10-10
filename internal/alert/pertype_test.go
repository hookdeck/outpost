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

// An attempt's own MaxRetries replaces the evaluator default for the
// exhausted-retries signal: with a default of 10, an MCP-like type with 3
// retries exhausts on attempt 4.
func TestEvaluator_RetriesExhausted_PerAttemptMax(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(testutil.CreateTestRedisClient(t), ""),
		10,
		alert.WithAutoDisableFailureCount(100),
	)

	for _, tc := range []struct {
		name       string
		maxRetries int
		number     int
		want       bool
	}{
		{"own max: within budget", 3, 3, false},
		{"own max: final attempt exhausts", 3, 4, true},
		{"own max above the default", 20, 11, false},
		{"zero falls back to the default: within", 0, 4, false},
		{"zero falls back to the default: exhausted", 0, 11, true},
		{"negative: no retries, never exhausted", -1, 50, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := failedAttempt("dest_pt", "tenant_pt", fmt.Sprintf("att_%s", tc.name))
			a.Number = tc.number
			a.EligibleForRetry = true
			a.MaxRetries = tc.maxRetries
			eval, err := e.Evaluate(ctx, a)
			require.NoError(t, err)
			assert.Equal(t, tc.want, eval.RetriesExhausted)
		})
	}
}

func TestEvaluator_SignalsEnabled_PerTypeLimits(t *testing.T) {
	t.Parallel()
	store := alert.NewRedisAlertStore(testutil.CreateTestRedisClient(t), "")
	cfOff := alert.WithConsecutiveFailureEnabled(false)

	assert.False(t, alert.NewEvaluator(store, 0, cfOff).SignalsEnabled(),
		"no tracking and no retries anywhere: nothing can fire")
	assert.True(t, alert.NewEvaluator(store, 0, cfOff, alert.WithTypeMaxRetries(map[string]int{"mcp": 3})).SignalsEnabled(),
		"a type with retries can exhaust even when the default has none")
	assert.False(t, alert.NewEvaluator(store, 0, cfOff, alert.WithTypeMaxRetries(map[string]int{"mcp": 0})).SignalsEnabled())
	assert.False(t, alert.NewEvaluator(store, 0, cfOff,
		alert.WithTypeMaxRetries(map[string]int{"mcp": 3}),
		alert.WithExhaustedRetriesEnabled(false),
	).SignalsEnabled(), "the exhausted-retries gate still applies")
}

// A skipped failure neither counts toward the streak nor resets it.
func TestEvaluator_SkipConsecutiveFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(testutil.CreateTestRedisClient(t), ""),
		3,
		alert.WithAutoDisableFailureCount(4),
		alert.WithAlertThresholds([]int{50, 100}),
	)

	// Two counted failures: 50% crossed at 2.
	levels := crossedLevels(t, ctx, e, "dest_skip", "tenant_skip", 1, 2)
	assert.Equal(t, []int{50}, levels)

	// Skipped failures (e.g. 410s) are invisible to the streak, whatever their count.
	for i := range 10 {
		a := failedAttempt("dest_skip", "tenant_skip", fmt.Sprintf("att_410_%d", i))
		a.SkipConsecutiveFailure = true
		eval, err := e.Evaluate(ctx, a)
		require.NoError(t, err)
		assert.Nil(t, eval.ConsecutiveFailure)
	}

	// The streak resumes where it was: failures 3 and 4 reach 100%.
	levels = crossedLevels(t, ctx, e, "dest_skip", "tenant_skip", 3, 4)
	assert.Equal(t, []int{100}, levels)
}

// A skipped failure still reports retry exhaustion.
func TestEvaluator_SkipConsecutiveFailure_StillReportsExhaustion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := alert.NewEvaluator(
		alert.NewRedisAlertStore(testutil.CreateTestRedisClient(t), ""),
		3,
		alert.WithAutoDisableFailureCount(1),
	)
	a := failedAttempt("dest_skip_ex", "tenant_skip_ex", "att_4")
	a.Number = 4
	a.EligibleForRetry = true
	a.SkipConsecutiveFailure = true
	eval, err := e.Evaluate(ctx, a)
	require.NoError(t, err)
	assert.Nil(t, eval.ConsecutiveFailure)
	assert.True(t, eval.RetriesExhausted)
}
