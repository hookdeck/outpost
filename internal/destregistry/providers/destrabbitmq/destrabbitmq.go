package destrabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQDestination struct {
	*destregistry.BaseProvider
}

type RabbitMQDestinationConfig struct {
	ServerURL  string // TODO: consider renaming
	Exchange   string
	RoutingKey string
}

type RabbitMQDestinationCredentials struct {
	Username string
	Password string
}

var _ destregistry.Provider = (*RabbitMQDestination)(nil)

func New(loader metadata.MetadataLoader) (*RabbitMQDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "rabbitmq")
	if err != nil {
		return nil, err
	}
	return &RabbitMQDestination{BaseProvider: base}, nil
}

func (d *RabbitMQDestination) Validate(ctx context.Context, destination *models.Destination) error {
	config, _, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return err
	}

	// At least one of exchange or routing_key must be non-empty
	if config.Exchange == "" && config.RoutingKey == "" {
		return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "config.exchange",
				Type:  "either_required",
			},
			{
				Field: "config.routing_key",
				Type:  "either_required",
			},
		})
	}

	return nil
}

func (d *RabbitMQDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	config, credentials, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return nil, err
	}
	return &RabbitMQPublisher{
		BasePublisher: &destregistry.BasePublisher{},
		url:           rabbitURL(config, credentials),
		exchange:      config.Exchange,
		routingKey:    config.RoutingKey,
	}, nil
}

func (d *RabbitMQDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*RabbitMQDestinationConfig, *RabbitMQDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	return &RabbitMQDestinationConfig{
			ServerURL:  destination.Config["server_url"],
			Exchange:   destination.Config["exchange"],
			RoutingKey: destination.Config["routing_key"],
		}, &RabbitMQDestinationCredentials{
			Username: destination.Credentials["username"],
			Password: destination.Credentials["password"],
		}, nil
}

type RabbitMQPublisher struct {
	*destregistry.BasePublisher
	url        string
	exchange   string
	routingKey string
	conn       *amqp091.Connection
	channel    *amqp091.Channel
	mu         sync.Mutex
}

func (p *RabbitMQPublisher) Close() error {
	p.BasePublisher.StartClose()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.channel != nil {
		p.channel.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
	return nil
}

func (p *RabbitMQPublisher) Publish(ctx context.Context, event *models.Event) error {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return err
	}
	defer p.BasePublisher.FinishPublish()

	// Ensure we have a valid connection
	if err := p.ensureConnection(ctx); err != nil {
		return err
	}

	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	headers := make(amqp091.Table)
	for k, v := range event.Metadata {
		headers[k] = v
	}

	if err := p.channel.PublishWithContext(ctx,
		p.exchange,   // exchange
		p.routingKey, // routing key
		false,        // mandatory
		false,        // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Headers:     headers,
			Body:        []byte(dataBytes),
		},
	); err != nil {
		return destregistry.NewErrDestinationPublishAttempt(err, "rabbitmq", err)
	}

	return nil
}

func (p *RabbitMQPublisher) ensureConnection(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil && !p.conn.IsClosed() && p.channel != nil && !p.channel.IsClosed() {
		return nil
	}

	// Create new connection
	conn, err := amqp091.Dial(p.url)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create channel
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create channel: %w", err)
	}

	// Update connection and channel
	if p.conn != nil {
		p.conn.Close()
	}
	if p.channel != nil {
		p.channel.Close()
	}
	p.conn = conn
	p.channel = channel

	return nil
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

// ===== TEST HELPERS =====

func (p *RabbitMQPublisher) GetConnection() *amqp091.Connection {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn
}

func (p *RabbitMQPublisher) ForceConnectionClose() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		p.conn.Close()
	}
}

func (d *RabbitMQDestination) ComputeTarget(destination *models.Destination) string {
	exchange := destination.Config["exchange"]
	routingKey := destination.Config["routing_key"]
	if exchange == "" {
		return routingKey
	}
	if routingKey == "" {
		return exchange
	}
	return exchange + " -> " + routingKey
}
