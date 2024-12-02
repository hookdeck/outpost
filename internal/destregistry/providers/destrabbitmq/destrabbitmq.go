package destrabbitmq

import (
	"context"
	"encoding/json"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQDestination struct {
	*destregistry.BaseProvider
}

type RabbitMQDestinationConfig struct {
	ServerURL string // TODO: consider renaming
	Exchange  string
}

type RabbitMQDestinationCredentials struct {
	Username string
	Password string
}

var _ destregistry.Provider = (*RabbitMQDestination)(nil)

func New() (*RabbitMQDestination, error) {
	base, err := destregistry.NewBaseProvider("rabbitmq")
	if err != nil {
		return nil, err
	}
	return &RabbitMQDestination{BaseProvider: base}, nil
}

func (d *RabbitMQDestination) Validate(ctx context.Context, destination *models.Destination) error {
	if _, _, err := d.resolveMetadata(ctx, destination); err != nil {
		return err
	}
	return nil
}

func (d *RabbitMQDestination) Publish(ctx context.Context, destination *models.Destination, event *models.Event) error {
	config, credentials, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	url := rabbitURL(config, credentials)
	exchange := config.Exchange
	if err := publishEvent(ctx, url, exchange, event); err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	return nil
}

func (d *RabbitMQDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*RabbitMQDestinationConfig, *RabbitMQDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	return &RabbitMQDestinationConfig{
			ServerURL: destination.Config["server_url"],
			Exchange:  destination.Config["exchange"],
		}, &RabbitMQDestinationCredentials{
			Username: destination.Credentials["username"],
			Password: destination.Credentials["password"],
		}, nil
}

func rabbitURL(config *RabbitMQDestinationConfig, credentials *RabbitMQDestinationCredentials) string {
	return "amqp://" + credentials.Username + ":" + credentials.Password + "@" + config.ServerURL
}

func publishEvent(ctx context.Context, url string, exchange string, event *models.Event) error {
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
