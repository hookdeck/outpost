package configs

import (
	"testing"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

func Basic(t *testing.T) (*config.Config, func(), error) {
	cleanupFns := []func(){}
	cleanup := func() {
		for _, fn := range cleanupFns {
			fn()
		}
	}

	// Testcontainer
	chEndpoint, cleanupCH, err := testutil.StartTestContainerClickHouse()
	if err != nil {
		return nil, cleanup, err
	}
	cleanupFns = append(cleanupFns, cleanupCH)

	awsEndpoint, cleanupAWS, err := testutil.StartTestcontainerLocalstack()
	if err != nil {
		return nil, cleanup, err
	}
	cleanupFns = append(cleanupFns, cleanupAWS)

	// Config
	redisConfig := testutil.CreateTestRedisConfig(t)
	clickHouseConfig := &clickhouse.ClickHouseConfig{
		Addr:     chEndpoint,
		Username: "default",
		Password: "",
		Database: "default",
	}

	return &config.Config{
		Hostname:               "outpost",
		Service:                config.ServiceTypeSingular,
		Port:                   testutil.RandomPortNumber(),
		APIKey:                 "apikey",
		JWTSecret:              "jwtsecret",
		EncryptionSecret:       "encryptionsecret",
		PortalProxyURL:         "",
		Topics:                 testutil.TestTopics,
		Redis:                  redisConfig,
		ClickHouse:             clickHouseConfig,
		OpenTelemetry:          nil,
		PublishQueueConfig:     nil,
		DeliveryQueueConfig:    &mqs.QueueConfig{AWSSQS: &mqs.AWSSQSConfig{Endpoint: awsEndpoint, Region: "us-east-1", ServiceAccountCredentials: "test:test:", Topic: "delivery"}},
		LogQueueConfig:         &mqs.QueueConfig{AWSSQS: &mqs.AWSSQSConfig{Endpoint: awsEndpoint, Region: "us-east-1", ServiceAccountCredentials: "test:test:", Topic: "log"}},
		PublishMaxConcurrency:  3,
		DeliveryMaxConcurrency: 3,
		LogMaxConcurrency:      3,
		RetryIntervalSeconds:   5,
		RetryMaxCount:          3,
	}, cleanup, nil
}
