package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/redis/go-redis/v9"
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

// WithDebouncingInterval sets the debouncing interval in milliseconds
func WithDebouncingInterval(ms int64) AlertOption {
	return func(c *alertMonitor) {
		c.debouncingIntervalMS = ms
	}
}

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

// DeliveryAttempt represents a single delivery attempt
type DeliveryAttempt struct {
	Success       bool
	DeliveryEvent *models.DeliveryEvent
	Destination   *models.Destination
	Response      *Response
	Timestamp     time.Time
}

// Response contains details about a failed delivery attempt
type Response struct {
	Status string         `json:"status"`
	Data   map[string]any `json:"data"`
}

type alertMonitor struct {
	store     AlertStore
	evaluator AlertEvaluator
	notifier  AlertNotifier
	disabler  DestinationDisabler

	// debouncingIntervalMS is the time in milliseconds between alerts for the same destination
	debouncingIntervalMS int64
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
func NewAlertMonitor(redisClient *redis.Client, opts ...AlertOption) AlertMonitor {
	alertMonitor := &alertMonitor{}

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
		alertMonitor.evaluator = NewAlertEvaluator(alertMonitor.alertThresholds, alertMonitor.autoDisableFailureCount, alertMonitor.debouncingIntervalMS)
	}

	return alertMonitor
}

func (m *alertMonitor) HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	if attempt.Success {
		return m.store.ResetAlertState(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	}

	// Get alert state
	state, err := m.store.IncrementAndGetAlertState(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	if err != nil {
		return fmt.Errorf("failed to get alert state: %w", err)
	}

	// Check if we should send an alert
	level, shouldAlert := m.evaluator.ShouldAlert(state.FailureCount, state.LastAlertTime, state.LastAlertLevel)
	if !shouldAlert {
		return nil
	}

	// Send alert if notifier is configured
	if m.notifier != nil {
		alert := Alert{
			Topic:               attempt.DeliveryEvent.Event.Topic,
			DisableThreshold:    m.autoDisableFailureCount,
			ConsecutiveFailures: state.FailureCount,
			Destination:         attempt.Destination,
			Response:            attempt.Response,
		}

		if err := m.notifier.Notify(ctx, alert); err != nil {
			return fmt.Errorf("failed to send alert: %w", err)
		}

		// Update last alert time and level atomically
		if err := m.store.UpdateLastAlert(ctx, attempt.Destination.TenantID, attempt.Destination.ID, time.Now(), level); err != nil {
			return fmt.Errorf("failed to update last alert state: %w", err)
		}
	}

	// If we've hit 100% and have a disabler configured, disable the destination
	if level == 100 && m.disabler != nil {
		if err := m.disabler.DisableDestination(ctx, attempt.Destination.TenantID, attempt.Destination.ID); err != nil {
			return fmt.Errorf("failed to disable destination: %w", err)
		}
	}

	return nil
}
