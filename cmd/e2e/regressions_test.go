package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
)

// regressionHTTPClient is a simple HTTP helper for standalone regression tests.
type regressionHTTPClient struct {
	client *http.Client
	apiKey string
}

func newRegressionHTTPClient(apiKey string) *regressionHTTPClient {
	return &regressionHTTPClient{
		client: &http.Client{Timeout: 10 * time.Second},
		apiKey: apiKey,
	}
}

func (c *regressionHTTPClient) doJSON(t *testing.T, method, url string, body any, result any) int {
	t.Helper()
	return c.doJSONWithAuth(t, method, url, "Bearer "+c.apiKey, body, result)
}

func (c *regressionHTTPClient) doJSONRaw(t *testing.T, method, url string, body any, result any) int {
	t.Helper()
	return c.doJSONWithAuth(t, method, url, "", body, result)
}

func (c *regressionHTTPClient) doJSONWithAuth(t *testing.T, method, url string, authHeader string, body any, result any) int {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := c.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if result != nil {
		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		if len(respBody) > 0 {
			require.NoError(t, json.Unmarshal(respBody, result))
		}
	}

	return resp.StatusCode
}

// TestE2E_Regression_AutoDisableWithoutCallbackURL tests issue #596:
// ALERT_AUTO_DISABLE_DESTINATION=true works without any callback configured.
func TestE2E_Regression_AutoDisableWithoutCallbackURL(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)
	mockServerBaseURL := testinfra.GetMockServer(t)

	cfg := configs.Basic(t, configs.BasicOpts{
		LogStorage: configs.LogStorageTypePostgres,
	})
	cfg.Alert.AutoDisableDestination = true
	cfg.Alert.ConsecutiveFailureCount = 20

	require.NoError(t, cfg.Validate(config.Flags{}))
	configs.ApplyMigrations(t, &cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	tenantID := fmt.Sprintf("tenant_%d", time.Now().UnixNano())
	destinationID := fmt.Sprintf("dest_%d", time.Now().UnixNano())
	secret := testSecret

	// Create tenant
	status := client.doJSON(t, http.MethodPut, apiURL+"/tenants/"+tenantID, nil, nil)
	require.Equal(t, 201, status, "failed to create tenant")

	// Configure mock server destination to return errors
	status = client.doJSONRaw(t, http.MethodPut, mockServerBaseURL+"/destinations", map[string]any{
		"id":   destinationID,
		"type": "webhook",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
		},
		"credentials": map[string]any{
			"secret": secret,
		},
	}, nil)
	require.Equal(t, 200, status, "failed to configure mock server")

	// Create destination
	status = client.doJSON(t, http.MethodPost, apiURL+"/tenants/"+tenantID+"/destinations", map[string]any{
		"id":     destinationID,
		"type":   "webhook",
		"topics": "*",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
		},
		"credentials": map[string]any{
			"secret": secret,
		},
	}, nil)
	require.Equal(t, 201, status, "failed to create destination")

	// Publish 21 events that will fail
	for i := 0; i < 21; i++ {
		status = client.doJSON(t, http.MethodPost, apiURL+"/publish", map[string]any{
			"tenant_id":          tenantID,
			"topic":              "user.created",
			"eligible_for_retry": false,
			"metadata": map[string]any{
				"should_err": "true",
			},
			"data": map[string]any{
				"index": i,
			},
		}, nil)
		require.Equal(t, 202, status, "failed to publish event %d", i)
	}

	// Poll until destination is disabled (replaces flaky time.Sleep)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var dest map[string]any
		status = client.doJSON(t, http.MethodGet, apiURL+"/tenants/"+tenantID+"/destinations/"+destinationID, nil, &dest)
		require.Equal(t, 200, status, "failed to get destination")
		if dest["disabled_at"] != nil {
			return // success
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("timed out waiting for destination to be disabled (disabled_at should not be null) - issue #596")
}

