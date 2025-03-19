package destrabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	ServerURL string // TODO: consider renaming
	Exchange  string
	UseTLS    bool
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
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return err
	}

	// Validate TLS config if provided
	if tlsStr, ok := destination.Config["tls"]; ok {
		if tlsStr != "on" && tlsStr != "true" && tlsStr != "false" {
			return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "config.tls",
					Type:  "invalid",
				},
			})
		}
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
	}, nil
}

func (d *RabbitMQDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*RabbitMQDestinationConfig, *RabbitMQDestinationCredentials, error) {
	if err := d.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	useTLS := false // default to false if omitted
	if tlsStr, ok := destination.Config["tls"]; ok {
		useTLS = tlsStr == "true" || tlsStr == "on"
	}

	return &RabbitMQDestinationConfig{
			ServerURL: destination.Config["server_url"],
			Exchange:  destination.Config["exchange"],
			UseTLS:    useTLS,
		}, &RabbitMQDestinationCredentials{
			Username: destination.Credentials["username"],
			Password: destination.Credentials["password"],
		}, nil
}

// Preprocess sets the default TLS value to "true" if not provided
func (d *RabbitMQDestination) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	if newDestination.Config == nil {
		return nil
	}
	if newDestination.Config["tls"] == "on" {
		newDestination.Config["tls"] = "true"
	} else if newDestination.Config["tls"] == "" {
		newDestination.Config["tls"] = "false" // default to false if omitted
	}
	if _, _, err := d.resolveMetadata(context.Background(), newDestination); err != nil {
		return err
	}
	return nil
}

// AMQPChannel is an interface that defines the methods we need from amqp091.Channel
// This is exported so that tests can implement this interface
type AMQPChannel interface {
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp091.Publishing) error
	Close() error
	IsClosed() bool
}

// AMQPConnection is an interface that defines the methods we need from amqp091.Connection for testing
type AMQPConnection interface {
	Close() error
	IsClosed() bool
}

type RabbitMQPublisher struct {
	*destregistry.BasePublisher
	url      string
	exchange string
	conn     AMQPConnection
	channel  AMQPChannel
	mu       sync.Mutex
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

func (p *RabbitMQPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	// Ensure we have a valid connection
	if err := p.ensureConnection(ctx); err != nil {
		return nil, destregistry.NewErrDestinationPublishAttempt(err, "rabbitmq", map[string]interface{}{
			"error":   "connection_failed",
			"message": err.Error(),
		})
	}

	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return nil, err
	}

	headers := make(amqp091.Table)
	for k, v := range event.Metadata {
		headers[k] = v
	}

	if err := p.channel.PublishWithContext(ctx,
		p.exchange,  // exchange
		event.Topic, // routing key
		false,       // mandatory
		false,       // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Headers:     headers,
			Body:        []byte(dataBytes),
		},
	); err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error": err.Error(),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "rabbitmq", map[string]interface{}{
				"error":   "publish_failed",
				"message": err.Error(),
			})
	}

	return &destregistry.Delivery{
		Status:   "success",
		Code:     "OK",
		Response: map[string]interface{}{},
	}, nil
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
	scheme := "amqp"
	if config.UseTLS {
		scheme = "amqps"
	}
	return fmt.Sprintf("%s://%s:%s@%s", scheme, credentials.Username, credentials.Password, config.ServerURL)
}

// ===== TEST HELPERS =====

func (p *RabbitMQPublisher) GetConnection() AMQPConnection {
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

// SetupForTesting sets both the connection and channel for testing purposes
func (p *RabbitMQPublisher) SetupForTesting(conn AMQPConnection, channel AMQPChannel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conn = conn
	p.channel = channel
}

func (d *RabbitMQDestination) ComputeTarget(destination *models.Destination) string {
	exchange := destination.Config["exchange"]
	return exchange + " -> " + strings.Join(destination.Topics, ", ")
}
