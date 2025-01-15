package alert_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertNotifier_Notify(t *testing.T) {
	tests := []struct {
		name          string
		alert         alert.Alert
		serverHandler http.HandlerFunc
		expectError   bool
	}{
		{
			name: "successful notification",
			alert: alert.Alert{
				Topic:               "event.failed",
				DisableThreshold:    10,
				ConsecutiveFailures: 5,
				Destination: &models.Destination{
					ID:       "dest_123",
					TenantID: "tenant_123",
				},
				Response: &alert.Response{
					Status: "error",
					Data: map[string]any{
						"code": "ETIMEDOUT",
					},
				},
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				// Read and verify body
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)

				// Print the raw JSON for debugging
				t.Logf("Raw JSON: %s", string(body))

				var receivedAlert alert.Alert
				err = json.Unmarshal(body, &receivedAlert)
				require.NoError(t, err)

				// Compare the original alert with the received one
				assert.Equal(t, "event.failed", receivedAlert.Topic)
				assert.Equal(t, 10, receivedAlert.DisableThreshold)
				assert.Equal(t, int64(5), receivedAlert.ConsecutiveFailures)
				require.NotNil(t, receivedAlert.Destination)
				assert.Equal(t, "dest_123", receivedAlert.Destination.ID)
				assert.Equal(t, "tenant_123", receivedAlert.Destination.TenantID)
				require.NotNil(t, receivedAlert.Response)
				assert.Equal(t, "error", receivedAlert.Response.Status)
				assert.Equal(t, "ETIMEDOUT", receivedAlert.Response.Data["code"])

				w.WriteHeader(http.StatusOK)
			},
			expectError: false,
		},
		{
			name:  "server error",
			alert: alert.Alert{},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectError: true,
		},
		{
			name:  "invalid response status",
			alert: alert.Alert{},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			// Create notifier with test server URL
			notifier := alert.NewHTTPAlertNotifier(server.URL)

			// Send notification
			err := notifier.Notify(context.Background(), tt.alert)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
