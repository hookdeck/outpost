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

func Declare(ctx context.Context, cfg Config) error {
	if cfg.DeliveryMQ != nil {
		if err := mqinfra.DeclareMQ(ctx, *cfg.DeliveryMQ); err != nil {
			return err
		}
	}

	if cfg.LogMQ != nil {
		if err := mqinfra.DeclareMQ(ctx, *cfg.LogMQ); err != nil {
			return err
		}
	}

	return nil
}

func Teardown(ctx context.Context, cfg Config) error {
	if cfg.DeliveryMQ != nil {
		if err := mqinfra.TeardownMQ(ctx, *cfg.DeliveryMQ); err != nil {
			return err
		}
	}

	if cfg.LogMQ != nil {
		if err := mqinfra.TeardownMQ(ctx, *cfg.LogMQ); err != nil {
			return err
		}
	}

	return nil
}
