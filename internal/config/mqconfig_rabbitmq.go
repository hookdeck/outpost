package config

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
)

type RabbitMQConfig struct {
	ServerURL     string `yaml:"server_url" env:"RABBITMQ_SERVER_URL" desc:"RabbitMQ server connection URL (e.g., 'amqp://user:pass@host:port/vhost'). Required if RabbitMQ is the chosen MQ provider." required:"C"`
	Exchange      string `yaml:"exchange" env:"RABBITMQ_EXCHANGE" desc:"Name of the RabbitMQ exchange to use." required:"N"`
	DeliveryQueue string `yaml:"delivery_queue" env:"RABBITMQ_DELIVERY_QUEUE" desc:"Name of the RabbitMQ queue for delivery events." required:"N"`
	LogQueue      string `yaml:"log_queue" env:"RABBITMQ_LOG_QUEUE" desc:"Name of the RabbitMQ queue for log events." required:"N"`
}

func (c *RabbitMQConfig) getQueueName(queueType string) string {
	switch queueType {
	case "deliverymq":
		return c.DeliveryQueue
	case "logmq":
		return c.LogQueue
	default:
		return ""
	}
}

func (c *RabbitMQConfig) ToInfraConfig(queueType string) *mqinfra.MQInfraConfig {
	return &mqinfra.MQInfraConfig{
		RabbitMQ: &mqinfra.RabbitMQInfraConfig{
			ServerURL: c.ServerURL,
			Exchange:  c.Exchange,
			Queue:     c.getQueueName(queueType),
		},
	}
}

func (c *RabbitMQConfig) ToQueueConfig(ctx context.Context, queueType string) (*mqs.QueueConfig, error) {
	return &mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: c.ServerURL,
			Exchange:  c.Exchange,
			Queue:     c.getQueueName(queueType),
		},
		VisibilityTimeout: 60 * time.Second,
	}, nil
}

func (c *RabbitMQConfig) GetProviderType() string {
	return "rabbitmq"
}

func (c *RabbitMQConfig) IsConfigured() bool {
	return c.ServerURL != ""
}
