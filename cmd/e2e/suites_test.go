package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/alert"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// waitForHealthy polls the /healthz endpoint until it returns 200 or times out.
func waitForHealthy(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	healthURL := fmt.Sprintf("http://localhost:%d/healthz", port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for health check at %s", healthURL)
}

// waitForDeliveries polls until at least minCount deliveries exist for the given path.
func (s *e2eSuite) waitForDeliveries(t *testing.T, path string, minCount int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := s.client.Do(s.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   path,
		}))
		if err == nil && resp.StatusCode == http.StatusOK {
			if body, ok := resp.Body.(map[string]interface{}); ok {
				if data, ok := body["data"].([]interface{}); ok && len(data) >= minCount {
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d deliveries at %s", minCount, path)
}

// waitForDestinationDisabled polls until the destination has disabled_at set (non-null).
func (s *e2eSuite) waitForDestinationDisabled(t *testing.T, tenantID, destinationID string, timeout time.Duration) {
	t.Helper()
	path := "/tenants/" + tenantID + "/destinations/" + destinationID
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := s.client.Do(s.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   path,
		}))
		if err == nil && resp.StatusCode == http.StatusOK {
			if body, ok := resp.Body.(map[string]interface{}); ok {
				if disabledAt, exists := body["disabled_at"]; exists && disabledAt != nil {
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for destination %s to be disabled", destinationID)
}

// waitForMockServerEvents polls the mock server until at least minCount events exist for the destination.
func (s *e2eSuite) waitForMockServerEvents(t *testing.T, destinationID string, minCount int, timeout time.Duration) {
	t.Helper()
	path := "/destinations/" + destinationID + "/events"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := s.client.Do(httpclient.Request{
			Method:  httpclient.MethodGET,
			BaseURL: s.mockServerBaseURL,
			Path:    path,
		})
		if err == nil && resp.StatusCode == http.StatusOK {
			if events, ok := resp.Body.([]interface{}); ok && len(events) >= minCount {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events at mock server %s", minCount, path)
}

type e2eSuite struct {
	ctx               context.Context
	cancel            context.CancelFunc
	config            config.Config
	mockServerBaseURL string
	mockServerInfra   *testinfra.MockServerInfra
	cleanup           func()
	client            httpclient.Client
	appDone           chan struct{}
}

func (suite *e2eSuite) SetupSuite() {
	ctx, cancel := context.WithCancel(context.Background())
	suite.ctx = ctx
	suite.cancel = cancel
	suite.appDone = make(chan struct{})
	suite.client = httpclient.New(fmt.Sprintf("http://localhost:%d/api/v1", suite.config.APIPort), suite.config.APIKey)
	go func() {
		defer close(suite.appDone)
		application := app.New(&suite.config)
		if err := application.Run(suite.ctx); err != nil {
			log.Println("Application failed to run", err)
		}
	}()
}

func (s *e2eSuite) TearDownSuite() {
	if s.cancel != nil {
		s.cancel()
		// Wait for application to fully shut down before cleaning up resources
		<-s.appDone
	}
	s.cleanup()
}

func (s *e2eSuite) AuthRequest(req httpclient.Request) httpclient.Request {
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Authorization"] = fmt.Sprintf("Bearer %s", s.config.APIKey)
	return req
}

func (s *e2eSuite) AuthJWTRequest(req httpclient.Request, token string) httpclient.Request {
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Authorization"] = fmt.Sprintf("Bearer %s", token)
	return req
}

func (suite *e2eSuite) RunAPITests(t *testing.T, tests []APITest) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t, suite.client)
		})
	}
}

// MockServerPoll configures polling for the mock server before running the test.
type MockServerPoll struct {
	BaseURL  string        // Mock server base URL
	DestID   string        // Destination ID to poll
	MinCount int           // Minimum events to wait for
	Timeout  time.Duration // Poll timeout
}

type APITest struct {
	Name     string
	Delay    time.Duration   // Deprecated: use WaitForMockEvents instead
	WaitFor  *MockServerPoll // Poll mock server before running test
	Request  httpclient.Request
	Expected APITestExpectation
}

type APITestExpectation struct {
	Match    *httpclient.Response
	Validate map[string]interface{}
}

