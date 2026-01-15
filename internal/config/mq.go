package config

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
)

type MQConfigAdapter interface {
	ToInfraConfig(queueType string) *mqinfra.MQInfraConfig
	ToQueueConfig(ctx context.Context, queueType string) (*mqs.QueueConfig, error)
	GetProviderType() string
	IsConfigured() bool
}

type MQsConfig struct {
	AWSSQS          AWSSQSConfig          `yaml:"aws_sqs" desc:"Configuration for using AWS SQS as the message queue. Only one MQ provider should be configured." required:"N"`
	AzureServiceBus AzureServiceBusConfig `yaml:"azure_servicebus" desc:"Configuration for using Azure Service Bus as the message queue. Only one MQ provider should be configured." required:"N"`
	GCPPubSub       GCPPubSubConfig       `yaml:"gcp_pubsub" desc:"Configuration for using GCP Pub/Sub as the message queue. Only one MQ provider should be configured." required:"N"`
	RabbitMQ        RabbitMQConfig        `yaml:"rabbitmq" desc:"Configuration for using RabbitMQ as the message queue. Only one MQ provider should be configured." required:"N"`
	AutoProvision   *bool                 `yaml:"auto_provision" env:"MQS_AUTO_PROVISION" desc:"Whether Outpost should create and manage message queue infrastructure. Set to false if you manage infrastructure externally (e.g., via Terraform). Defaults to true for backward compatibility." required:"N" default:"true"`

	adapter MQConfigAdapter
}

func (c *MQsConfig) init() {
	if c.adapter != nil {
		return
	}

	if c.AWSSQS.IsConfigured() {
		c.adapter = &c.AWSSQS
	} else if c.AzureServiceBus.IsConfigured() {
		c.adapter = &c.AzureServiceBus
	} else if c.GCPPubSub.IsConfigured() {
		c.adapter = &c.GCPPubSub
	} else if c.RabbitMQ.IsConfigured() {
		c.adapter = &c.RabbitMQ
	}
}

func (c *MQsConfig) GetInfraType() string {
	c.init()
	if c.adapter == nil {
		return ""
	}
	return c.adapter.GetProviderType()
}

func (c *MQsConfig) ToInfraConfig(queueType string) *mqinfra.MQInfraConfig {
	c.init()
	if c.adapter == nil {
		return nil
	}
	return c.adapter.ToInfraConfig(queueType)
}

func (c *MQsConfig) ToQueueConfig(ctx context.Context, queueType string) (*mqs.QueueConfig, error) {
	c.init()
	if c.adapter == nil {
		return nil, nil
	}
	return c.adapter.ToQueueConfig(ctx, queueType)
}
