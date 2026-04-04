package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	topicConsecutiveFailure  = "alert.destination.consecutive_failure"
	topicDestinationDisabled = "alert.destination.disabled"
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
}

// NewAlertMonitor creates a new alert monitor. Emitter is required — callers
// that don't need alerts should pass nil AlertMonitor to consumers instead.
func NewAlertMonitor(logger *logging.Logger, redisClient redis.Cmdable, emitter AlertEmitter, opts ...AlertOption) AlertMonitor {
	alertMonitor := &alertMonitor{
		logger:          logger,
		emitter:         emitter,
		alertThresholds: []int{50, 70, 90, 100}, // default thresholds
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
		return m.store.ResetConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	}

	// Get alert state — attemptID ensures idempotent counting on message replay
	count, err := m.store.IncrementConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID, attempt.Attempt.ID)
	if err != nil {
		return fmt.Errorf("failed to get alert state: %w", err)
	}

	level, shouldAlert := m.evaluator.ShouldAlert(count)
	if !shouldAlert {
		return nil
	}

	// At 100% threshold, disable the destination and emit disabled alert.
	// Both operations are idempotent on replay: DisableDestination is a no-op
	// if already disabled, and consumers deduplicate events by ID.
	if level == 100 && m.disabler != nil {
		if err := m.disabler.DisableDestination(ctx, attempt.Destination.TenantID, attempt.Destination.ID); err != nil {
			return fmt.Errorf("failed to disable destination: %w", err)
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
		if err := m.emitter.Emit(ctx, topicDestinationDisabled, attempt.Destination.TenantID, disabledData); err != nil {
			return fmt.Errorf("failed to emit destination disabled alert: %w", err)
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
	if err := m.emitter.Emit(ctx, topicConsecutiveFailure, attempt.Destination.TenantID, cfData); err != nil {
		return fmt.Errorf("failed to emit consecutive failure alert: %w", err)
	}

	m.logger.Ctx(ctx).Audit("alert sent",
		zap.String("topic", topicConsecutiveFailure),
		zap.String("attempt_id", attempt.Attempt.ID),
		zap.String("event_id", attempt.Event.ID),
		zap.String("tenant_id", attempt.Destination.TenantID),
		zap.String("destination_id", attempt.Destination.ID),
		zap.String("destination_type", attempt.Destination.Type),
	)

	return nil
}
