package mqinfra

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
)

func DeclareMQ(ctx context.Context, cfg mqs.QueueConfig, policy mqs.Policy) error {
	if cfg.AWSSQS != nil {
		return NewErrUnimplemented("AWSSQS")
	}
	if cfg.AzureServiceBus != nil {
		return NewErrUnimplemented("AzureServiceBus")
	}
	if cfg.GCPPubSub != nil {
		return NewErrUnimplemented("GCPPubSub")
	}
	if cfg.RabbitMQ != nil {
		return DeclareRabbitMQ(ctx, &cfg, &policy)
	}
	return ErrInvalidConfig
}

func TeardownMQ(ctx context.Context, cfg mqs.QueueConfig) error {
	if cfg.AWSSQS != nil {
		return NewErrUnimplemented("AWSSQS")
	}
	if cfg.AzureServiceBus != nil {
		return NewErrUnimplemented("AzureServiceBus")
	}
	if cfg.GCPPubSub != nil {
		return NewErrUnimplemented("GCPPubSub")
	}
	if cfg.RabbitMQ != nil {
		return TeardownRabbitMQ(ctx, &cfg)
	}
	return ErrInvalidConfig
}

var (
	ErrInvalidConfig = fmt.Errorf("invalid config")
)

func NewErrUnimplemented(name string) error {
	return fmt.Errorf("mqinfra.DeclareMQ unimplemented: %s", name)
}
