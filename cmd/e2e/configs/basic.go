package configs

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

type LogStorageType string

const (
	LogStorageTypePostgres   LogStorageType = "postgres"
	LogStorageTypeClickHouse LogStorageType = "clickhouse"
)

type BasicOpts struct {
	LogStorage   LogStorageType
	RedisConfig  *redis.RedisConfig // Optional Redis config override
	DeploymentID string             // Optional deployment ID for multi-tenancy testing
}

func Basic(t *testing.T, opts BasicOpts) config.Config {
	// Get test infrastructure configs
	var redisConfig *redis.RedisConfig
	if opts.RedisConfig != nil {
		// Use provided Redis config (e.g., for cluster testing)
		redisConfig = opts.RedisConfig
	} else {
		// Use default test Redis config
		redisConfig = testutil.CreateTestRedisConfig(t)
	}
	rabbitmqServerURL := testinfra.EnsureRabbitMQ()

	logLevel := "fatal"
	if os.Getenv("LOG_LEVEL") != "" {
		logLevel = os.Getenv("LOG_LEVEL")
	}

	// Start with defaults
	c := &config.Config{}
	c.InitDefaults()

	require.NoError(t, setLogStorage(t, c, opts.LogStorage))

	// Override only what's needed for e2e tests
	c.LogLevel = logLevel
	c.Service = config.ServiceTypeAll.String()
	c.APIPort = testutil.RandomPortNumber()
	c.APIKey = "apikey"
	c.APIJWTSecret = "jwtsecret"
	c.AESEncryptionSecret = "encryptionsecret"
	c.Topics = testutil.TestTopics

	// Infrastructure overrides
	c.Redis.Host = redisConfig.Host
	c.Redis.Port = redisConfig.Port
	c.Redis.Password = redisConfig.Password
	c.Redis.Database = redisConfig.Database
	c.Redis.ClusterEnabled = redisConfig.ClusterEnabled
	c.Redis.DevClusterHostOverride = redisConfig.DevClusterHostOverride

	// MQ overrides
	c.MQs.RabbitMQ.ServerURL = rabbitmqServerURL
	c.MQs.RabbitMQ.Exchange = idgen.String()
	c.MQs.RabbitMQ.DeliveryQueue = idgen.String()
	c.MQs.RabbitMQ.LogQueue = idgen.String()

	// Test-specific overrides
	c.PublishMaxConcurrency = 3
	c.DeliveryMaxConcurrency = 3
	c.LogMaxConcurrency = 3
	c.RetryIntervalSeconds = 1
	c.RetryMaxLimit = 3
	c.LogBatchThresholdSeconds = 1
	c.LogBatchSize = 100
	c.DeploymentID = opts.DeploymentID

	// Setup cleanup
	t.Cleanup(func() {
		redisClient, err := redis.New(context.Background(), c.Redis.ToConfig())
		if err != nil {
			log.Println("Failed to create redis client:", err)
		}
		outpostInfra := infra.NewInfra(infra.Config{
			DeliveryMQ: c.MQs.ToInfraConfig("deliverymq"),
			LogMQ:      c.MQs.ToInfraConfig("logmq"),
		}, redisClient)
		if err := outpostInfra.Teardown(context.Background()); err != nil {
			log.Println("Teardown failed:", err)
		}
	})

	return *c
}

// CreateRedisClusterConfig creates a Redis cluster configuration for testing
// Returns nil if TEST_REDIS_CLUSTER_URL is not set
func CreateRedisClusterConfig(t *testing.T) *redis.RedisConfig {
	// Get Redis cluster URL from environment
	redisClusterURL := os.Getenv("TEST_REDIS_CLUSTER_URL")
	if redisClusterURL == "" {
		return nil
	}

	// Parse host and port from URL (format: "redis-cluster:7000")
	parts := strings.Split(redisClusterURL, ":")
	if len(parts) != 2 {
		t.Fatalf("Invalid TEST_REDIS_CLUSTER_URL format: %s (expected host:port)", redisClusterURL)
	}

	redisHost := parts[0]
	redisPort := 7000 // Default port, could parse from parts[1] if needed

	redisConfig := &redis.RedisConfig{
		Host:                   redisHost,
		Port:                   redisPort,
		Password:               "",
		Database:               0,
		ClusterEnabled:         true,
		DevClusterHostOverride: true, // Always true for Docker-based cluster tests
	}

	// Test Redis connection before returning
	t.Logf("Testing Redis cluster connection to %s:%d", redisHost, redisPort)
	testCtx := context.Background()
	_, err := redis.New(testCtx, redisConfig)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	t.Logf("Redis client created successfully")

	return redisConfig
}

func setLogStorage(t *testing.T, c *config.Config, logStorage LogStorageType) error {
	switch logStorage {
	case LogStorageTypePostgres:
		postgresURL := testinfra.NewPostgresConfig(t)
		c.PostgresURL = postgresURL
	case LogStorageTypeClickHouse:
		clickHouseConfig := testinfra.NewClickHouseConfig(t)
		c.ClickHouse.Addr = clickHouseConfig.Addr
		c.ClickHouse.Username = clickHouseConfig.Username
		c.ClickHouse.Password = clickHouseConfig.Password
		c.ClickHouse.Database = clickHouseConfig.Database
	default:
		return fmt.Errorf("invalid log storage type: %s", logStorage)
	}
	return nil
}
