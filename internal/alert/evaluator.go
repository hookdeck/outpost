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
	// MaxRetries is the retry budget of the attempt's destination type, when
	// it has its own: 0 = the evaluator's default limit, negative = no
	// retries (never exhausted).
	MaxRetries int
	// SkipConsecutiveFailure leaves the consecutive-failure streak untouched
	// (no increment, no reset) for a failure that says nothing about the
	// destination's health, e.g. a receiver rejecting one event with 410.
	SkipConsecutiveFailure bool
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

// WithTypeMaxRetries declares the per-destination-type retry limits that
// attempts may carry in Attempt.MaxRetries. The evaluator only uses them to
// decide SignalsEnabled: exhausted-retries can fire when any type retries,
// even with the default limit at 0.
func WithTypeMaxRetries(limits map[string]int) Option {
	return func(e *Evaluator) {
		for _, limit := range limits {
			e.typeMaxRetries = max(e.typeMaxRetries, limit)
		}
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
	// typeMaxRetries is the highest per-type retry limit (WithTypeMaxRetries).
	typeMaxRetries int

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

// SignalsEnabled reports whether any signal can ever fire: consecutive-failure
// tracking, or exhausted-retries with a positive retry limit (default or any
// per-type one). When false, Evaluate never touches the store and always
// returns an empty verdict.
func (e *Evaluator) SignalsEnabled() bool {
	return e.consecutiveFailureEnabled ||
		(e.exhaustedRetriesEnabled && (e.retryMaxLimit > 0 || e.typeMaxRetries > 0))
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

	if e.consecutiveFailureEnabled && !attempt.SkipConsecutiveFailure {
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
	// Attempt is 1-indexed: with a limit of 10, attempt 11 is the final one.
	// Skip if the limit is 0 (retries disabled — no exhausted state to report)
	// or if the exhausted-retries signal is disabled.
	limit := e.retryLimit(attempt)
	if e.exhaustedRetriesEnabled && limit > 0 && attempt.EligibleForRetry && attempt.Number > limit {
		eval.RetriesExhausted = true
	}

	return eval, nil
}

// retryLimit is the retry budget that applies to the attempt: its own
// MaxRetries when set, else the evaluator's default.
func (e *Evaluator) retryLimit(attempt Attempt) int {
	switch {
	case attempt.MaxRetries > 0:
		return attempt.MaxRetries
	case attempt.MaxRetries < 0:
		return 0
	default:
		return e.retryMaxLimit
	}
}
