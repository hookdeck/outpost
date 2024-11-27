package configs

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

func Basic(t *testing.T) *config.Config {
	// Config
	redisConfig := testutil.CreateTestRedisConfig(t)
	clickHouseConfig := testinfra.NewClickHouseConfig(t)
	deliveryMQConfig := testinfra.NewMQAWSConfig(t, nil)
	logMQConfig := testinfra.NewMQAWSConfig(t, nil)

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
		LogBatcherDelayThresholdSeconds: 1,
		LogBatcherItemCountThreshold:    100,
	}
}
