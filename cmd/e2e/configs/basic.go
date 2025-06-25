package configs

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/config"
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
	LogStorage LogStorageType
}

func Basic(t *testing.T, opts BasicOpts) config.Config {
	// Get test infrastructure configs
	redisConfig := testutil.CreateTestRedisConfig(t)
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

	// MQ overrides
	c.MQs.RabbitMQ.ServerURL = rabbitmqServerURL
	c.MQs.RabbitMQ.Exchange = uuid.New().String()
	c.MQs.RabbitMQ.DeliveryQueue = uuid.New().String()
	c.MQs.RabbitMQ.LogQueue = uuid.New().String()

	// Test-specific overrides
	c.PublishMaxConcurrency = 3
	c.DeliveryMaxConcurrency = 3
	c.LogMaxConcurrency = 3
	c.RetryIntervalSeconds = 1
	c.RetryMaxLimit = 3
	c.LogBatchThresholdSeconds = 1
	c.LogBatchSize = 100

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
