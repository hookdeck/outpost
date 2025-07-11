package mqinfra

import (
	"context"
	"fmt"
)

type MQInfra interface {
	Exist(ctx context.Context) (bool, error)
	Declare(ctx context.Context) error
	TearDown(ctx context.Context) error
}

type MQInfraConfig struct {
	AWSSQS          *AWSSQSInfraConfig
	AzureServiceBus *AzureServiceBusInfraConfig
	GCPPubSub       *GCPPubSubInfraConfig
	RabbitMQ        *RabbitMQInfraConfig

	Policy Policy
}

type Policy struct {
	VisibilityTimeout int
	RetryLimit        int
}

type AWSSQSInfraConfig struct {
	Endpoint                  string
	Region                    string
	ServiceAccountCredentials string
	Topic                     string
}

type AzureServiceBusInfraConfig struct {
	ConnectionString string // If set, skip infra management

	TenantID       string
	ClientID       string
	ClientSecret   string
	SubscriptionID string
	ResourceGroup  string
	Namespace      string
	Topic          string
	Subscription   string
}

type GCPPubSubInfraConfig struct {
	ProjectID                 string
	TopicID                   string
	SubscriptionID            string
	ServiceAccountCredentials string
}

type RabbitMQInfraConfig struct {
	ServerURL string
	Exchange  string
	Queue     string
}

func New(cfg *MQInfraConfig) MQInfra {
	if cfg.AWSSQS != nil {
		return &infraAWSSQS{cfg: cfg}
	}
	if cfg.AzureServiceBus != nil {
		return &infraAzureServiceBus{cfg: cfg}
	}
	if cfg.GCPPubSub != nil {
		return &infraGCPPubSub{cfg: cfg}
	}
	if cfg.RabbitMQ != nil {
		return &infraRabbitMQ{cfg: cfg}
	}

	return &infraInvalid{}
}

type infraInvalid struct {
}

func (infra *infraInvalid) Exist(ctx context.Context) (bool, error) {
	return false, ErrInvalidConfig
}

func (infra *infraInvalid) Declare(ctx context.Context) error {
	return ErrInvalidConfig
}

func (infra *infraInvalid) TearDown(ctx context.Context) error {
	return ErrInvalidConfig
}

var (
	ErrInvalidConfig      = fmt.Errorf("invalid config")
	ErrInfraUnimplemented = fmt.Errorf("unimplemented infra")
)
