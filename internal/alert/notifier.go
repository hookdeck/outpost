package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// Alert represents any alert that can be sent
type Alert interface {
	json.Marshaler
}

// AlertNotifier sends alerts to configured destinations
type AlertNotifier interface {
	// Notify sends an alert to the configured callback URL
	Notify(ctx context.Context, alert Alert) error
}

// NotifierOption configures an AlertNotifier
type NotifierOption func(n *httpAlertNotifier)

// NotifierWithTimeout sets the timeout for alert notifications.
// If timeout is 0, it defaults to 30 seconds.
func NotifierWithTimeout(timeout time.Duration) NotifierOption {
	return func(n *httpAlertNotifier) {
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		n.client.Timeout = timeout
	}
}

func NotifierWithBearerToken(token string) NotifierOption {
	return func(n *httpAlertNotifier) {
		n.bearerToken = token
	}
}

type AlertedEvent struct {
	ID       string                 `json:"id"`
	Topic    string                 `json:"topic"`    // event topic
	Metadata map[string]string      `json:"metadata"` // event metadata
	Data     map[string]interface{} `json:"data"`     // event payload
}

type AlertDestination struct {
	ID         string          `json:"id" redis:"id"`
	TenantID   string          `json:"tenant_id" redis:"-"`
	Type       string          `json:"type" redis:"type"`
	Topics     models.Topics   `json:"topics" redis:"-"`
	Filter     models.Filter   `json:"filter,omitempty" redis:"-"`
	Config     models.Config   `json:"config" redis:"-"`
	Metadata   models.Metadata `json:"metadata,omitempty" redis:"-"`
	CreatedAt  time.Time       `json:"created_at" redis:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at" redis:"updated_at"`
	DisabledAt *time.Time      `json:"disabled_at" redis:"disabled_at"`
}

func AlertDestinationFromDestination(d *models.Destination) *AlertDestination {
	return &AlertDestination{
		ID:         d.ID,
		TenantID:   d.TenantID,
		Type:       d.Type,
		Topics:     d.Topics,
		Filter:     d.Filter,
		Config:     d.Config,
		Metadata:   d.Metadata,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
		DisabledAt: d.DisabledAt,
	}
}

// ConsecutiveFailureData represents the data needed for a consecutive failure alert
type ConsecutiveFailureData struct {
	TenantID               string                 `json:"tenant_id"`
	Event                  AlertedEvent           `json:"event"`
	MaxConsecutiveFailures int                    `json:"max_consecutive_failures"`
	ConsecutiveFailures    int                    `json:"consecutive_failures"`
	WillDisable            bool                   `json:"will_disable"`
	Destination            *AlertDestination      `json:"destination"`
	AttemptResponse        map[string]interface{} `json:"attempt_response"`
}

// ConsecutiveFailureAlert represents an alert for consecutive failures
type ConsecutiveFailureAlert struct {
	Topic     string                 `json:"topic"`
	Timestamp time.Time              `json:"timestamp"`
	Data      ConsecutiveFailureData `json:"data"`
}

// MarshalJSON implements json.Marshaler
func (a ConsecutiveFailureAlert) MarshalJSON() ([]byte, error) {
	type Alias ConsecutiveFailureAlert
	return json.Marshal(Alias(a))
}

// NewConsecutiveFailureAlert creates a new consecutive failure alert with defaults
func NewConsecutiveFailureAlert(data ConsecutiveFailureData) ConsecutiveFailureAlert {
	return ConsecutiveFailureAlert{
		Topic:     "alert.consecutive_failure",
		Timestamp: time.Now(),
		Data:      data,
	}
}

type httpAlertNotifier struct {
	client      *http.Client
	callbackURL string
	bearerToken string
}

// NewHTTPAlertNotifier creates a new HTTP-based alert notifier
func NewHTTPAlertNotifier(callbackURL string, opts ...NotifierOption) AlertNotifier {
	n := &httpAlertNotifier{
		client:      &http.Client{},
		callbackURL: callbackURL,
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

func (n *httpAlertNotifier) Notify(ctx context.Context, alert Alert) error {
	body, err := alert.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.callbackURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if n.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+n.bearerToken)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send alert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("alert callback failed with status %d", resp.StatusCode)
	}

	return nil
}
