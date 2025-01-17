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

// AlertOption is a function that configures an alertMonitor
type AlertOption func(*AlertConfig)

// WithDebouncingInterval sets the debouncing interval in milliseconds
func WithDebouncingInterval(ms int64) AlertOption {
	return func(c *AlertConfig) {
		c.DebouncingIntervalMS = ms
	}
}

// WithAutoDisableFailureCount sets the number of consecutive failures before auto-disabling
func WithAutoDisableFailureCount(count int) AlertOption {
	return func(c *AlertConfig) {
		c.AutoDisableFailureCount = count
	}
}

// WithCallbackURL sets the URL where alerts will be sent
func WithCallbackURL(url string) AlertOption {
	return func(c *AlertConfig) {
		c.CallbackURL = url
	}
}

// WithAlertThresholds sets the percentage thresholds at which to send alerts
func WithAlertThresholds(thresholds []int) AlertOption {
	return func(c *AlertConfig) {
		c.AlertThresholds = thresholds
	}
}

// AlertConfig holds configuration for the alert system
type AlertConfig struct {
	// DebouncingIntervalMS is the time in milliseconds between alerts for the same destination
	DebouncingIntervalMS int64
	// AutoDisableFailureCount is the number of consecutive failures before auto-disabling
	AutoDisableFailureCount int
	// CallbackURL is where alerts will be sent
	CallbackURL string
	// AlertThresholds defines the percentage thresholds at which to send alerts
	// e.g., []int{50, 70, 90, 100} means send alerts at 50%, 70%, 90%, and 100% of AutoDisableFailureCount
	AlertThresholds []int
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
	config    AlertConfig
}

// NewAlertMonitor creates a new alert monitor with default implementations
func NewAlertMonitor(redisClient *redis.Client, disabler DestinationDisabler, opts ...AlertOption) AlertMonitor {
	config := AlertConfig{
		DebouncingIntervalMS:    0,                      // Default 0 debounce
		AutoDisableFailureCount: 20,                     // Default 20 failures
		AlertThresholds:         []int{50, 70, 90, 100}, // Default thresholds
	}

	for _, opt := range opts {
		opt(&config)
	}

	return NewAlertMonitorWithDeps(
		NewRedisAlertStore(redisClient),
		NewAlertEvaluator(config),
		NewHTTPAlertNotifier(config.CallbackURL),
		disabler,
		config,
	)
}

// NewAlertMonitorWithDeps creates a monitor with the provided dependencies
func NewAlertMonitorWithDeps(store AlertStore, evaluator AlertEvaluator, notifier AlertNotifier, disabler DestinationDisabler, config AlertConfig) AlertMonitor {
	return &alertMonitor{
		store:     store,
		evaluator: evaluator,
		notifier:  notifier,
		disabler:  disabler,
		config:    config,
	}
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

	// Send alert
	alert := Alert{
		Topic:               attempt.DeliveryEvent.Event.Topic,
		DisableThreshold:    m.config.AutoDisableFailureCount,
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

	// If we've hit 100%, we should disable the destination
	if level == 100 {
		if err := m.disabler.DisableDestination(ctx, attempt.Destination.TenantID, attempt.Destination.ID); err != nil {
			return fmt.Errorf("failed to disable destination: %w", err)
		}
	}

	return nil
}
