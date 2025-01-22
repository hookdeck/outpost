package alert_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertNotifier_Notify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		handler      func(w http.ResponseWriter, r *http.Request)
		notifierOpts []alert.NotifierOption
		wantErr      bool
		errContains  string
	}{
		{
			name: "successful notification",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				// Read and verify request body
				var body map[string]interface{}
				err := json.NewDecoder(r.Body).Decode(&body)
				require.NoError(t, err)

				assert.Equal(t, "event.failed", body["topic"])
				assert.Equal(t, float64(10), body["disable_threshold"])
				assert.Equal(t, float64(5), body["consecutive_failures"])

				// Log raw JSON for debugging
				rawJSON, _ := json.Marshal(body)
				t.Logf("Raw JSON: %s", string(rawJSON))

				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:     true,
			errContains: "alert callback failed with status 500",
		},
		{
			name: "invalid response status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			wantErr:     true,
			errContains: "alert callback failed with status 400",
		},
		{
			name: "timeout exceeded",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			},
			notifierOpts: []alert.NotifierOption{alert.NotifierWithTimeout(50 * time.Millisecond)},
			wantErr:      true,
			errContains:  "context deadline exceeded",
		},
		{
			name: "successful notification with bearer token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Verify request headers
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				// Read and verify request body
				var body map[string]interface{}
				err := json.NewDecoder(r.Body).Decode(&body)
				require.NoError(t, err)

				assert.Equal(t, "event.failed", body["topic"])
				assert.Equal(t, float64(10), body["disable_threshold"])
				assert.Equal(t, float64(5), body["consecutive_failures"])

				w.WriteHeader(http.StatusOK)
			},
			notifierOpts: []alert.NotifierOption{alert.NotifierWithBearerToken("test-token")},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test server
			ts := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer ts.Close()

			// Create notifier
			notifier := alert.NewHTTPAlertNotifier(ts.URL, tt.notifierOpts...)

			// Create test alert
			dest := &models.Destination{ID: "dest_123", TenantID: "tenant_123"}
			alert := alert.Alert{
				Topic:               "event.failed",
				DisableThreshold:    10,
				ConsecutiveFailures: 5,
				Destination:         dest,
				Data: map[string]interface{}{
					"status": "error",
					"data":   map[string]any{"code": "ETIMEDOUT"},
				},
			}

			// Send alert
			err := notifier.Notify(context.Background(), alert)

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
