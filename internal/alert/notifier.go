package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hookdeck/outpost/internal/models"
)

// AlertNotifier sends alerts to configured destinations
type AlertNotifier interface {
	// Notify sends an alert to the configured callback URL
	Notify(ctx context.Context, alert Alert) error
}

// Alert represents an alert that will be sent to the callback URL
type Alert struct {
	Topic               string              `json:"topic"`
	DisableThreshold    int                 `json:"disable_threshold"`
	ConsecutiveFailures int64               `json:"consecutive_failures"`
	Destination         *models.Destination `json:"destination"`
	Response            *Response           `json:"response,omitempty"`
}

type httpAlertNotifier struct {
	client      *http.Client
	callbackURL string
}

// NewHTTPAlertNotifier creates a new HTTP-based alert notifier
func NewHTTPAlertNotifier(callbackURL string) AlertNotifier {
	return &httpAlertNotifier{
		client:      http.DefaultClient,
		callbackURL: callbackURL,
	}
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
