package rabbitmq

import (
	"context"
	"errors"

	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/ingest"
)

type RabbitMQDestination struct {
}

type RabbitMQDestinationConfig struct {
	ServerURL string
	Exchange  string
}

var _ adapters.DestinationAdapter = (*RabbitMQDestination)(nil)

func New() *RabbitMQDestination {
	return &RabbitMQDestination{}
}

func (d *RabbitMQDestination) Validate(ctx context.Context, destination adapters.DestinationAdapterValue) error {
	destinationConfig := RabbitMQDestinationConfig{
		ServerURL: destination.Config["server_url"],
		Exchange:  destination.Config["exchange"],
	}

	if destinationConfig.ServerURL == "" {
		return errors.New("server url is required for rabbitmq destination config")
	}
	if destinationConfig.Exchange == "" {
		return errors.New("exchange is required for rabbitmq destination config")
	}

	return nil
}

func (d *RabbitMQDestination) Publish(ctx context.Context, destination adapters.DestinationAdapterValue, event *ingest.Event) error {
	return nil
}
