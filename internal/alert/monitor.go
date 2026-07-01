package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// DestinationDisabler handles disabling destinations.
type DestinationDisabler interface {
	DisableDestination(ctx context.Context, tenantID, destinationID string) error
}

// AlertMonitor evaluates delivery attempts and returns the alerts to deliver as
// data. It does not emit — the caller (logmq) owns delivery.
type AlertMonitor interface {
	Evaluate(ctx context.Context, attempt DeliveryAttempt) (Evaluation, error)
}

// Evaluation is the result of evaluating one delivery attempt: the operator
// events to deliver (in order) plus a commit callback the caller runs strictly
// AFTER all events are delivered.
type Evaluation struct {
	// Events to emit, in order. The delivery layer recognizes the
	// exhausted-retries event by topic and wraps that emit in its
	// per-(event,destination) suppression window.
	Events []opevents.Event
	// Commit marks the attempt fully evaluated (so replays skip re-emitting).
	// Nil when there is nothing to commit (success, replay short-circuit, or
	// consecutive-failure tracking disabled). Non-fatal: on error the attempt
	// simply re-evaluates on replay.
	Commit func(ctx context.Context) error
}

// AlertOption is a function that configures an AlertConfig
type AlertOption func(*alertMonitor)

// WithAutoDisableFailureCount sets the number of consecutive failures before auto-disabling
func WithAutoDisableFailureCount(count int) AlertOption {
	return func(c *alertMonitor) {
		c.autoDisableFailureCount = count
	}
}

// WithAlertThresholds sets the percentage thresholds at which to send alerts
func WithAlertThresholds(thresholds []int) AlertOption {
	return func(c *alertMonitor) {
		c.alertThresholds = thresholds
	}
}

// WithStore sets the alert store for the monitor
func WithStore(store AlertStore) AlertOption {
	return func(m *alertMonitor) {
		m.store = store
	}
}

// WithEvaluator sets the alert evaluator for the monitor
func WithEvaluator(evaluator AlertEvaluator) AlertOption {
	return func(m *alertMonitor) {
		m.evaluator = evaluator
	}
}

// WithDisabler sets the destination disabler for the monitor.
// When set, destinations are auto-disabled at the 100% failure threshold.
func WithDisabler(disabler DestinationDisabler) AlertOption {
	return func(m *alertMonitor) {
		m.disabler = disabler
	}
}

// WithLogger sets the logger for the monitor
func WithLogger(logger *logging.Logger) AlertOption {
	return func(m *alertMonitor) {
		m.logger = logger
	}
}

// WithDeploymentID sets the deployment ID for the monitor
func WithDeploymentID(deploymentID string) AlertOption {
	return func(m *alertMonitor) {
		m.deploymentID = deploymentID
	}
}

// WithConsecutiveFailureEnabled toggles consecutive-failure alerting. When set
// to false the monitor never tracks or alerts on consecutive failures (and
// therefore never auto-disables). Defaults to true.
func WithConsecutiveFailureEnabled(enabled bool) AlertOption {
	return func(m *alertMonitor) {
		m.consecutiveFailureEnabled = enabled
	}
}

// WithExhaustedRetriesEnabled toggles exhausted_retries alerting. When set to
// false the monitor never emits exhausted_retries alerts. Defaults to true.
func WithExhaustedRetriesEnabled(enabled bool) AlertOption {
	return func(m *alertMonitor) {
		m.exhaustedRetriesEnabled = enabled
	}
}

// DeliveryAttempt represents a single delivery attempt
type DeliveryAttempt struct {
	Event       *models.Event
	Destination *AlertDestination
	Attempt     *models.Attempt
}

type alertMonitor struct {
	logger       *logging.Logger
	store        AlertStore
	evaluator    AlertEvaluator
	disabler     DestinationDisabler
	deploymentID string

	autoDisableFailureCount int
	alertThresholds         []int
	retryMaxLimit           int

	consecutiveFailureEnabled bool
	exhaustedRetriesEnabled   bool
}

// NewAlertMonitor creates a new alert monitor. The monitor is eval-only: it
// decides which alerts fire and returns them as data for the caller to deliver.
func NewAlertMonitor(logger *logging.Logger, redisClient redis.Cmdable, retryMaxLimit int, opts ...AlertOption) AlertMonitor {
	alertMonitor := &alertMonitor{
		logger:                    logger,
		retryMaxLimit:             retryMaxLimit,
		alertThresholds:           []int{50, 70, 90, 100}, // default thresholds
		consecutiveFailureEnabled: true,
		exhaustedRetriesEnabled:   true,
	}

	for _, opt := range opts {
		opt(alertMonitor)
	}

	if alertMonitor.store == nil {
		alertMonitor.store = NewRedisAlertStore(redisClient, alertMonitor.deploymentID)
	}

	if alertMonitor.evaluator == nil {
		alertMonitor.evaluator = NewAlertEvaluator(alertMonitor.alertThresholds, alertMonitor.autoDisableFailureCount)
	}

	return alertMonitor
}

