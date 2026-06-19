package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// AlertEmitter is the interface for emitting alert events. Satisfied by opevents.Emitter.
type AlertEmitter interface {
	Emit(ctx context.Context, topic string, tenantID string, data any) error
}

// DestinationDisabler handles disabling destinations.
type DestinationDisabler interface {
	DisableDestination(ctx context.Context, tenantID, destinationID string) error
}

// AlertMonitor is the main interface for handling delivery attempt alerts
type AlertMonitor interface {
	HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error
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

// WithExhaustedRetriesIdempotence sets the idempotence instance for
// exhausted_retries suppression. When set, only the first exhaustion per
// destination within the TTL window emits an alert.
func WithExhaustedRetriesIdempotence(idemp idempotence.Idempotence) AlertOption {
	return func(m *alertMonitor) {
		m.exhaustedRetryIdemp = idemp
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
	emitter      AlertEmitter
	disabler     DestinationDisabler
	deploymentID string

	autoDisableFailureCount int
	alertThresholds         []int
	retryMaxLimit           int
	exhaustedRetryIdemp     idempotence.Idempotence

	consecutiveFailureEnabled bool
	exhaustedRetriesEnabled   bool
}

// NewAlertMonitor creates a new alert monitor. Emitter and retryMaxLimit are
// required — callers that don't need alerts should pass nil AlertMonitor to
// consumers instead.
func NewAlertMonitor(logger *logging.Logger, redisClient redis.Cmdable, emitter AlertEmitter, retryMaxLimit int, opts ...AlertOption) AlertMonitor {
	if emitter == nil {
		panic("alert: NewAlertMonitor requires a non-nil emitter")
	}
	alertMonitor := &alertMonitor{
		logger:                    logger,
		emitter:                   emitter,
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

func (m *alertMonitor) HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	if attempt.Attempt.Status == models.AttemptStatusSuccess {
		// Nothing is tracked when consecutive-failure alerting is disabled, so
		// there is no count to reset.
		if !m.consecutiveFailureEnabled {
			return nil
		}
		return m.store.ResetConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	}

	if m.consecutiveFailureEnabled {
		// A replayed attempt that already completed evaluation skips the rest of
		// the pipeline (exhausted-retries check and the evaluated mark), matching
		// the original single-pass behavior.
		done, err := m.handleConsecutiveFailure(ctx, attempt)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
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

		emitFn := func(ctx context.Context) error {
			if err := m.emitter.Emit(ctx, opevents.TopicAlertExhaustedRetries, attempt.Destination.TenantID, erData); err != nil {
				return err
			}
			m.logger.Ctx(ctx).Audit("alert sent",
				zap.String("topic", opevents.TopicAlertExhaustedRetries),
				zap.String("attempt_id", attempt.Attempt.ID),
				zap.String("event_id", attempt.Event.ID),
				zap.String("tenant_id", attempt.Destination.TenantID),
				zap.String("destination_id", attempt.Destination.ID),
				zap.String("destination_type", attempt.Destination.Type),
			)
			return nil
		}

		if m.exhaustedRetryIdemp != nil {
			key := "opevents:exhausted:" + attempt.Event.ID + ":" + attempt.Destination.ID
			if err := m.exhaustedRetryIdemp.Exec(ctx, key, emitFn); err != nil {
				return fmt.Errorf("failed to emit exhausted retries alert: %w", err)
			}
		} else {
			if err := emitFn(ctx); err != nil {
				return fmt.Errorf("failed to emit exhausted retries alert: %w", err)
			}
		}
	}

	// Mark the attempt fully evaluated so replays skip re-emitting alerts. Only
	// relevant when consecutive-failure tracking ran — it is the consumer of the
	// evaluated mark (the replay short-circuit above).
	// Non-fatal: on failure the attempt simply re-evaluates on replay, which
	// matches the previous behavior (emit/disable are idempotent-by-design).
	if m.consecutiveFailureEnabled {
		if err := m.store.MarkAttemptEvaluated(ctx, attempt.Destination.TenantID, attempt.Destination.ID, attempt.Attempt.ID); err != nil {
			m.logger.Ctx(ctx).Warn("failed to mark attempt evaluated",
				zap.Error(err),
				zap.String("attempt_id", attempt.Attempt.ID),
				zap.String("tenant_id", attempt.Destination.TenantID),
				zap.String("destination_id", attempt.Destination.ID),
			)
		}
	}

	return nil
}

// handleConsecutiveFailure runs consecutive-failure tracking, alerting and
// auto-disable for a failed attempt. It returns done=true when the attempt is a
// replay that already completed evaluation, signalling the caller to stop
// processing (skip the exhausted-retries check and the evaluated mark).
func (m *alertMonitor) handleConsecutiveFailure(ctx context.Context, attempt DeliveryAttempt) (done bool, err error) {
	res, err := m.store.IncrementConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID, attempt.Attempt.ID)
	if err != nil {
		return false, fmt.Errorf("failed to get alert state: %w", err)
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
		return true, nil
	}

	count := res.Count
	level, shouldAlert := m.evaluator.ShouldAlert(count)
	if !shouldAlert {
		return false, nil
	}

	// At 100% threshold, disable the destination and emit disabled alert.
	// Both operations are idempotent on replay: DisableDestination is a no-op
	// if already disabled, and consumers deduplicate events by ID.
	if level == 100 && m.disabler != nil {
		if err := m.disabler.DisableDestination(ctx, attempt.Destination.TenantID, attempt.Destination.ID); err != nil {
			return false, fmt.Errorf("failed to disable destination: %w", err)
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
		if err := m.emitter.Emit(ctx, opevents.TopicAlertDestinationDisabled, attempt.Destination.TenantID, disabledData); err != nil {
			return false, fmt.Errorf("failed to emit destination disabled alert: %w", err)
		}
	}

	// Emit consecutive failure alert
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
	if err := m.emitter.Emit(ctx, opevents.TopicAlertConsecutiveFailure, attempt.Destination.TenantID, cfData); err != nil {
		return false, fmt.Errorf("failed to emit consecutive failure alert: %w", err)
	}

	m.logger.Ctx(ctx).Audit("alert sent",
		zap.String("topic", opevents.TopicAlertConsecutiveFailure),
		zap.String("attempt_id", attempt.Attempt.ID),
		zap.String("event_id", attempt.Event.ID),
		zap.String("tenant_id", attempt.Destination.TenantID),
		zap.String("destination_id", attempt.Destination.ID),
		zap.String("destination_type", attempt.Destination.Type),
	)

	return false, nil
}
