package alert

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// AlertMonitor is the main interface for handling delivery attempt alerts
type AlertMonitor interface {
	HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error
}

// AlertEvaluator determines when alerts should be triggered
type AlertEvaluator interface {
	// ShouldAlert determines if an alert should be sent based on failures and debouncing
	ShouldAlert(failures int64, lastAlertTime time.Time) bool

	// GetAlertLevel returns the threshold level reached (e.g., 50%, 70%, 90%, 100%)
	GetAlertLevel(failures int64) (int, bool)
}

// AlertNotifier sends alerts to configured destinations
type AlertNotifier interface {
	// Notify sends an alert to the configured callback URL
	Notify(ctx context.Context, alert Alert) error
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
	Success     bool
	Destination *models.Destination
	Response    *Response
	Timestamp   time.Time
}

// Response contains details about a failed delivery attempt
type Response struct {
	Status string         `json:"status"`
	Data   map[string]any `json:"data"`
}

// Alert represents an alert that will be sent to the callback URL
type Alert struct {
	Topic               string              `json:"topic"`
	DisableThreshold    int                 `json:"disable_threshold"`
	ConsecutiveFailures int64               `json:"consecutive_failures"`
	Destination         *models.Destination `json:"destination"`
	Response            *Response           `json:"response,omitempty"`
}

// Common errors
var (
	ErrNotFound = errors.New("not found")
)
