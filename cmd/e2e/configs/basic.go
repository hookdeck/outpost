package configs

import (
	"context"
	"log"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

func Basic(t *testing.T) *config.Config {
	// Config
	redisConfig := testutil.CreateTestRedisConfig(t)
	clickHouseConfig := testinfra.NewClickHouseConfig(t)
	rabbitmqServerURL := testinfra.EnsureRabbitMQ()
	deliveryMQConfig := mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: rabbitmqServerURL,
			Exchange:  uuid.New().String(),
			Queue:     uuid.New().String(),
		},
		Policy: mqs.Policy{
			RetryLimit: 5,
		},
	}
	logMQConfig := mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: rabbitmqServerURL,
			Exchange:  uuid.New().String(),
			Queue:     uuid.New().String(),
		},
		Policy: mqs.Policy{
			RetryLimit: 5,
		},
	}
	t.Cleanup(func() {
		if err := infra.Teardown(context.Background(), infra.Config{
			DeliveryMQ: &deliveryMQConfig,
			LogMQ:      &logMQConfig,
		}); err != nil {
			log.Println("Teardown failed:", err)
		}
	})

	return &config.Config{
		Hostname:                        "outpost",
		Service:                         config.ServiceTypeSingular,
		Port:                            testutil.RandomPortNumber(),
		APIKey:                          "apikey",
		JWTSecret:                       "jwtsecret",
		EncryptionSecret:                "encryptionsecret",
		PortalProxyURL:                  "",
		Topics:                          testutil.TestTopics,
		Redis:                           redisConfig,
		ClickHouse:                      &clickHouseConfig,
		OpenTelemetry:                   nil,
		PublishQueueConfig:              nil,
		DeliveryQueueConfig:             &deliveryMQConfig,
		LogQueueConfig:                  &logMQConfig,
		PublishMaxConcurrency:           3,
		DeliveryMaxConcurrency:          3,
		LogMaxConcurrency:               3,
		RetryIntervalSeconds:            1,
		RetryMaxCount:                   3,
		DeliveryTimeoutSeconds:          5,
		LogBatcherDelayThresholdSeconds: 1,
		LogBatcherItemCountThreshold:    100,
		MaxDestinationsPerTenant:        20,
		DestinationWebhookHeaderPrefix:  "x-outpost-",
	}
}
