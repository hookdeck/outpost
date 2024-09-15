package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/rabbitmq/amqp091-go"
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
	_, err := parseConfig(destination)
	return err
}

func (d *RabbitMQDestination) Publish(ctx context.Context, destination adapters.DestinationAdapterValue, event *ingest.Event) error {
	config, err := parseConfig(destination)
	if err != nil {
		return err
	}
	return publishEvent(ctx, config, event)
}

func parseConfig(destination adapters.DestinationAdapterValue) (*RabbitMQDestinationConfig, error) {
	if destination.Type != "rabbitmq" {
		return nil, errors.New("invalid destination type")
	}

	destinationConfig := &RabbitMQDestinationConfig{
		ServerURL: destination.Config["server_url"],
		Exchange:  destination.Config["exchange"],
	}

	if destinationConfig.ServerURL == "" {
		return nil, errors.New("server_url is required for rabbitmq destination config")
	}
	if destinationConfig.Exchange == "" {
		return nil, errors.New("exchange is required for rabbitmq destination config")
	}

	return destinationConfig, nil
}

func publishEvent(ctx context.Context, config *RabbitMQDestinationConfig, event *ingest.Event) error {
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	conn, err := amqp091.Dial(config.ServerURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	ch.PublishWithContext(ctx,
		config.Exchange, // exchange
		"",              // routing key
		false,           // mandatory
		false,           // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        []byte(dataBytes),
		},
	)

	return nil
}
