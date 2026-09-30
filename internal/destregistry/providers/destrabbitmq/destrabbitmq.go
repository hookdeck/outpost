package destrabbitmq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQDestination struct {
	*destregistry.BaseProvider
	proxyDial proxychain.DialFunc
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

type Option func(*RabbitMQDestination)

// WithProxy connects to brokers through the given forward proxies, nearest
// hop first. amqps TLS runs end to end through the tunnel.
func WithProxy(hops []*url.URL) Option {
	return func(d *RabbitMQDestination) {
		d.proxyDial = destregistry.ProxyDialFunc(hops)
	}
}

func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption, opts ...Option) (*RabbitMQDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "rabbitmq", basePublisherOpts...)
	if err != nil {
		return nil, err
	}
	d := &RabbitMQDestination{BaseProvider: base}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
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
	amqpURL := rabbitURL(config, credentials)
	host := config.ServerURL
	if uri, err := amqp091.ParseURI(amqpURL); err == nil {
		host = uri.Host
	}
	return &RabbitMQPublisher{
		BasePublisher: d.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		url:           amqpURL,
		host:          host,
		exchange:      config.Exchange,
		proxyDial:     d.proxyDial,
		lock:          make(chan struct{}, 1),
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
	host     string
	exchange string
	// proxyDial, when set, opens the broker connection through a proxy chain.
	proxyDial proxychain.DialFunc
	conn      *amqp091.Connection
	channel   *amqp091.Channel
	// lock guards conn and channel. A channel rather than a mutex so a
	// delivery waiting on another's dial gives up at its own deadline.
	lock chan struct{}
}

func (p *RabbitMQPublisher) Close() error {
	p.BasePublisher.StartClose()

	p.lock <- struct{}{}
	defer func() { <-p.lock }()

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

	if err := p.ensureConnection(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		if destregistry.IsProxyError(err) {
			return destregistry.ProxyPublishResult(err, "rabbitmq")
		}
		return &destregistry.Delivery{
				Status: "failed",
				Code:   ClassifyRabbitMQError(err),
				Response: map[string]interface{}{
					"error": p.responseError(err),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "rabbitmq", map[string]interface{}{
				"error":   "connection_failed",
				"message": err.Error(),
			})
	}

	dataBytes := []byte(event.Data)

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
		return &destregistry.Delivery{
				Status: "failed",
				Code:   ClassifyRabbitMQError(err),
				Response: map[string]interface{}{
					"error": p.responseError(err),
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

// responseError is the customer-visible error. Through a proxy, network
// errors name the proxy's address, so only broker and TLS errors are kept
// verbatim.
func (p *RabbitMQPublisher) responseError(err error) string {
	var amqpErr *amqp091.Error
	if p.proxyDial == nil || (errors.As(err, &amqpErr) && amqpErr.Code != amqp091.FrameError) {
		return err.Error()
	}
	code := ClassifyRabbitMQError(err)
	if code == "tls_error" {
		return err.Error()
	}
	return fmt.Sprintf("%s connecting to %s", code, p.host)
}

func (p *RabbitMQPublisher) ensureConnection(ctx context.Context) error {
	select {
	case p.lock <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("failed to connect to RabbitMQ: %w", ctx.Err())
	}
	defer func() { <-p.lock }()

	if p.conn != nil && !p.conn.IsClosed() && p.channel != nil && !p.channel.IsClosed() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create new connection
	conn, err := connect(ctx, p.url, p.proxyDial)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create channel
	channel, err := openChannel(ctx, conn)
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

// connect opens the connection within ctx: the TCP dial, TLS and AMQP
// handshakes all stop at ctx's deadline or the URL's connection_timeout
// (default 30s), whichever comes first. A nil dialContext dials directly.
func connect(ctx context.Context, amqpURL string, dialContext proxychain.DialFunc) (*amqp091.Connection, error) {
	uri, err := amqp091.ParseURI(amqpURL)
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if uri.ConnectionTimeout != 0 {
		timeout = time.Duration(uri.ConnectionTimeout) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if dialContext == nil {
		dialContext = (&net.Dialer{}).DialContext
	}
	// The deadline bounds the TLS and AMQP handshakes; cancel cuts them
	// short too.
	var stop func() bool
	conn, err := amqp091.DialConfig(amqpURL, amqp091.Config{
		Dial: func(network, addr string) (net.Conn, error) {
			conn, err := dialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			// amqp091 clears the deadline once the connection is open.
			deadline, _ := ctx.Deadline()
			if err := conn.SetDeadline(deadline); err != nil {
				conn.Close()
				return nil, err
			}
			stop = context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
			return conn, nil
		},
	})
	if stop != nil && !stop() {
		if err == nil {
			_ = conn.CloseDeadline(time.Now())
			return nil, ctx.Err()
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
	}
	return conn, err
}

// openChannel opens a channel on conn, dropping conn if ctx ends first:
// amqp091 has no deadline once the handshake is done.
func openChannel(ctx context.Context, conn *amqp091.Connection) (*amqp091.Channel, error) {
	type result struct {
		channel *amqp091.Channel
		err     error
	}
	done := make(chan result, 1)
	go func() {
		channel, err := conn.Channel()
		done <- result{channel, err}
	}()
	select {
	case r := <-done:
		return r.channel, r.err
	case <-ctx.Done():
		// Close would wait for the broker's close-ok.
		_ = conn.CloseDeadline(time.Now())
		<-done
		return nil, ctx.Err()
	}
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
		case amqp091.FrameError:
			// The handshake deadline surfaces as a frame error.
			if strings.Contains(errStr, "i/o timeout") {
				return "timeout"
			}
			return "rabbitmq_error"
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
	p.lock <- struct{}{}
	defer func() { <-p.lock }()
	return p.conn
}

func (p *RabbitMQPublisher) ForceConnectionClose() {
	p.lock <- struct{}{}
	defer func() { <-p.lock }()
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
