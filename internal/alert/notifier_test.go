package alert_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertNotifier_Notify(t *testing.T) {
	t.Parallel()

	t.Run("successful notification", func(t *testing.T) {
		t.Parallel()
		var called atomic.Bool

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called.Store(true)
			// Verify request
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			// Read and verify request body
			var body map[string]any
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "alert.consecutive_failure", body["topic"])
			data := body["data"].(map[string]any)
			assert.Equal(t, float64(10), data["max_consecutive_failures"])
			assert.Equal(t, float64(5), data["consecutive_failures"])
			assert.Equal(t, true, data["will_disable"])

			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		notifier := alert.NewHTTPAlertNotifier(ts.URL)
		dest := &alert.AlertDestination{ID: "dest_123", TenantID: "tenant_123"}
		testAlert := alert.NewConsecutiveFailureAlert(alert.ConsecutiveFailureData{
			MaxConsecutiveFailures: 10,
			ConsecutiveFailures:    5,
			WillDisable:            true,
			Destination:            dest,
			AttemptResponse: map[string]any{
				"status": "error",
				"data":   map[string]any{"code": "ETIMEDOUT"},
			},
		})

		err := notifier.Notify(context.Background(), testAlert)
		require.NoError(t, err)
		assert.True(t, called.Load(), "handler should have been called")
	})

	t.Run("successful notification with bearer token", func(t *testing.T) {
		t.Parallel()
		var called atomic.Bool

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called.Store(true)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		notifier := alert.NewHTTPAlertNotifier(ts.URL, alert.NotifierWithBearerToken("test-token"))
		dest := &alert.AlertDestination{ID: "dest_123", TenantID: "tenant_123"}
		testAlert := alert.NewConsecutiveFailureAlert(alert.ConsecutiveFailureData{
			MaxConsecutiveFailures: 10,
			ConsecutiveFailures:    5,
			WillDisable:            true,
			Destination:            dest,
		})

		err := notifier.Notify(context.Background(), testAlert)
		require.NoError(t, err)
		assert.True(t, called.Load(), "handler should have been called")
	})

	t.Run("server error returns error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		notifier := alert.NewHTTPAlertNotifier(ts.URL)
		dest := &alert.AlertDestination{ID: "dest_123", TenantID: "tenant_123"}
		testAlert := alert.NewConsecutiveFailureAlert(alert.ConsecutiveFailureData{
			Destination: dest,
		})

		err := notifier.Notify(context.Background(), testAlert)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 500")
	})

	t.Run("timeout returns error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		notifier := alert.NewHTTPAlertNotifier(ts.URL, alert.NotifierWithTimeout(50*time.Millisecond))
		dest := &alert.AlertDestination{ID: "dest_123", TenantID: "tenant_123"}
		testAlert := alert.NewConsecutiveFailureAlert(alert.ConsecutiveFailureData{
			Destination: dest,
		})

		err := notifier.Notify(context.Background(), testAlert)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send alert")
	})
}
