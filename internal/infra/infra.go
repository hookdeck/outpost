package infra

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqinfra"
)

type Config struct {
	DeliveryMQ *mqinfra.MQInfraConfig
	LogMQ      *mqinfra.MQInfraConfig
}

func (cfg *Config) SetSensiblePolicyDefaults() {
	cfg.DeliveryMQ.Policy.RetryLimit = 5
	cfg.LogMQ.Policy.RetryLimit = 5
}

func Declare(ctx context.Context, cfg Config) error {
	cfg.SetSensiblePolicyDefaults()

	// Check existence first
	var deliveryMQExists, logMQExists bool
	var deliveryMQ, logMQ mqinfra.MQInfra

	if cfg.DeliveryMQ != nil {
		deliveryMQ = mqinfra.New(cfg.DeliveryMQ)
		exists, err := deliveryMQ.Exist(ctx)
		if err != nil {
			return err
		}
		deliveryMQExists = exists
	}

	if cfg.LogMQ != nil {
		logMQ = mqinfra.New(cfg.LogMQ)
		exists, err := logMQ.Exist(ctx)
		if err != nil {
			return err
		}
		logMQExists = exists
	}

	// Declare if necessary
	if cfg.DeliveryMQ != nil && !deliveryMQExists {
		if err := deliveryMQ.Declare(ctx); err != nil {
			return err
		}
	}

	if cfg.LogMQ != nil && !logMQExists {
		if err := logMQ.Declare(ctx); err != nil {
			return err
		}
	}

	return nil
}

func Teardown(ctx context.Context, cfg Config) error {
	if cfg.DeliveryMQ != nil {
		if err := mqinfra.New(cfg.DeliveryMQ).TearDown(ctx); err != nil {
			return err
		}
	}

	if cfg.LogMQ != nil {
		if err := mqinfra.New(cfg.LogMQ).TearDown(ctx); err != nil {
			return err
		}
	}

	return nil
}
