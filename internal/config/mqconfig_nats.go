package config

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
)

type NATSConfig struct {
	ServerURL       string `yaml:"server_url" env:"NATS_SERVER_URL" desc:"NATS server connection URL (e.g., 'nats://host:4222'). Required if NATS JetStream is the chosen MQ provider." required:"C"`
	Stream          string `yaml:"stream" env:"NATS_STREAM" desc:"Name of the JetStream stream to use." required:"N"`
	DeliverySubject string `yaml:"delivery_subject" env:"NATS_DELIVERY_SUBJECT" desc:"Subject for delivery events." required:"N"`
	LogSubject      string `yaml:"log_subject" env:"NATS_LOG_SUBJECT" desc:"Subject for log events." required:"N"`
	DeliveryDLQ     string `yaml:"delivery_dlq" env:"NATS_DELIVERY_DLQ" desc:"Subject for delivery messages that exhaust all delivery attempts. Optional; defaults to '<delivery_subject>.dlq' if unset." required:"N"`
	LogDLQ          string `yaml:"log_dlq" env:"NATS_LOG_DLQ" desc:"Subject for log messages that exhaust all delivery attempts. Optional; defaults to '<log_subject>.dlq' if unset." required:"N"`
}

func (c *NATSConfig) getSubject(queueType string) string {
	switch queueType {
	case "deliverymq":
		return c.DeliverySubject
	case "logmq":
		return c.LogSubject
	default:
		return ""
	}
}

func (c *NATSConfig) getDLQSubject(queueType string) string {
	var dlq string
	switch queueType {
	case "deliverymq":
		dlq = c.DeliveryDLQ
	case "logmq":
		dlq = c.LogDLQ
	default:
		return ""
	}
	if dlq != "" {
		return dlq
	}
	if subject := c.getSubject(queueType); subject != "" {
		return mqinfra.DefaultNATSDLQName(subject)
	}
	return ""
}

func (c *NATSConfig) ToInfraConfig(queueType string) *mqinfra.MQInfraConfig {
	return &mqinfra.MQInfraConfig{
		NATS: &mqinfra.NATSInfraConfig{
			ServerURL: c.ServerURL,
			Stream:    c.Stream,
			Subject:   c.getSubject(queueType),
			DLQ:       c.getDLQSubject(queueType),
		},
	}
}

func (c *NATSConfig) ToQueueConfig(ctx context.Context, queueType string) (*mqs.QueueConfig, error) {
	return &mqs.QueueConfig{
		NATS: &mqs.NATSConfig{
			ServerURL:  c.ServerURL,
			Stream:     c.Stream,
			Subject:    c.getSubject(queueType),
			DLQSubject: c.getDLQSubject(queueType),
			AckWait:    60 * time.Second,
		},
		VisibilityTimeout: 60 * time.Second,
	}, nil
}

func (c *NATSConfig) GetProviderType() string {
	return "nats"
}

func (c *NATSConfig) IsConfigured() bool {
	return c.ServerURL != ""
}
