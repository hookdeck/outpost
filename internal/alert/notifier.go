package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

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