func (test *APITest) Run(t *testing.T, client httpclient.Client) {
	t.Helper()

	// Poll mock server if configured (preferred over Delay)
	if test.WaitFor != nil {
		w := test.WaitFor
		path := "/destinations/" + w.DestID + "/events"
		deadline := time.Now().Add(w.Timeout)
		for time.Now().Before(deadline) {
			resp, err := client.Do(httpclient.Request{
				Method:  httpclient.MethodGET,
				BaseURL: w.BaseURL,
				Path:    path,
			})
			if err == nil && resp.StatusCode == http.StatusOK {
				if events, ok := resp.Body.([]interface{}); ok && len(events) >= w.MinCount {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	} else if test.Delay > 0 {
		time.Sleep(test.Delay)
	}

	resp, err := client.Do(test.Request)
	require.NoError(t, err)

	if test.Expected.Match != nil {
		require.Equal(t, test.Expected.Match.StatusCode, resp.StatusCode)
		if test.Expected.Match.Body != nil {
			require.True(t, resp.MatchBody(test.Expected.Match.Body), "expected body %s, got %s", test.Expected.Match.Body, resp.Body)
		}
	}

	if test.Expected.Validate != nil {
		c := jsonschema.NewCompiler()
		require.NoError(t, c.AddResource("schema.json", test.Expected.Validate))
		schema, err := c.Compile("schema.json")
		require.NoError(t, err, "failed to compile schema: %v", err)
		respStr, _ := json.Marshal(resp)
		var respJSON map[string]interface{}
		require.NoError(t, json.Unmarshal(respStr, &respJSON), "failed to parse response: %v", err)
		validationErr := schema.Validate(respJSON)
		require.NoError(t, validationErr, "response validation failed: %v: %s", validationErr, respJSON)
	}
}

type basicSuite struct {
	suite.Suite
	e2eSuite
	logStorageType configs.LogStorageType
	redisConfig    *redis.RedisConfig // Optional Redis config override
	deploymentID   string             // Optional deployment ID
	hasRediSearch  bool               // Whether the Redis backend supports RediSearch (only RedisStack)
	alertServer    *alert.AlertMockServer
	failed         bool // Fail-fast: skip remaining tests after first failure
}

func (s *basicSuite) BeforeTest(suiteName, testName string) {
	if s.failed {
		s.T().Skip("skipping due to previous test failure")
	}
}

func (s *basicSuite) AfterTest(suiteName, testName string) {
	if s.T().Failed() {
		s.failed = true
	}
}

func (suite *basicSuite) SetupSuite() {
	t := suite.T()
	testinfraCleanup := testinfra.Start(t)
	defer t.Cleanup(testinfraCleanup)
	gin.SetMode(gin.TestMode)
	mockServerBaseURL := testinfra.GetMockServer(t)

	// Setup alert mock server
	alertServer := alert.NewAlertMockServer()
	require.NoError(t, alertServer.Start())
	suite.alertServer = alertServer

	// Configure alert callback URL
	cfg := configs.Basic(t, configs.BasicOpts{
		LogStorage:   suite.logStorageType,
		RedisConfig:  suite.redisConfig,
		DeploymentID: suite.deploymentID,
	})
	cfg.Alert.CallbackURL = alertServer.GetCallbackURL()

	require.NoError(t, cfg.Validate(config.Flags{}))

	suite.e2eSuite = e2eSuite{
		config:            cfg,
		mockServerBaseURL: mockServerBaseURL,
		mockServerInfra:   testinfra.NewMockServerInfra(mockServerBaseURL),
		cleanup: func() {
			if err := alertServer.Stop(); err != nil {
				t.Logf("failed to stop alert server: %v", err)
			}
		},
	}
	suite.e2eSuite.SetupSuite()

	// wait for outpost services to start
	waitForHealthy(t, cfg.APIPort, 5*time.Second)
}

func (s *basicSuite) TearDownSuite() {
	s.e2eSuite.TearDownSuite()
}

// =============================================================================
// Default E2E Test Suites (always run)
// =============================================================================
// These suites test the primary supported configuration: Dragonfly + ClickHouse.
// They run in parallel by default.

// TestE2E tests the main configuration: Dragonfly (Redis) + ClickHouse (log storage).
func TestE2E(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(t),
	})
}

// TestE2E_WithDeploymentID tests multi-tenancy with deployment ID prefix.
func TestE2E_WithDeploymentID(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(t),
		deploymentID:   "dp_e2e_test",
	})
}

// =============================================================================
// Compatibility Test Suites (TESTCOMPAT=1 only)
// =============================================================================
// These suites test alternative backend configurations for compatibility.
// Run with: TESTCOMPAT=1 make test/e2e

// TestE2E_Compat_Postgres tests Postgres as log storage backend.
func TestE2E_Compat_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testutil.SkipUnlessCompat(t)
	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypePostgres,
		redisConfig:    testinfra.NewDragonflyStackConfig(t),
	})
}

// TestE2E_Compat_RedisStack tests Redis Stack as the Redis backend.
func TestE2E_Compat_RedisStack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testutil.SkipUnlessCompat(t)
	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewRedisStackConfig(t),
	})
}

