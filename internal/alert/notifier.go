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

// Alert represents an alert that will be sent to the callback URL
type Alert struct {
	Topic               string                 `json:"topic"`
	DisableThreshold    int                    `json:"disable_threshold"`
	ConsecutiveFailures int                    `json:"consecutive_failures"`
	Destination         *models.Destination    `json:"destination"`
	Data                map[string]interface{} `json:"data"`
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
	// Marshal alert to JSON
	body, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.callbackURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if n.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+n.bearerToken)
	}

	// Send request
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send alert: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 400 {
		return fmt.Errorf("alert callback failed with status %d", resp.StatusCode)
	}

	return nil
}
