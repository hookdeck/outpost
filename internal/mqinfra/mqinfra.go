package mqinfra

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
)

type MQInfra interface {
	Declare(ctx context.Context) error
	TearDown(ctx context.Context) error
}

func New(cfg *mqs.QueueConfig) MQInfra {
	if cfg.AWSSQS != nil {
		return &infraAWSSQS{cfg: cfg}
	}
	if cfg.AzureServiceBus != nil {
		return &infraUnimplemented{}
	}
	if cfg.GCPPubSub != nil {
		return &infraUnimplemented{}
	}
	if cfg.RabbitMQ != nil {
		return &infraRabbitMQ{cfg: cfg}
	}

	return &infraInvalid{}
}

type infraInvalid struct {
}

func (infra *infraInvalid) Declare(ctx context.Context) error {
	return ErrInvalidConfig
}

func (infra *infraInvalid) TearDown(ctx context.Context) error {
	return ErrInvalidConfig
}

type infraUnimplemented struct {
}

func (infra *infraUnimplemented) Declare(ctx context.Context) error {
	return ErrInvalidConfig
}

func (infra *infraUnimplemented) TearDown(ctx context.Context) error {
	return ErrInvalidConfig
}

var (
	ErrInvalidConfig      = fmt.Errorf("invalid config")
	ErrInfraUnimplemented = fmt.Errorf("unimplemented infra")
)
