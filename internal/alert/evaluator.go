// Package alert tracks delivery health per destination: consecutive-failure
// counting, alert thresholds, and retry exhaustion. It is a pure tracker — it
// returns signals as data and performs no side effects outside its own state.
// Acting on the signals (operator events, auto-disable, replay dedup) is the
// caller's job.
package alert

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
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

// Evaluation is the tracker's verdict on one attempt. Zero value = nothing to
// report (success, or a failure that crossed no threshold and exhausted no
// retries).
type Evaluation struct {
	// ConsecutiveFailures is the destination's current consecutive-failure
	// count after recording this attempt. 0 when consecutive-failure tracking
	// is disabled.
	ConsecutiveFailures int
	// ThresholdCrossed reports that this attempt's count sits on a configured
	// alert threshold (or at/above the 100% auto-disable count).
	ThresholdCrossed bool
	// ThresholdLevel is the crossed threshold's percentage (e.g. 50/70/90/100).
	// 0 when no threshold was crossed.
	ThresholdLevel int
	// MaxFailures is the configured 100%-threshold failure count, for payload
	// context alongside ConsecutiveFailures.
	MaxFailures int
	// RetriesExhausted reports that this attempt exceeded the retry budget for
	// a retry-eligible event.
	RetriesExhausted bool
}

// Evaluator evaluates delivery attempts against the destination's failure
// history and returns the resulting signals as data.
type Evaluator interface {
	Evaluate(ctx context.Context, attempt Attempt) (Evaluation, error)
}

// Option configures an evaluator.
type Option func(*evaluator)

// WithAutoDisableFailureCount sets the consecutive-failure count that means
// 100% — the denominator for threshold math.
func WithAutoDisableFailureCount(count int) Option {
	return func(e *evaluator) {
		e.autoDisableFailureCount = count
	}
}

// WithAlertThresholds sets the percentage thresholds at which alerts fire.
func WithAlertThresholds(thresholds []int) Option {
	return func(e *evaluator) {
		e.alertThresholds = thresholds
	}
}

// WithStore sets the alert store for the evaluator.
func WithStore(store AlertStore) Option {
	return func(e *evaluator) {
		e.store = store
	}
}

// WithDeploymentID sets the deployment ID used to scope store keys.
func WithDeploymentID(deploymentID string) Option {
	return func(e *evaluator) {
		e.deploymentID = deploymentID
	}
}

// WithConsecutiveFailureEnabled toggles consecutive-failure tracking. When set
// to false the evaluator never tracks failures or crosses thresholds.
// Defaults to true.
func WithConsecutiveFailureEnabled(enabled bool) Option {
	return func(e *evaluator) {
		e.consecutiveFailureEnabled = enabled
	}
}

// WithExhaustedRetriesEnabled toggles the retry-exhaustion signal. Defaults to
// true.
func WithExhaustedRetriesEnabled(enabled bool) Option {
	return func(e *evaluator) {
		e.exhaustedRetriesEnabled = enabled
	}
}

type evaluator struct {
	store      AlertStore
	thresholds thresholdEvaluator

	deploymentID            string
	autoDisableFailureCount int
	alertThresholds         []int
	retryMaxLimit           int

	consecutiveFailureEnabled bool
	exhaustedRetriesEnabled   bool
}

// NewEvaluator creates a new alert evaluator.
func NewEvaluator(redisClient redis.Cmdable, retryMaxLimit int, opts ...Option) Evaluator {
	e := &evaluator{
		retryMaxLimit:             retryMaxLimit,
		alertThresholds:           []int{50, 70, 90, 100}, // default thresholds
		consecutiveFailureEnabled: true,
		exhaustedRetriesEnabled:   true,
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.store == nil {
		e.store = NewRedisAlertStore(redisClient, e.deploymentID)
	}

	e.thresholds = newThresholdEvaluator(e.alertThresholds, e.autoDisableFailureCount)

	return e
}

func (e *evaluator) Evaluate(ctx context.Context, attempt Attempt) (Evaluation, error) {
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
		level, crossed := e.thresholds.shouldAlert(count)
		eval.ConsecutiveFailures = count
		eval.ThresholdCrossed = crossed
		eval.ThresholdLevel = level
		eval.MaxFailures = e.autoDisableFailureCount
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