// TestE2E_ManualRetryScheduleInteraction is a standalone E2E test that verifies
// manual retries correctly interact with the automatic retry schedule.
//
// Setup:
//   - Isolated outpost instance with a scheduled backoff: [2s, 4s] (2 retries, 3 max attempts)
//   - Fast log flush (immediate) so logstore queries in the retry handler are reliable
//   - Fast retry poll (50ms) so retries fire promptly after their delay expires
//   - A destination that always fails (should_err metadata)
//
// Timeline:
//
//	t=0s     Publish always-failing event
//	t~0s     Attempt 1 (auto) fails → auto retry scheduled at tier 0 (2s)
//	t<2s     Trigger manual retry via POST /retry
//	t~0s     Attempt 2 (manual) fails → cancels pending 2s retry, schedules at tier 1 (4s)
//	         Key assertion: NO attempt 3 arrives at t~2s (the 2s retry was canceled)
//	t~4s     Attempt 3 (auto) fires → fails → budget exhausted (3 = 1 initial + 2 retries)
//	         Key assertion: no attempt 4 arrives (budget exhausted)
//
// What this proves:
//  1. Manual retry gets correct sequential attempt_number (2, derived from logstore)
//  2. Manual retry failure cancels the pending automatic retry (the 2s tier never fires)
//  3. Manual retry failure schedules the next tier (4s) — the schedule advances
//  4. Budget is correctly exhausted after 3 total attempts (1 auto + 1 manual + 1 auto)
//  5. No further retries after budget exhaustion
func TestE2E_ManualRetryScheduleInteraction(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)
	mockServerBaseURL := testinfra.GetMockServer(t)

	cfg := configs.Basic(t, configs.BasicOpts{
		LogStorage: configs.LogStorageTypeClickHouse,
	})

	// Scheduled backoff: 2s, 4s — enough window to trigger manual retry before
	// the first auto retry fires at 2s, while keeping the test under 10s
	cfg.RetrySchedule = []int{2, 4}
	cfg.RetryPollBackoffMs = 50
	cfg.LogBatchThresholdSeconds = 0 // Immediate flush so logstore queries work

	require.NoError(t, cfg.Validate(config.Flags{}))
	configs.ApplyMigrations(t, &cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	tenantID := fmt.Sprintf("tenant_manual_%d", time.Now().UnixNano())
	destinationID := fmt.Sprintf("dest_manual_%d", time.Now().UnixNano())
	eventID := fmt.Sprintf("evt_manual_%d", time.Now().UnixNano())
	secret := testSecret

	// Create tenant
	status := client.doJSON(t, http.MethodPut, apiURL+"/tenants/"+tenantID, nil, nil)
	require.Equal(t, 201, status, "failed to create tenant")

	// Configure mock server destination (always returns error)
	status = client.doJSONRaw(t, http.MethodPut, mockServerBaseURL+"/destinations", map[string]any{
		"id":   destinationID,
		"type": "webhook",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
		},
		"credentials": map[string]any{
			"secret": secret,
		},
	}, nil)
	require.Equal(t, 200, status, "failed to configure mock server")

	// Create destination
	status = client.doJSON(t, http.MethodPost, apiURL+"/tenants/"+tenantID+"/destinations", map[string]any{
		"id":     destinationID,
		"type":   "webhook",
		"topics": "*",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
		},
		"credentials": map[string]any{
			"secret": secret,
		},
	}, nil)
	require.Equal(t, 201, status, "failed to create destination")

	// Publish always-failing event with retry enabled
	status = client.doJSON(t, http.MethodPost, apiURL+"/publish", map[string]any{
		"id":                 eventID,
		"tenant_id":          tenantID,
		"topic":              "user.created",
		"eligible_for_retry": true,
		"metadata": map[string]any{
			"should_err": "true",
		},
		"data": map[string]any{
			"test": "manual-retry-schedule",
		},
	}, nil)
	require.Equal(t, 202, status, "failed to publish event")

	// Wait for attempt 1 (initial auto delivery, fails)
	// TODO: extract into a shared standalone poll helper (not tied to basicSuite)
	pollAttempts := func(t *testing.T, minCount int, timeout time.Duration) []map[string]any {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			var resp struct {
				Models []map[string]any `json:"models"`
			}
			s := client.doJSON(t, http.MethodGet,
				apiURL+"/attempts?tenant_id="+tenantID+"&event_id="+eventID+"&dir=asc", nil, &resp)
			if s == http.StatusOK && len(resp.Models) >= minCount {
				return resp.Models
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %d attempts", minCount)
		return nil
	}

	attempts := pollAttempts(t, 1, 5*time.Second)
	require.Len(t, attempts, 1, "should have exactly 1 attempt (initial delivery)")
	require.Equal(t, float64(1), attempts[0]["attempt_number"], "initial attempt should be attempt_number=1")

	// Trigger manual retry BEFORE the 3s auto retry fires
	// This should cancel the pending 3s retry and schedule a 6s retry
	status = client.doJSON(t, http.MethodPost, apiURL+"/retry", map[string]any{
		"event_id":       eventID,
		"destination_id": destinationID,
	}, nil)
	require.Equal(t, 202, status, "manual retry should be accepted")

	// Wait for attempt 2 (manual retry, fails)
	attempts = pollAttempts(t, 2, 5*time.Second)
	require.Equal(t, float64(2), attempts[1]["attempt_number"], "manual retry should be attempt_number=2")
	require.Equal(t, true, attempts[1]["manual"], "attempt 2 should be manual=true")

	// Verify the 2s auto retry was canceled: wait 2.5s and confirm no attempt 3 arrived
	// (if the cancel failed, a 3rd attempt would appear at ~t=2s)
	time.Sleep(2500 * time.Millisecond)
	var midResp struct {
		Models []map[string]any `json:"models"`
	}
	client.doJSON(t, http.MethodGet,
		apiURL+"/attempts?tenant_id="+tenantID+"&event_id="+eventID, nil, &midResp)
	require.Len(t, midResp.Models, 2,
		"should still have only 2 attempts at t~2.5s (2s auto retry was canceled)")

	// Wait for attempt 3 (auto retry at tier 1 = 4s from manual retry time)
	attempts = pollAttempts(t, 3, 5*time.Second)
	require.Equal(t, float64(3), attempts[2]["attempt_number"], "auto retry should be attempt_number=3")
	require.NotEqual(t, true, attempts[2]["manual"], "attempt 3 should be auto (not manual)")

	// Verify budget exhaustion: no attempt 4 should arrive
	// Schedule has 2 entries → 2 retries max → 3 total attempts. Budget is exhausted.
	time.Sleep(2 * time.Second)
	var finalResp struct {
		Models []map[string]any `json:"models"`
	}
	client.doJSON(t, http.MethodGet,
		apiURL+"/attempts?tenant_id="+tenantID+"&event_id="+eventID+"&dir=asc", nil, &finalResp)
	require.Len(t, finalResp.Models, 3,
		"should have exactly 3 attempts (budget exhausted: 1 initial + 2 retries)")

	// Assert the full picture: attempt_number is sequential, manual flag is correct
	expected := []struct {
		attemptNumber float64
		manual        bool
	}{
		{1, false}, // initial auto delivery
		{2, true},  // manual retry
		{3, false}, // auto retry (advanced tier)
	}
	for i, exp := range expected {
		atm := finalResp.Models[i]
		require.Equal(t, exp.attemptNumber, atm["attempt_number"],
			"attempt %d: expected attempt_number=%v", i+1, exp.attemptNumber)
		manual, _ := atm["manual"].(bool)
		require.Equal(t, exp.manual, manual,
			"attempt %d: expected manual=%v", i+1, exp.manual)
	}
}

