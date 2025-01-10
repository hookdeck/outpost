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

func Basic(t *testing.T) *config.Config {
	// Config
	redisConfig := testutil.CreateTestRedisConfig(t)
	clickHouseConfig := testinfra.NewClickHouseConfig(t)
	rabbitmqServerURL := testinfra.EnsureRabbitMQ()
	mqsConfig := config.MQsConfig{
		RabbitMQ: &config.RabbitMQConfig{
			ServerURL:     rabbitmqServerURL,
			Exchange:      uuid.New().String(),
			DeliveryQueue: uuid.New().String(),
			LogQueue:      uuid.New().String(),
		},
		AWSSQS:             &config.AWSSQSConfig{},
		DeliveryRetryLimit: 5,
		LogRetryLimit:      5,
	}
	t.Cleanup(func() {
		if err := infra.Teardown(context.Background(), infra.Config{
			DeliveryMQ: mqsConfig.GetDeliveryQueueConfig(),
			LogMQ:      mqsConfig.GetLogQueueConfig(),
		}); err != nil {
			log.Println("Teardown failed:", err)
		}
	})

	return &config.Config{
		Service:             config.ServiceTypeSingular,
		Port:                testutil.RandomPortNumber(),
		APIKey:              "apikey",
		APIJWTSecret:        "jwtsecret",
		AESEncryptionSecret: "encryptionsecret",
		PortalProxyURL:      "",
		Topics:              testutil.TestTopics,
		Redis: &config.RedisConfig{
			Host:     redisConfig.Host,
			Port:     redisConfig.Port,
			Password: redisConfig.Password,
			Database: redisConfig.Database,
		},
		ClickHouse: &config.ClickHouseConfig{
			Addr:     clickHouseConfig.Addr,
			Username: clickHouseConfig.Username,
			Password: clickHouseConfig.Password,
			Database: clickHouseConfig.Database,
		},
		OpenTelemetry:                   nil,
		PublishMQ:                       nil,
		MQs:                             &mqsConfig,
		PublishMaxConcurrency:           3,
		DeliveryMaxConcurrency:          3,
		LogMaxConcurrency:               3,
		RetryIntervalSeconds:            1,
		RetryMaxLimit:                   3,
		DeliveryTimeoutSeconds:          5,
		LogBatcherDelayThresholdSeconds: 1,
		LogBatcherItemCountThreshold:    100,
		MaxDestinationsPerTenant:        20,
		DestinationWebhookHeaderPrefix:  "x-outpost-",
	}
}
