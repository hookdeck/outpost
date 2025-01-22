package infra

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
)

type Config struct {
	DeliveryMQ *mqs.QueueConfig
	LogMQ      *mqs.QueueConfig
}

func (cfg *Config) SetSensiblePolicyDefaults() {
	cfg.DeliveryMQ.Policy.RetryLimit = 5
	cfg.LogMQ.Policy.RetryLimit = 5
}

func Declare(ctx context.Context, cfg Config) error {
	cfg.SetSensiblePolicyDefaults()

	if cfg.DeliveryMQ != nil {
		if err := mqinfra.New(cfg.DeliveryMQ).Declare(ctx); err != nil {
			return err
		}
	}

	if cfg.LogMQ != nil {
		if err := mqinfra.New(cfg.LogMQ).Declare(ctx); err != nil {
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
