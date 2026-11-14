package destkafka

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/partitionkey"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

const (
	// The read cap keeps a misbehaving or non-Kafka endpoint from making the
	// client allocate unbounded memory. A producer only reads ApiVersions,
	// SASL, single-topic Metadata and Produce responses, all far below it.
	maxBrokerReadBytes = 1 << 20
	// The broker default message.max.bytes.
	maxBatchBytes = 1048588
	dialTimeout   = 10 * time.Second
)

// Configuration types

type KafkaConfig struct {
	Brokers              []string
	Topic                string
	SASLMechanism        string
	UseTLS               bool
	PartitionKeyTemplate string
}

type KafkaCredentials struct {
	Username string
	Password string
}

// Provider implementation

type KafkaDestination struct {
	*destregistry.BaseProvider
	proxyDial proxychain.DialFunc
}

var _ destregistry.Provider = (*KafkaDestination)(nil)

type Option func(*KafkaDestination)

// WithProxy connects to brokers through the given forward proxies, nearest
// hop first. TLS runs end to end through the tunnel.
func WithProxy(hops []*url.URL) Option {
	return func(d *KafkaDestination) {
		d.proxyDial = destregistry.ProxyDialFunc(hops)
	}
}

func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption, opts ...Option) (*KafkaDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "kafka", basePublisherOpts...)
	if err != nil {
		return nil, err
	}
	d := &KafkaDestination{BaseProvider: base}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
}

