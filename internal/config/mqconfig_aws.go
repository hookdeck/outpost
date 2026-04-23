package config

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
)

type AWSSQSConfig struct {
	AccessKeyID     string `yaml:"access_key_id" env:"AWS_SQS_ACCESS_KEY_ID" desc:"AWS Access Key ID for SQS. Required if AWS SQS is the chosen MQ provider." required:"C"`
	SecretAccessKey string `yaml:"secret_access_key" env:"AWS_SQS_SECRET_ACCESS_KEY" desc:"AWS Secret Access Key for SQS. Required if AWS SQS is the chosen MQ provider." required:"C"`
	Region          string `yaml:"region" env:"AWS_SQS_REGION" desc:"AWS Region for SQS. Required if AWS SQS is the chosen MQ provider." required:"C"`
	Endpoint        string `yaml:"endpoint" env:"AWS_SQS_ENDPOINT" desc:"Custom AWS SQS endpoint URL. Optional, typically used for local testing (e.g., LocalStack)." required:"N"`
	DeliveryQueue   string `yaml:"delivery_queue" env:"AWS_SQS_DELIVERY_QUEUE" desc:"Name of the SQS queue for delivery events." required:"N"`
	LogQueue        string `yaml:"log_queue" env:"AWS_SQS_LOG_QUEUE" desc:"Name of the SQS queue for log events." required:"N"`
}

func (c *AWSSQSConfig) getQueueName(queueType string) string {
	switch queueType {
	case "deliverymq":
		return c.DeliveryQueue
	case "logmq":
		return c.LogQueue
	default:
		return ""
	}
}

func (c *AWSSQSConfig) getCredentials() string {
	return fmt.Sprintf("%s:%s:", c.AccessKeyID, c.SecretAccessKey)
}

func (c *AWSSQSConfig) ToInfraConfig(queueType string) *mqinfra.MQInfraConfig {
	return &mqinfra.MQInfraConfig{
		AWSSQS: &mqinfra.AWSSQSInfraConfig{
			Endpoint:                  c.Endpoint,
			Region:                    c.Region,
			ServiceAccountCredentials: c.getCredentials(),
			Topic:                     c.getQueueName(queueType),
		},
	}
}

func (c *AWSSQSConfig) ToQueueConfig(ctx context.Context, queueType string) (*mqs.QueueConfig, error) {
	return &mqs.QueueConfig{
		AWSSQS: &mqs.AWSSQSConfig{
			Endpoint:                  c.Endpoint,
			Region:                    c.Region,
			ServiceAccountCredentials: c.getCredentials(),
			Topic:                     c.getQueueName(queueType),
		},
		VisibilityTimeout: 60 * time.Second,
	}, nil
}

func (c *AWSSQSConfig) GetProviderType() string {
	return "awssqs"
}

func (c *AWSSQSConfig) IsConfigured() bool {
	return c.AccessKeyID != "" && c.SecretAccessKey != "" && c.Region != ""
}
