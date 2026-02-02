package e2e_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/alert"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
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

type e2eSuite struct {
	ctx               context.Context
	cancel            context.CancelFunc
	config            config.Config
	mockServerBaseURL string
	mockServerInfra   *testinfra.MockServerInfra
	cleanup           func()
	appDone           chan struct{}
}

func (suite *e2eSuite) SetupSuite() {
	ctx, cancel := context.WithCancel(context.Background())
	suite.ctx = ctx
	suite.cancel = cancel
	suite.appDone = make(chan struct{})
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
		// Wait for application to shut down, but don't block forever.
		select {
		case <-s.appDone:
		case <-time.After(30 * time.Second):
			log.Println("WARNING: application did not shut down within 30s, proceeding with cleanup")
		}
	}
	s.cleanup()
}

type basicSuite struct {
	suite.Suite
	e2eSuite
	logStorageType configs.LogStorageType
	redisConfig    *redis.RedisConfig // Optional Redis config override
	deploymentID   string             // Optional deployment ID
	hasRediSearch  bool               // Whether the Redis backend supports RediSearch (only RedisStack)
	alertServer    *alert.AlertMockServer
	httpClient     *http.Client // Used by doJSON helpers
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

	suite.httpClient = &http.Client{Timeout: 10 * time.Second}

	// wait for outpost services to start
	waitForHealthy(t, cfg.APIPort, 5*time.Second)
}

func (s *basicSuite) SetupTest() {
	s.alertServer.Reset()
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