func (d *KafkaDestination) Validate(ctx context.Context, destination *models.Destination) error {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return err
	}

	// Validate SASL mechanism value (field itself is required via metadata)
	saslMechanism := destination.Config["sasl_mechanism"]
	switch saslMechanism {
	case "plain", "scram-sha-256", "scram-sha-512":
		// valid
	default:
		return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "config.sasl_mechanism",
				Type:  "invalid",
			},
		})
	}

	// A broker without a port defaults to 9092; one with a port needs a valid one.
	for _, broker := range parseBrokers(destination.Config["brokers"]) {
		if !validBrokerAddr(broker) {
			return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "config.brokers",
					Type:  "invalid",
				},
			})
		}
	}

	// Validate TLS config if provided — empty string is treated as "not configured"
	if tlsStr, ok := destination.Config["tls"]; ok && tlsStr != "" {
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

// validBrokerAddr accepts what kgo can parse, with a port in 1-65535 when
// one is given.
func validBrokerAddr(addr string) bool {
	var port string
	if strings.HasPrefix(addr, "[") {
		parts := strings.Split(addr[1:], "]")
		if len(parts) != 2 {
			return false
		}
		if parts[1] == "" {
			return true
		}
		if parts[1][0] != ':' {
			return false
		}
		port = parts[1][1:]
	} else {
		_, p, err := net.SplitHostPort(addr)
		if err != nil {
			return true
		}
		port = p
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func (d *KafkaDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	config, credentials, err := d.resolveConfig(ctx, destination)
	if err != nil {
		return nil, err
	}

	// Build SASL mechanism
	var mechanism sasl.Mechanism
	if config.SASLMechanism != "" {
		mechanism, err = buildSASLMechanism(config.SASLMechanism, credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to configure SASL: %w", err)
		}
	}

	dials := &dialErrs{}
	opts := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.DefaultProduceTopic(config.Topic),
		kgo.ClientID("outpost"),
		kgo.Dialer(brokerDialer(d.proxyDial, config.UseTLS, dials)),
		kgo.BrokerMaxReadBytes(maxBrokerReadBytes),
		// kgo rejects a fetch limit above the read cap; the producer never fetches.
		kgo.FetchMaxBytes(maxBrokerReadBytes),
		kgo.RecordPartitioner(newPartitioner()),
		// No broker acks; kgo requires idempotent writes off for that.
		kgo.RequiredAcks(kgo.NoAck()),
		kgo.DisableIdempotentWrite(),
		kgo.ProducerBatchCompression(kgo.NoCompression()),
		kgo.ProducerBatchMaxBytes(maxBatchBytes),
		kgo.ProducerLinger(0),
		// Few retries and quick metadata refreshes, so a failing broker
		// returns its error before the delivery times out.
		kgo.RecordRetries(2),
		kgo.MetadataMinAge(250 * time.Millisecond),
	}
	if mechanism != nil {
		opts = append(opts, kgo.SASL(mechanism))
	}
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &KafkaPublisher{
		BasePublisher:        d.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		client:               client,
		dials:                dials,
		partitionKeyTemplate: config.PartitionKeyTemplate,
		proxied:              d.proxyDial != nil,
		brokerHosts:          brokerHosts(config.Brokers),
	}, nil
}

// newPartitioner places keyed records by FNV-1a modulo the partition count.
// Keep it stable: a change moves existing keys to other partitions.
func newPartitioner() kgo.Partitioner {
	return kgo.StickyKeyPartitioner(kgo.SaramaCompatHasher(func(b []byte) uint32 {
		h := fnv.New32a()
		_, _ = h.Write(b)
		return h.Sum32()
	}))
}

// dialErrs remembers the latest dial error. kgo drops the cause of a failed
// produce-path dial and reports only ErrRecordRetries.
type dialErrs struct {
	mu  sync.Mutex
	err error
	at  time.Time
}

func (d *dialErrs) record(err error) {
	d.mu.Lock()
	d.err, d.at = err, time.Now()
	d.mu.Unlock()
}

func (d *dialErrs) since(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil && !d.at.Before(t) {
		return d.err
	}
	return nil
}

// brokerDialer dials through the proxy chain when set, and runs TLS end to
// end over the connection.
func brokerDialer(proxyDial proxychain.DialFunc, useTLS bool, rec *dialErrs) func(ctx context.Context, network, addr string) (net.Conn, error) {
	var base proxychain.DialFunc = (&net.Dialer{}).DialContext
	if proxyDial != nil {
		base = proxyDial
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		conn, err := base(ctx, network, addr)
		if err == nil && useTLS {
			host, _, splitErr := net.SplitHostPort(addr)
			if splitErr != nil {
				host = addr
			}
			tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
			if err = tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
			} else {
				conn = tlsConn
			}
		}
		if err != nil {
			rec.record(err)
			return nil, err
		}
		return conn, nil
	}
}

func (d *KafkaDestination) resolveConfig(ctx context.Context, destination *models.Destination) (*KafkaConfig, *KafkaCredentials, error) {
	if err := d.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	// Parse brokers
	brokersStr := destination.Config["brokers"]
	brokers := parseBrokers(brokersStr)

	useTLS := false
	if tlsStr, ok := destination.Config["tls"]; ok {
		useTLS = tlsStr == "true" || tlsStr == "on"
	}

	return &KafkaConfig{
			Brokers:              brokers,
			Topic:                destination.Config["topic"],
			SASLMechanism:        destination.Config["sasl_mechanism"],
			UseTLS:               useTLS,
			PartitionKeyTemplate: destination.Config["partition_key_template"],
		}, &KafkaCredentials{
			Username: destination.Credentials["username"],
			Password: destination.Credentials["password"],
		}, nil
}

func (d *KafkaDestination) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	if newDestination.Config == nil {
		return nil
	}

	// Normalize TLS value
	if newDestination.Config["tls"] == "on" {
		newDestination.Config["tls"] = "true"
	} else if newDestination.Config["tls"] == "" {
		newDestination.Config["tls"] = "false"
	}

	// Trim whitespace from brokers
	if brokers := newDestination.Config["brokers"]; brokers != "" {
		parts := strings.Split(brokers, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		newDestination.Config["brokers"] = strings.Join(parts, ",")
	}

	if _, _, err := d.resolveConfig(context.Background(), newDestination); err != nil {
		return err
	}
	return nil
}

func (d *KafkaDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	brokers := parseBrokers(destination.Config["brokers"])
	topic := destination.Config["topic"]

	var target string
	if len(brokers) > 0 {
		target = fmt.Sprintf("%s / %s", brokers[0], topic)
	} else {
		target = topic
	}

	return destregistry.DestinationTarget{
		Target:    target,
		TargetURL: "",
	}
}

// Publisher implementation

type KafkaPublisher struct {
	*destregistry.BasePublisher
	client               *kgo.Client
	dials                *dialErrs
	partitionKeyTemplate string
	proxied              bool
	brokerHosts          string
}

func (p *KafkaPublisher) Close() error {
	p.BasePublisher.StartClose()
	p.client.Close()
	return nil
}

func (p *KafkaPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	// Build metadata for headers and partition key evaluation
	meta := p.BasePublisher.MakeMetadata(event, time.Now())
	metadataMap := make(map[string]interface{})
	for k, v := range meta {
		metadataMap[k] = v
	}

	// Build parsed payload for partition key JMESPath evaluation
	dataMap, err := event.ParsedData()
	if err != nil {
		return destregistry.NewFormatError("kafka", "", err)
	}
	if dataMap == nil {
		dataMap = make(map[string]interface{})
	}
	payload := map[string]interface{}{
		"metadata": metadataMap,
		"data":     dataMap,
	}

	// Evaluate partition key
	key, err := partitionkey.Evaluate(p.partitionKeyTemplate, payload, event.ID)
	if err != nil {
		key = event.ID
	}

	// Build Kafka headers from metadata
	headers := make([]kgo.RecordHeader, 0, len(meta)+1)
	headers = append(headers, kgo.RecordHeader{Key: "content-type", Value: []byte("application/json")})
	for k, v := range meta {
		headers = append(headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}

	// Write message — send raw event data as value, metadata in headers only
	record := &kgo.Record{
		Key:     []byte(key),
		Value:   []byte(event.Data),
		Headers: headers,
	}

	start := time.Now()
	if err := p.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		// kgo reports a failed leader dial as ErrRecordRetries alone;
		// attach the dial error.
		if errors.Is(err, kgo.ErrRecordRetries) {
			if dialErr := p.dials.since(start); dialErr != nil {
				err = fmt.Errorf("%w: %w", err, dialErr)
			}
		}
		if destregistry.IsProxyError(err) {
			return destregistry.ProxyPublishResult(err, "kafka")
		}
		return &destregistry.Delivery{
				Status: "failed",
				Code:   ClassifyKafkaError(err),
				Response: map[string]interface{}{
					"error": p.responseError(err),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "kafka", map[string]interface{}{
				"error":   ClassifyKafkaError(err),
				"message": err.Error(),
			})
	}

	return &destregistry.Delivery{
		Status:   "success",
		Code:     "OK",
		Response: map[string]interface{}{},
	}, nil
}

// responseError is the user-visible error. Through a proxy, network
// errors name the proxy's address, so only broker and TLS errors are kept
// verbatim.
func (p *KafkaPublisher) responseError(err error) string {
	var kafkaErr *kerr.Error
	if !p.proxied || errors.As(err, &kafkaErr) {
		return err.Error()
	}
	code := ClassifyKafkaError(err)
	if code == "tls_error" {
		return err.Error()
	}
	return fmt.Sprintf("%s connecting to %s", code, p.brokerHosts)
}

// ClassifyKafkaError returns a descriptive error code based on the error type.
func ClassifyKafkaError(err error) string {
	if err == nil {
		return "unknown"
	}

	errStr := err.Error()

	var netErr net.Error
	var dnsErr *net.DNSError

	switch {
	case errors.Is(err, kerr.SaslAuthenticationFailed) || errors.Is(err, kerr.UnsupportedSaslMechanism) || errors.Is(err, kerr.IllegalSaslState) ||
		strings.Contains(errStr, "SASL") || strings.Contains(errStr, "authentication") || strings.Contains(errStr, "Authentication"):
		return "auth_failed"
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(errStr, "connection refused"):
		return "connection_refused"
	case errors.As(err, &dnsErr) || strings.Contains(errStr, "no such host"):
		return "dns_error"
	case errors.Is(err, kerr.UnknownTopicOrPartition) || strings.Contains(errStr, "UNKNOWN_TOPIC"):
		return "topic_not_found"
	case errors.Is(err, kerr.MessageTooLarge) || strings.Contains(errStr, "MESSAGE_TOO_LARGE"):
		return "message_too_large"
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, kgo.ErrRecordTimeout) || errors.Is(err, kerr.RequestTimedOut) ||
		(errors.As(err, &netErr) && netErr.Timeout()) ||
		strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "TIMED_OUT"):
		return "timeout"
	case strings.Contains(errStr, "tls:") || strings.Contains(errStr, "x509:"):
		return "tls_error"
	default:
		return "kafka_error"
	}
}

// Helper functions

// brokerHosts lists the distinct broker hosts, without ports.
func brokerHosts(brokers []string) string {
	hosts := make([]string, 0, len(brokers))
	seen := make(map[string]bool, len(brokers))
	for _, b := range brokers {
		host := b
		if h, _, err := net.SplitHostPort(b); err == nil {
			host = h
		}
		if !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	return strings.Join(hosts, ",")
}

func parseBrokers(brokersStr string) []string {
	if brokersStr == "" {
		return nil
	}
	parts := strings.Split(brokersStr, ",")
	brokers := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			brokers = append(brokers, trimmed)
		}
	}
	return brokers
}

func buildSASLMechanism(mechanism string, creds *KafkaCredentials) (sasl.Mechanism, error) {
	switch mechanism {
	case "plain":
		return plain.Auth{User: creds.Username, Pass: creds.Password}.AsMechanism(), nil
	case "scram-sha-256":
		return scram.Auth{User: creds.Username, Pass: creds.Password}.AsSha256Mechanism(), nil
	case "scram-sha-512":
		return scram.Auth{User: creds.Username, Pass: creds.Password}.AsSha512Mechanism(), nil
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", mechanism)
	}
}
