package alert

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	topicConsecutiveFailure = "alert.destination.consecutive_failure"
)

// AlertEmitter is the interface for emitting alert events. Satisfied by opevents.Emitter.
type AlertEmitter interface {
	Emit(ctx context.Context, topic string, tenantID string, data any) error
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
	deploymentID string

	autoDisableFailureCount int
	alertThresholds         []int
}

// noopAlertMonitor is a monitor that does nothing
type noopAlertMonitor struct{}

func (m *noopAlertMonitor) HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	return nil
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
