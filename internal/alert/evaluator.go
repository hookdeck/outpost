// Package alert tracks delivery health per destination: consecutive-failure
// counting, alert thresholds, and retry exhaustion. It is a pure tracker — it
// returns signals as data and performs no side effects outside its own state.
// Acting on the signals (operator events, auto-disable, replay dedup) is the
// caller's job.
package alert

import (
	"context"
	"fmt"
)

// Attempt is the tracker's input: the identity and outcome of one delivery
// attempt, nothing more.
type Attempt struct {
	TenantID         string
	DestinationID    string
	AttemptID        string
	Number           int // 1-indexed attempt number
	Success          bool
	EligibleForRetry bool
}

// Evaluation is the tracker's verdict on one attempt: one field per signal
// kind, nil/false when that kind has nothing to report. An attempt can carry
// several signals at once. Zero value = nothing to report (success, or a
// failure that crossed no threshold and exhausted no retries).
type Evaluation struct {
	// ConsecutiveFailure is non-nil when this attempt's consecutive-failure
	// count crossed an alert threshold.
	ConsecutiveFailure *ConsecutiveFailureSignal
	// RetriesExhausted reports that this attempt exceeded the retry budget for
	// a retry-eligible event.
	RetriesExhausted bool
}

// ConsecutiveFailureSignal reports a crossed consecutive-failure threshold.
type ConsecutiveFailureSignal struct {
	Failures int // current consecutive-failure count
	Max      int // configured 100%-threshold failure count
	Level    int // crossed threshold's percentage (e.g. 50/70/90/100)
}

// Option configures an evaluator.
type Option func(*Evaluator)

// WithAutoDisableFailureCount sets the consecutive-failure count that means
// 100% — the denominator for threshold math.
func WithAutoDisableFailureCount(count int) Option {
	return func(e *Evaluator) {
		e.autoDisableFailureCount = count
	}
}

// WithAlertThresholds sets the percentage thresholds at which alerts fire.
func WithAlertThresholds(thresholds []int) Option {
	return func(e *Evaluator) {
		e.alertThresholds = thresholds
	}
}

// WithConsecutiveFailureEnabled toggles consecutive-failure tracking. When set
// to false the evaluator never tracks failures or crosses thresholds.
// Defaults to true.
func WithConsecutiveFailureEnabled(enabled bool) Option {
	return func(e *Evaluator) {
		e.consecutiveFailureEnabled = enabled
	}
}

// WithExhaustedRetriesEnabled toggles the retry-exhaustion signal. Defaults to
// true.
func WithExhaustedRetriesEnabled(enabled bool) Option {
	return func(e *Evaluator) {
		e.exhaustedRetriesEnabled = enabled
	}
}

// Evaluator evaluates delivery attempts against the destination's failure
// history and returns the resulting signals as data.
type Evaluator struct {
	store      AlertStore
	thresholds thresholdEvaluator

	autoDisableFailureCount int
	alertThresholds         []int
	retryMaxLimit           int

	consecutiveFailureEnabled bool
	exhaustedRetriesEnabled   bool
}

// NewEvaluator creates a new alert evaluator on the given store.
func NewEvaluator(store AlertStore, retryMaxLimit int, opts ...Option) *Evaluator {
	e := &Evaluator{
		store:                     store,
		retryMaxLimit:             retryMaxLimit,
		alertThresholds:           []int{50, 70, 90, 100}, // default thresholds
		consecutiveFailureEnabled: true,
		exhaustedRetriesEnabled:   true,
	}

	for _, opt := range opts {
		opt(e)
	}

	e.thresholds = newThresholdEvaluator(e.alertThresholds, e.autoDisableFailureCount)

	return e
}

func (e *Evaluator) Evaluate(ctx context.Context, attempt Attempt) (Evaluation, error) {
	if attempt.Success {
		// Nothing is tracked when consecutive-failure tracking is disabled, so
		// there is no count to reset.
		if !e.consecutiveFailureEnabled {
			return Evaluation{}, nil
		}
		if err := e.store.ResetConsecutiveFailureCount(ctx, attempt.TenantID, attempt.DestinationID); err != nil {
			return Evaluation{}, err
		}
		return Evaluation{}, nil
	}

	var eval Evaluation

	if e.consecutiveFailureEnabled {
		count, err := e.store.IncrementConsecutiveFailureCount(ctx, attempt.TenantID, attempt.DestinationID, attempt.AttemptID)
		if err != nil {
			return Evaluation{}, fmt.Errorf("failed to track consecutive failures: %w", err)
		}
		if level, crossed := e.thresholds.shouldAlert(count); crossed {
			eval.ConsecutiveFailure = &ConsecutiveFailureSignal{
				Failures: count,
				Max:      e.autoDisableFailureCount,
				Level:    level,
			}
		}
	}

	// Exhausted retries check (independent of consecutive failure thresholds).
	// Attempt is 1-indexed: with retryMaxLimit=10, attempt 11 is the final one.
	// Skip if retryMaxLimit=0 (retries disabled — no exhausted state to report)
	// or if the exhausted-retries signal is disabled.
	if e.exhaustedRetriesEnabled && e.retryMaxLimit > 0 && attempt.EligibleForRetry && attempt.Number > e.retryMaxLimit {
		eval.RetriesExhausted = true
	}

	return eval, nil
}
