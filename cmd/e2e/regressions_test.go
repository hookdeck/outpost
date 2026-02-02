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
// ALERT_AUTO_DISABLE_DESTINATION=true without ALERT_CALLBACK_URL set.
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
	cfg.Alert.CallbackURL = ""
	cfg.Alert.AutoDisableDestination = true
	cfg.Alert.ConsecutiveFailureCount = 20

	require.NoError(t, cfg.Validate(config.Flags{}))

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