func (m *alertMonitor) Evaluate(ctx context.Context, attempt DeliveryAttempt) (Evaluation, error) {
	if attempt.Attempt.Status == models.AttemptStatusSuccess {
		// Nothing is tracked when consecutive-failure alerting is disabled, so
		// there is no count to reset.
		if !m.consecutiveFailureEnabled {
			return Evaluation{}, nil
		}
		if err := m.store.ResetConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID); err != nil {
			return Evaluation{}, err
		}
		return Evaluation{}, nil
	}

	var events []opevents.Event

	if m.consecutiveFailureEnabled {
		// A replayed attempt that already completed evaluation skips the rest of
		// the pipeline (exhausted-retries check and the evaluated mark), matching
		// the original single-pass behavior.
		cfEvents, done, err := m.evaluateConsecutiveFailure(ctx, attempt)
		if err != nil {
			return Evaluation{}, err
		}
		if done {
			return Evaluation{}, nil
		}
		events = append(events, cfEvents...)
	}

	// Exhausted retries check (independent of consecutive failure thresholds).
	// Attempt is 1-indexed: with retryMaxLimit=10, attempt 11 is the final one.
	// Skip if retryMaxLimit=0 (retries disabled — no exhausted state to report)
	// or if exhausted-retries alerting is disabled.
	if m.exhaustedRetriesEnabled && m.retryMaxLimit > 0 && attempt.Event.EligibleForRetry && attempt.Attempt.AttemptNumber > m.retryMaxLimit {
		erData := ExhaustedRetriesData{
			TenantID:    attempt.Destination.TenantID,
			Event:       attempt.Event,
			Attempt:     attempt.Attempt,
			Destination: attempt.Destination,
		}
		// Eval only decides that retries are exhausted. Suppression (dedup per
		// event+destination within a window) is a delivery concern the caller owns.
		events = append(events, opevents.Event{
			Topic:    opevents.TopicAlertExhaustedRetries,
			TenantID: attempt.Destination.TenantID,
			Data:     erData,
		})
	}

	// Mark the attempt fully evaluated so replays skip re-emitting alerts. Only
	// relevant when consecutive-failure tracking ran — it is the consumer of the
	// evaluated mark (the replay short-circuit above). The caller runs this
	// strictly AFTER all events are delivered.
	var commit func(ctx context.Context) error
	if m.consecutiveFailureEnabled {
		tenantID := attempt.Destination.TenantID
		destID := attempt.Destination.ID
		attemptID := attempt.Attempt.ID
		commit = func(ctx context.Context) error {
			return m.store.MarkAttemptEvaluated(ctx, tenantID, destID, attemptID)
		}
	}

	return Evaluation{Events: events, Commit: commit}, nil
}

// evaluateConsecutiveFailure runs consecutive-failure tracking and auto-disable
// for a failed attempt, returning the events to deliver (disabled then
// consecutive_failure, in order). It returns done=true when the attempt is a
// replay that already completed evaluation, signalling the caller to stop
// processing (skip the exhausted-retries check and the evaluated mark).
func (m *alertMonitor) evaluateConsecutiveFailure(ctx context.Context, attempt DeliveryAttempt) (events []opevents.Event, done bool, err error) {
	res, err := m.store.IncrementConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID, attempt.Attempt.ID)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get alert state: %w", err)
	}

	// Replayed attempt (MQ redelivery, producer re-publish) that already
	// completed a full evaluation — skip re-emitting alerts. Attempts that
	// were counted but never marked evaluated (eval failed mid-way and the
	// message was nacked) fall through and re-run as recovery.
	if !res.NewlyCounted && res.AlreadyEvaluated {
		m.logger.Ctx(ctx).Debug("skipping replayed attempt: already evaluated",
			zap.String("attempt_id", attempt.Attempt.ID),
			zap.String("tenant_id", attempt.Destination.TenantID),
			zap.String("destination_id", attempt.Destination.ID),
		)
		return nil, true, nil
	}

	count := res.Count
	level, shouldAlert := m.evaluator.ShouldAlert(count)
	if !shouldAlert {
		return nil, false, nil
	}

	// At 100% threshold, disable the destination and emit disabled alert.
	// Both operations are idempotent on replay: DisableDestination is a no-op
	// if already disabled, and consumers deduplicate events by ID.
	if level == 100 && m.disabler != nil {
		if err := m.disabler.DisableDestination(ctx, attempt.Destination.TenantID, attempt.Destination.ID); err != nil {
			return nil, false, fmt.Errorf("failed to disable destination: %w", err)
		}

		now := time.Now()
		attempt.Destination.DisabledAt = &now

		m.logger.Ctx(ctx).Audit("destination disabled",
			zap.String("attempt_id", attempt.Attempt.ID),
			zap.String("event_id", attempt.Event.ID),
			zap.String("tenant_id", attempt.Destination.TenantID),
			zap.String("destination_id", attempt.Destination.ID),
			zap.String("destination_type", attempt.Destination.Type),
		)

		disabledData := DestinationDisabledData{
			TenantID:    attempt.Destination.TenantID,
			Destination: attempt.Destination,
			DisabledAt:  now,
			Reason:      "consecutive_failure",
			Event:       attempt.Event,
			Attempt:     attempt.Attempt,
		}
		events = append(events, opevents.Event{
			Topic:    opevents.TopicAlertDestinationDisabled,
			TenantID: attempt.Destination.TenantID,
			Data:     disabledData,
		})
	}

	cfData := ConsecutiveFailureData{
		TenantID:    attempt.Destination.TenantID,
		Event:       attempt.Event,
		Attempt:     attempt.Attempt,
		Destination: attempt.Destination,
		ConsecutiveFailures: ConsecutiveFailures{
			Current:   count,
			Max:       m.autoDisableFailureCount,
			Threshold: level,
		},
	}
	events = append(events, opevents.Event{
		Topic:    opevents.TopicAlertConsecutiveFailure,
		TenantID: attempt.Destination.TenantID,
		Data:     cfData,
	})

	return events, false, nil
}
