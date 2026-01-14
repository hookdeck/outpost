package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

type APITest struct {
	Name     string
	Delay    time.Duration
	Request  httpclient.Request
	Expected APITestExpectation
}

type APITestExpectation struct {
	Match    *httpclient.Response
	Validate map[string]interface{}
}

func (test *APITest) Run(t *testing.T, client httpclient.Client) {
	t.Helper()

	if test.Delay > 0 {
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
	// TODO: replace with a health check
	time.Sleep(2 * time.Second)
}

func (s *basicSuite) TearDownSuite() {
	s.e2eSuite.TearDownSuite()
}

// func TestCHBasicSuite(t *testing.T) {
// 	t.Parallel()
// 	if testing.Short() {
// 		t.Skip("skipping e2e test")
// 	}
// 	suite.Run(t, &basicSuite{logStorageType: configs.LogStorageTypeClickHouse})
// }

// TestPGBasicSuite is skipped by default - redundant with TestDragonflyBasicSuite
func TestPGBasicSuite(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testutil.SkipUnlessCompat(t)
	suite.Run(t, &basicSuite{logStorageType: configs.LogStorageTypePostgres})
}

// TestRedisClusterBasicSuite is skipped by default - run with TESTCOMPAT=1 for full compatibility testing
func TestRedisClusterBasicSuite(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testutil.SkipUnlessCompat(t)

	// Get Redis cluster config from environment
	redisConfig := configs.CreateRedisClusterConfig(t)
	if redisConfig == nil {
		t.Skip("skipping Redis cluster test (TEST_REDIS_CLUSTER_URL not set)")
	}

	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypePostgres,
		redisConfig:    redisConfig,
	})
}

func TestDragonflyBasicSuite(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	// Use NewDragonflyStackConfig (DB 0) for RediSearch support
	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypePostgres,
		redisConfig:    testinfra.NewDragonflyStackConfig(t),
	})
}

// TestRedisStackBasicSuite is skipped by default - run with TESTCOMPAT=1 for full compatibility testing
func TestRedisStackBasicSuite(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testutil.SkipUnlessCompat(t)

	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypePostgres,
		redisConfig:    testinfra.NewRedisStackConfig(t),
	})
}

func TestBasicSuiteWithDeploymentID(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite.Run(t, &basicSuite{
		logStorageType: configs.LogStorageTypePostgres,
		deploymentID:   "dp_e2e_test",
	})
}

// TestAutoDisableWithoutCallbackURL tests the scenario from issue #596:
// ALERT_AUTO_DISABLE_DESTINATION=true without ALERT_CALLBACK_URL set.
// Run with: go test -v -run TestAutoDisableWithoutCallbackURL ./cmd/e2e/...
func TestAutoDisableWithoutCallbackURL(t *testing.T) {
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
	time.Sleep(2 * time.Second)

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
		Path:    "/" + tenantID,
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
		Path:    "/" + tenantID + "/destinations",
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
		Path:    "/" + tenantID + "/destinations/" + destinationID,
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
