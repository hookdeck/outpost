package destrabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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

func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption) (*RabbitMQDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "rabbitmq", basePublisherOpts...)
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
		BasePublisher: d.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
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

type RabbitMQPublisher struct {
	*destregistry.BasePublisher
	url      string
	exchange string
	conn     *amqp091.Connection
	channel  *amqp091.Channel
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
		// Context canceled is a system error (e.g., service shutdown) - return nil
		// so it's treated as PreDeliveryError (nack → requeue for another instance).
		// See: https://github.com/hookdeck/outpost/issues/571
		if errors.Is(err, context.Canceled) {
			return nil, destregistry.NewErrDestinationPublishAttempt(err, "rabbitmq", map[string]interface{}{
				"error":   "canceled",
				"message": err.Error(),
			})
		}

		// All other connection errors are destination-level failures (DeliveryError → ack + retry)
		return &destregistry.Delivery{
			Status: "failed",
			Code:   ClassifyRabbitMQError(err),
			Response: map[string]interface{}{
				"error": err.Error(),
			},
		}, destregistry.NewErrDestinationPublishAttempt(err, "rabbitmq", map[string]interface{}{
			"error":   "connection_failed",
			"message": err.Error(),
		})
	}

	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return nil, err
	}

	headers := make(amqp091.Table)
	metadata := p.BasePublisher.MakeMetadata(event, time.Now())
	for k, v := range metadata {
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
		// Context canceled is a system error (e.g., service shutdown) - return nil
		// so it's treated as PreDeliveryError (nack → requeue for another instance).
		if errors.Is(err, context.Canceled) {
			return nil, destregistry.NewErrDestinationPublishAttempt(err, "rabbitmq", map[string]interface{}{
				"error":   "canceled",
				"message": err.Error(),
			})
		}

		return &destregistry.Delivery{
				Status: "failed",
				Code:   ClassifyRabbitMQError(err),
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

// ClassifyRabbitMQError returns a descriptive error code based on the error type.
// All errors classified here are destination-level failures (DeliveryError → ack + retry).
//
// Error codes and their meanings:
//   - dns_error:           Domain doesn't exist or DNS lookup failed
//   - connection_refused:  Server not running or rejecting connections
//   - connection_reset:    Connection was dropped by the server
//   - auth_failed:         Authentication/authorization failure
//   - channel_error:       Channel-level error (closed, etc.)
//   - exchange_not_found:  Exchange doesn't exist
//   - timeout:             Connection or operation timed out
//   - tls_error:           TLS/SSL certificate or handshake failure
//   - rabbitmq_error:      Other RabbitMQ-related failures (catch-all)
//
// Note: context.Canceled is handled separately as a system error (nack → requeue).
func ClassifyRabbitMQError(err error) string {
	if err == nil {
		return "unknown"
	}

	errStr := err.Error()

	// Check for AMQP-specific errors first
	var amqpErr *amqp091.Error
	if errors.As(err, &amqpErr) {
		switch amqpErr.Code {
		case amqp091.AccessRefused:
			return "access_denied"
		case amqp091.NotFound:
			return "exchange_not_found"
		case amqp091.ChannelError:
			return "channel_error"
		case amqp091.ConnectionForced:
			return "connection_forced"
		default:
			return "rabbitmq_error"
		}
	}

	// Fall back to string matching for network-level errors
	switch {
	case strings.Contains(errStr, "no such host"):
		return "dns_error"
	case strings.Contains(errStr, "connection refused"):
		return "connection_refused"
	case strings.Contains(errStr, "connection reset"):
		return "connection_reset"
	case strings.Contains(errStr, "i/o timeout"):
		return "timeout"
	case strings.Contains(errStr, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(errStr, "tls:") || strings.Contains(errStr, "x509:"):
		return "tls_error"
	case strings.Contains(errStr, "PLAIN") || strings.Contains(errStr, "auth") || strings.Contains(errStr, "ACCESS_REFUSED"):
		return "auth_failed"
	case strings.Contains(errStr, "channel"):
		return "channel_error"
	default:
		return "rabbitmq_error"
	}
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

func (d *RabbitMQDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	exchange := destination.Config["exchange"]
	return destregistry.DestinationTarget{
		Target:    exchange + " -> " + strings.Join(destination.Topics, ", "),
		TargetURL: "",
	}
}