// TestE2E_Compat_RedisCluster tests Redis Cluster mode.
// Requires TEST_REDIS_CLUSTER_URL environment variable.
func TestE2E_Compat_RedisCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testutil.SkipUnlessCompat(t)

	redisConfig := configs.CreateRedisClusterConfig(t)
	if redisConfig == nil {
		t.Skip("skipping Redis cluster test (TEST_REDIS_CLUSTER_URL not set)")
	}

	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    redisConfig,
	})
}

// =============================================================================
// Regression Tests
// =============================================================================
// Standalone tests for specific issues/scenarios.

// TestE2E_Regression_AutoDisableWithoutCallbackURL tests issue #596:
// ALERT_AUTO_DISABLE_DESTINATION=true without ALERT_CALLBACK_URL set.
func TestE2E_Regression_AutoDisableWithoutCallbackURL(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	// Setup infrastructure
	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)
	mockServerBaseURL := testinfra.GetMockServer(t)

	// Configure WITHOUT alert callback URL (the issue #596 scenario)
	cfg := configs.Basic(t, configs.BasicOpts{
		LogStorage: configs.LogStorageTypePostgres,
	})
	cfg.Alert.CallbackURL = ""              // No callback URL
	cfg.Alert.AutoDisableDestination = true // Auto-disable enabled
	cfg.Alert.ConsecutiveFailureCount = 20  // Default threshold

	require.NoError(t, cfg.Validate(config.Flags{}))

	// Start application
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

	// Wait for services to start
	waitForHealthy(t, cfg.APIPort, 5*time.Second)

	// Setup test client
	client := httpclient.New(fmt.Sprintf("http://localhost:%d/api/v1", cfg.APIPort), cfg.APIKey)
	mockServerInfra := testinfra.NewMockServerInfra(mockServerBaseURL)

	// Test data
	tenantID := fmt.Sprintf("tenant_%d", time.Now().UnixNano())
	destinationID := fmt.Sprintf("dest_%d", time.Now().UnixNano())
	secret := "testsecret1234567890abcdefghijklmnop"

	// Create tenant
	resp, err := client.Do(httpclient.Request{
		Method:  httpclient.MethodPUT,
		Path:    "/tenants/" + tenantID,
		Headers: map[string]string{"Authorization": "Bearer " + cfg.APIKey},
	})
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode, "failed to create tenant")

	// Configure mock server destination to return errors
	resp, err = client.Do(httpclient.Request{
		Method:  httpclient.MethodPUT,
		BaseURL: mockServerBaseURL,
		Path:    "/destinations",
		Body: map[string]interface{}{
			"id":   destinationID,
			"type": "webhook",
			"config": map[string]interface{}{
				"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
			},
			"credentials": map[string]interface{}{
				"secret": secret,
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "failed to configure mock server")

	// Create destination
	resp, err = client.Do(httpclient.Request{
		Method:  httpclient.MethodPOST,
		Path:    "/tenants/" + tenantID + "/destinations",
		Headers: map[string]string{"Authorization": "Bearer " + cfg.APIKey},
		Body: map[string]interface{}{
			"id":     destinationID,
			"type":   "webhook",
			"topics": "*",
			"config": map[string]interface{}{
				"url": fmt.Sprintf("%s/webhook/%s", mockServerBaseURL, destinationID),
			},
			"credentials": map[string]interface{}{
				"secret": secret,
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode, "failed to create destination")

	// Publish 21 events that will fail (1 more than threshold to test idempotency)
	for i := 0; i < 21; i++ {
		resp, err = client.Do(httpclient.Request{
			Method:  httpclient.MethodPOST,
			Path:    "/publish",
			Headers: map[string]string{"Authorization": "Bearer " + cfg.APIKey},
			Body: map[string]interface{}{
				"tenant_id":          tenantID,
				"topic":              "user.created",
				"eligible_for_retry": false,
				"metadata": map[string]any{
					"should_err": "true",
				},
				"data": map[string]any{
					"index": i,
				},
			},
		})
		require.NoError(t, err)
		require.Equal(t, 202, resp.StatusCode, "failed to publish event %d", i)
	}

	// Wait for deliveries to be processed
	time.Sleep(time.Second)

	// Check if destination is disabled
	resp, err = client.Do(httpclient.Request{
		Method:  httpclient.MethodGET,
		Path:    "/tenants/" + tenantID + "/destinations/" + destinationID,
		Headers: map[string]string{"Authorization": "Bearer " + cfg.APIKey},
	})
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "failed to get destination")

	// Parse response to check disabled_at
	bodyMap, ok := resp.Body.(map[string]interface{})
	require.True(t, ok, "response body should be a map")

	disabledAt := bodyMap["disabled_at"]
	require.NotNil(t, disabledAt, "destination should be disabled (disabled_at should not be null) - issue #596")

	// Cleanup mock server
	_ = mockServerInfra
}
