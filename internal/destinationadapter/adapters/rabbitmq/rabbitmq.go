package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQDestination struct {
}

type RabbitMQDestinationConfig struct {
	ServerURL string // TODO: consider renaming
	Exchange  string
}

type RabbitMQDestinationCredentials struct {
	Username string
	Password string
}

var _ adapters.DestinationAdapter = (*RabbitMQDestination)(nil)

func New() *RabbitMQDestination {
	return &RabbitMQDestination{}
}

func (d *RabbitMQDestination) Validate(ctx context.Context, destination adapters.DestinationAdapterValue) error {
	_, err := parseConfig(destination)
	if err != nil {
		return err
	}
	_, err = parseCredentials(destination)
	return err
}

func (d *RabbitMQDestination) Publish(ctx context.Context, destination adapters.DestinationAdapterValue, event *adapters.Event) error {
	config, err := parseConfig(destination)
	if err != nil {
		return err
	}
	credentials, err := parseCredentials(destination)
	if err != nil {
		return err
	}
	url := rabbitURL(config, credentials)
	exchange := config.Exchange
	return publishEvent(ctx, url, exchange, event)
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

func parseCredentials(destination adapters.DestinationAdapterValue) (*RabbitMQDestinationCredentials, error) {
	if destination.Type != "rabbitmq" {
		return nil, errors.New("invalid destination type")
	}

	destinationCredentials := &RabbitMQDestinationCredentials{
		Username: destination.Credentials["username"],
		Password: destination.Credentials["password"],
	}

	if destinationCredentials.Username == "" {
		return nil, errors.New("username is required for rabbitmq destination credentials")
	}
	if destinationCredentials.Password == "" {
		return nil, errors.New("password is required for rabbitmq destination credentials")
	}

	return destinationCredentials, nil
}

func rabbitURL(config *RabbitMQDestinationConfig, credentials *RabbitMQDestinationCredentials) string {
	return "amqp://" + credentials.Username + ":" + credentials.Password + "@" + config.ServerURL
}

func publishEvent(ctx context.Context, url string, exchange string, event *adapters.Event) error {
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	conn, err := amqp091.Dial(url)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	headers := make(amqp091.Table)
	for k, v := range event.Metadata {
		headers[k] = v
	}

	return ch.PublishWithContext(ctx,
		exchange, // exchange
		"",       // routing key
		false,    // mandatory
		false,    // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Headers:     headers,
			Body:        []byte(dataBytes),
		},
	)
}
