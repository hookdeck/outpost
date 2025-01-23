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

// DestinationDisabler handles disabling destinations
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

// WithNotifier sets the alert notifier for the monitor
func WithNotifier(notifier AlertNotifier) AlertOption {
	return func(m *alertMonitor) {
		m.notifier = notifier
	}
}

// WithDisabler sets the destination disabler for the monitor
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

// DeliveryAttempt represents a single delivery attempt
type DeliveryAttempt struct {
	Success       bool
	DeliveryEvent *models.DeliveryEvent
	Destination   *models.Destination
	Timestamp     time.Time
	Data          map[string]interface{}
}

type alertMonitor struct {
	logger    *logging.Logger
	store     AlertStore
	evaluator AlertEvaluator
	notifier  AlertNotifier
	disabler  DestinationDisabler

	// autoDisableFailureCount is the number of consecutive failures before auto-disabling
	autoDisableFailureCount int
	// alertThresholds defines the percentage thresholds at which to send alerts
	// e.g., []int{50, 70, 90, 100} means send alerts at 50%, 70%, 90%, and 100% of AutoDisableFailureCount
	alertThresholds []int
}

// noopAlertMonitor is a monitor that does nothing
type noopAlertMonitor struct{}

func (m *noopAlertMonitor) HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	return nil
}

// NewAlertMonitor creates a new alert monitor with default implementations
func NewAlertMonitor(logger *logging.Logger, redisClient *redis.Client, opts ...AlertOption) AlertMonitor {
	alertMonitor := &alertMonitor{
		logger:          logger,
		alertThresholds: []int{50, 70, 90, 100}, // default thresholds
	}

	for _, opt := range opts {
		opt(alertMonitor)
	}

	if alertMonitor.notifier == nil && alertMonitor.disabler == nil {
		return &noopAlertMonitor{}
	}

	if alertMonitor.store == nil {
		alertMonitor.store = NewRedisAlertStore(redisClient)
	}

	if alertMonitor.evaluator == nil {
		alertMonitor.evaluator = NewAlertEvaluator(alertMonitor.alertThresholds, alertMonitor.autoDisableFailureCount)
	}

	return alertMonitor
}

func (m *alertMonitor) HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	if attempt.Success {
		return m.store.ResetConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	}

	// Get alert state
	count, err := m.store.IncrementConsecutiveFailureCount(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	if err != nil {
		return fmt.Errorf("failed to get alert state: %w", err)
	}

	// Check if we should send an alert
	level, shouldAlert := m.evaluator.ShouldAlert(count)
	if !shouldAlert {
		return nil
	}

	alert := NewConsecutiveFailureAlert(ConsecutiveFailureData{
		MaxConsecutiveFailures: m.autoDisableFailureCount,
		ConsecutiveFailures:    count,
		WillDisable:            m.disabler != nil && level == 100,
		Destination:            attempt.Destination,
		Data:                   attempt.Data,
	})

	// If we've hit 100% and have a disabler configured, disable the destination
	if level == 100 && m.disabler != nil {
		if err := m.disabler.DisableDestination(ctx, attempt.Destination.TenantID, attempt.Destination.ID); err != nil {
			return fmt.Errorf("failed to disable destination: %w", err)
		}

		m.logger.Ctx(ctx).Audit("destination disabled",
			zap.String("destination_id", attempt.Destination.ID),
			zap.String("tenant_id", attempt.Destination.TenantID),
			zap.String("topic", alert.Topic),
		)
	}

	// Send alert if notifier is configured
	if m.notifier != nil {
		if err := m.notifier.Notify(ctx, alert); err != nil {
			m.logger.Ctx(ctx).Error("failed to send alert",
				zap.String("destination_id", attempt.Destination.ID),
				zap.String("tenant_id", attempt.Destination.TenantID),
				zap.String("topic", alert.Topic),
				zap.Error(err),
			)
			return fmt.Errorf("failed to send alert: %w", err)
		}

		m.logger.Ctx(ctx).Audit("alert sent",
			zap.String("destination_id", attempt.Destination.ID),
			zap.String("tenant_id", attempt.Destination.TenantID),
			zap.String("topic", alert.Topic),
		)
	}

	return nil
}