// TestE2E_Regression_RetryRaceCondition verifies that retries are not lost when
// the retry scheduler queries logstore before the event has been persisted.
func TestE2E_Regression_RetryRaceCondition(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)
	mockServerBaseURL := testinfra.GetMockServer(t)

	cfg := configs.Basic(t, configs.BasicOpts{
		LogStorage: configs.LogStorageTypeClickHouse,
	})

	// SLOW log persistence: batch won't flush for 5 seconds
	cfg.LogBatchThresholdSeconds = 5
	cfg.LogBatchSize = 10000

	// FAST retry: retry fires after ~1 second
	cfg.RetryIntervalSeconds = 1
	cfg.RetryPollBackoffMs = 50
	cfg.RetryMaxLimit = 5
	cfg.RetryVisibilityTimeoutSeconds = 2

	require.NoError(t, cfg.Validate(config.Flags{}))
	configs.ApplyMigrations(t, &cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	tenantID := fmt.Sprintf("tenant_race_%d", time.Now().UnixNano())
	destinationID := fmt.Sprintf("dest_race_%d", time.Now().UnixNano())
	secret := testSecret

	// Create tenant
	status := client.doJSON(t, http.MethodPut, apiURL+"/tenants/"+tenantID, nil, nil)
	require.Equal(t, 201, status, "failed to create tenant")

	// Configure mock server destination
	status = client.doJSONRaw(t, http.MethodPut, mockServerBaseURL+"/destinations", map[string]any{
		"id":   destinationID,
		"type": "webhook",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
		},
		"credentials": map[string]any{
			"secret": secret,
		},
	}, nil)
	require.Equal(t, 200, status, "failed to configure mock server")

	// Create destination
	status = client.doJSON(t, http.MethodPost, apiURL+"/tenants/"+tenantID+"/destinations", map[string]any{
		"id":     destinationID,
		"type":   "webhook",
		"topics": "*",
		"config": map[string]any{
			"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
		},
		"credentials": map[string]any{
			"secret": secret,
		},
	}, nil)
	require.Equal(t, 201, status, "failed to create destination")

	// Publish event that will always fail (should_err: true)
	status = client.doJSON(t, http.MethodPost, apiURL+"/publish", map[string]any{
		"tenant_id":          tenantID,
		"topic":              "user.created",
		"eligible_for_retry": true,
		"metadata": map[string]any{
			"should_err": "true",
		},
		"data": map[string]any{
			"test": "race-condition-test",
		},
	}, nil)
	require.Equal(t, 202, status, "failed to publish event")

	// Poll for at least 2 delivery attempts (initial + retry after event persisted)
	deadline := time.Now().Add(15 * time.Second)
	var eventCount int
	for time.Now().Before(deadline) {
		resp, err := http.Get(mockServerBaseURL + "/destinations/" + destinationID + "/events")
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				var events []any
				if json.Unmarshal(body, &events) == nil {
					eventCount = len(events)
				}
			}
			resp.Body.Close()
			if eventCount >= 2 {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.GreaterOrEqual(t, eventCount, 2,
		"expected multiple delivery attempts (initial + retry after event persisted)")
}
