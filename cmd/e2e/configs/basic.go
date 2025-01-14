package configs

import (
	"context"
	"log"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

func Basic(t *testing.T) config.Config {
	// Get test infrastructure configs
	redisConfig := testutil.CreateTestRedisConfig(t)
	clickHouseConfig := testinfra.NewClickHouseConfig(t)
	rabbitmqServerURL := testinfra.EnsureRabbitMQ()

	// Start with defaults
	c := &config.Config{}
	c.InitDefaults()

	// Override only what's needed for e2e tests
	c.Service = config.ServiceTypeSingular.String()
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

	c.ClickHouse.Addr = clickHouseConfig.Addr
	c.ClickHouse.Username = clickHouseConfig.Username
	c.ClickHouse.Password = clickHouseConfig.Password
	c.ClickHouse.Database = clickHouseConfig.Database

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
		if err := infra.Teardown(context.Background(), infra.Config{
			DeliveryMQ: c.MQs.GetDeliveryQueueConfig(),
			LogMQ:      c.MQs.GetLogQueueConfig(),
		}); err != nil {
			log.Println("Teardown failed:", err)
		}
	})

	return *c
}
