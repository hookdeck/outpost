package e2e_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
)

// TestE2E_Regression_WebhookResponseBodyCap verifies, end to end, that
// DESTINATIONS_WEBHOOK_MAX_RESPONSE_BODY_BYTES bounds the response body stored
// on a delivery attempt. A response larger than the cap previously produced an
// oversized log message that failed to publish and retried forever; with the cap
// the body is replaced by a placeholder so the attempt persists normally.
//
// This exercises the full path: config -> provider option -> response capture ->
// logstore -> /attempts. It does not assert anything about the queue itself; the
// point is that the stored body is bounded.
func TestE2E_Regression_WebhookResponseBodyCap(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const maxBytes = 1024
	const placeholder = "Response body exceeded 1024 bytes and was not stored"

	// Two webhook receivers: one returns a body over the cap, one under it.
	bigServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", 200_000)))
	}))
	defer bigServer.Close()
	smallBody := "ok"
	smallServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(smallBody))
	}))
	defer smallServer.Close()

	cfg := configs.Basic(t, configs.BasicOpts{LogStorage: configs.LogStorageTypeClickHouse})
	cfg.Destinations.Webhook.MaxResponseBodyBytes = maxBytes
	cfg.LogBatchThresholdSeconds = 0 // immediate flush so /attempts is reliable
	require.NoError(t, cfg.Validate(config.Flags{}))
	configs.ApplyMigrations(t, &cfg)

	appDone := make(chan struct{})
	go func() {
		defer close(appDone)
		application := app.New(&cfg)
		if err := application.Run(ctx); err != nil {
			log.Println("Application stopped:", err)
		}
	}()
	defer func() {
		cancel()
		<-appDone
	}()

	waitForHealthy(t, cfg.APIPort, 5*time.Second)

	client := newRegressionHTTPClient(cfg.APIKey)
	apiURL := fmt.Sprintf("http://localhost:%d/api/v1", cfg.APIPort)

	tenantID := fmt.Sprintf("tenant_respcap_%d", time.Now().UnixNano())
	status := client.doJSON(t, http.MethodPut, apiURL+"/tenants/"+tenantID, nil, nil)
	require.Equal(t, 201, status, "failed to create tenant")

	// createWebhook creates a webhook destination pointing at url and returns its ID.
	createWebhook := func(url string) string {
		destinationID := fmt.Sprintf("dest_%d", time.Now().UnixNano())
		status := client.doJSON(t, http.MethodPost, apiURL+"/tenants/"+tenantID+"/destinations", map[string]any{
			"id":          destinationID,
			"type":        "webhook",
			"topics":      "*",
			"config":      map[string]any{"url": url},
			"credentials": map[string]any{"secret": testSecret},
		}, nil)
		require.Equal(t, 201, status, "failed to create webhook destination")
		return destinationID
	}

	bigDest := createWebhook(bigServer.URL)
	smallDest := createWebhook(smallServer.URL)

	// One event fans out to both destinations (both subscribe to "*"); we then
	// isolate each delivery by destination_id.
	eventID := fmt.Sprintf("evt_%d", time.Now().UnixNano())
	status = client.doJSON(t, http.MethodPost, apiURL+"/publish", map[string]any{
		"id":                 eventID,
		"tenant_id":          tenantID,
		"topic":              "user.created",
		"eligible_for_retry": false,
		"data":               map[string]any{"hello": "world"},
	}, nil)
	require.Equal(t, 202, status, "failed to publish event")

	// pollResponseBody waits for a successful attempt on destinationID and returns
	// its stored body.
	pollResponseBody := func(destinationID string) string {
		attemptsURL := apiURL + "/attempts?tenant_id=" + tenantID + "&event_id=" + eventID +
			"&destination_id=" + destinationID + "&dir=asc&include=response_data"
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			var resp struct {
				Models []map[string]any `json:"models"`
			}
			s := client.doJSON(t, http.MethodGet, attemptsURL, nil, &resp)
			if s == http.StatusOK && len(resp.Models) >= 1 {
				atm := resp.Models[0]
				require.Equal(t, "success", atm["status"], "delivery should succeed")
				rd, ok := atm["response_data"].(map[string]any)
				require.True(t, ok, "attempt should carry response_data")
				body, _ := rd["body"].(string)
				return body
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for attempt of destination %s", destinationID)
		return ""
	}

	// Over the cap: body is replaced wholesale with the placeholder.
	require.Equal(t, placeholder, pollResponseBody(bigDest),
		"oversized response body should be replaced with the placeholder")

	// Under the cap: body is stored verbatim — the cap only bites when exceeded.
	require.Equal(t, smallBody, pollResponseBody(smallDest),
		"response body under the cap should be stored verbatim")
}
