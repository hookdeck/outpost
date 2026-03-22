package destkafka

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/partitionkey"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
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
}

var _ destregistry.Provider = (*KafkaDestination)(nil)

func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption) (*KafkaDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "kafka", basePublisherOpts...)
	if err != nil {
		return nil, err
	}
	return &KafkaDestination{BaseProvider: base}, nil
}

func (d *KafkaDestination) Validate(ctx context.Context, destination *models.Destination) error {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return err
	}

	// Validate SASL mechanism if provided
	saslMechanism := destination.Config["sasl_mechanism"]
	if saslMechanism != "" {
		switch saslMechanism {
		case "plain", "scram-sha-256", "scram-sha-512":
			// valid — require credentials when SASL is configured
			if destination.Credentials["username"] == "" || destination.Credentials["password"] == "" {
				return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
					{
						Field: "credentials",
						Type:  "required",
					},
				})
			}
		default:
			return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "config.sasl_mechanism",
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

	// Build transport
	transport := &kafka.Transport{}
	if mechanism != nil {
		transport.SASL = mechanism
	}
	if config.UseTLS {
		transport.TLS = &tls.Config{}
	}

	// Create writer with hash balancer for consistent partition key routing.
	// No WriteTimeout — the caller's context deadline (registry DeliveryTimeout)
	// controls the timeout, consistent with how other providers work.
	writer := &kafka.Writer{
		Addr:      kafka.TCP(config.Brokers...),
		Topic:     config.Topic,
		Balancer:  &kafka.Hash{},
		Transport: transport,
	}

	return &KafkaPublisher{
		BasePublisher:        d.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		writer:               writer,
		partitionKeyTemplate: config.PartitionKeyTemplate,
	}, nil
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
	writer               *kafka.Writer
	partitionKeyTemplate string
}

func (p *KafkaPublisher) Close() error {
	p.BasePublisher.StartClose()
	return p.writer.Close()
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
		return nil, destregistry.NewErrDestinationPublishAttempt(
			err, "kafka", map[string]interface{}{
				"error":   "format_failed",
				"message": err.Error(),
			},
		)
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
	headers := make([]kafka.Header, 0, len(meta)+1)
	headers = append(headers, kafka.Header{Key: "content-type", Value: []byte("application/json")})
	for k, v := range meta {
		headers = append(headers, kafka.Header{Key: k, Value: []byte(v)})
	}

	// Write message — send raw event data as value, metadata in headers only
	msg := kafka.Message{
		Key:     []byte(key),
		Value:   []byte(event.Data),
		Headers: headers,
	}

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   ClassifyKafkaError(err),
				Response: map[string]interface{}{
					"error": err.Error(),
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

// ClassifyKafkaError returns a descriptive error code based on the error type.
func ClassifyKafkaError(err error) string {
	if err == nil {
		return "unknown"
	}

	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "SASL") || strings.Contains(errStr, "authentication") || strings.Contains(errStr, "Authentication"):
		return "auth_failed"
	case strings.Contains(errStr, "connection refused"):
		return "connection_refused"
	case strings.Contains(errStr, "no such host"):
		return "dns_error"
	case strings.Contains(errStr, "Unknown Topic") || strings.Contains(errStr, "UNKNOWN_TOPIC"):
		return "topic_not_found"
	case strings.Contains(errStr, "Message Size Too Large") || strings.Contains(errStr, "MESSAGE_TOO_LARGE"):
		return "message_too_large"
	case strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "Timed Out"):
		return "timeout"
	case strings.Contains(errStr, "tls:") || strings.Contains(errStr, "x509:"):
		return "tls_error"
	default:
		return "kafka_error"
	}
}

// Helper functions

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
		return &plain.Mechanism{
			Username: creds.Username,
			Password: creds.Password,
		}, nil
	case "scram-sha-256":
		return scram.Mechanism(scram.SHA256, creds.Username, creds.Password)
	case "scram-sha-512":
		return scram.Mechanism(scram.SHA512, creds.Username, creds.Password)
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", mechanism)
	}
}
