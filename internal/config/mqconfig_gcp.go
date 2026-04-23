package config

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
)

type GCPPubSubConfig struct {
	Project                   string `yaml:"project" env:"GCP_PUBSUB_PROJECT" desc:"GCP Project ID for Pub/Sub. Required if GCP Pub/Sub is the chosen MQ provider." required:"C"`
	ServiceAccountCredentials string `yaml:"service_account_credentials" env:"GCP_PUBSUB_SERVICE_ACCOUNT_CREDENTIALS" desc:"JSON string or path to a file containing GCP service account credentials for Pub/Sub. Required if GCP Pub/Sub is the chosen MQ provider and not running in an environment with implicit credentials (e.g., GCE, GKE)." required:"C"`
	DeliveryTopic             string `yaml:"delivery_topic" env:"GCP_PUBSUB_DELIVERY_TOPIC" desc:"Name of the GCP Pub/Sub topic for delivery events." required:"N"`
	DeliverySubscription      string `yaml:"delivery_subscription" env:"GCP_PUBSUB_DELIVERY_SUBSCRIPTION" desc:"Name of the GCP Pub/Sub subscription for delivery events." required:"N"`
	LogTopic                  string `yaml:"log_topic" env:"GCP_PUBSUB_LOG_TOPIC" desc:"Name of the GCP Pub/Sub topic for log events." required:"N"`
	LogSubscription           string `yaml:"log_subscription" env:"GCP_PUBSUB_LOG_SUBSCRIPTION" desc:"Name of the GCP Pub/Sub subscription for log events." required:"N"`
}

func (c *GCPPubSubConfig) getTopicByQueueType(queueType string) string {
	switch queueType {
	case "deliverymq":
		return c.DeliveryTopic
	case "logmq":
		return c.LogTopic
	default:
		return ""
	}
}

func (c *GCPPubSubConfig) getSubscriptionByQueueType(queueType string) string {
	switch queueType {
	case "deliverymq":
		return c.DeliverySubscription
	case "logmq":
		return c.LogSubscription
	default:
		return ""
	}
}

func (c *GCPPubSubConfig) ToInfraConfig(queueType string) *mqinfra.MQInfraConfig {
	return &mqinfra.MQInfraConfig{
		GCPPubSub: &mqinfra.GCPPubSubInfraConfig{
			ProjectID:                 c.Project,
			ServiceAccountCredentials: c.ServiceAccountCredentials,
			TopicID:                   c.getTopicByQueueType(queueType),
			SubscriptionID:            c.getSubscriptionByQueueType(queueType),
		},
	}
}

func (c *GCPPubSubConfig) ToQueueConfig(ctx context.Context, queueType string) (*mqs.QueueConfig, error) {
	return &mqs.QueueConfig{
		GCPPubSub: &mqs.GCPPubSubConfig{
			ProjectID:                 c.Project,
			ServiceAccountCredentials: c.ServiceAccountCredentials,
			TopicID:                   c.getTopicByQueueType(queueType),
			SubscriptionID:            c.getSubscriptionByQueueType(queueType),
		},
		VisibilityTimeout: 60 * time.Second,
	}, nil
}

func (c *GCPPubSubConfig) GetProviderType() string {
	return "gcppubsub"
}

func (c *GCPPubSubConfig) IsConfigured() bool {
	return c.Project != ""
}
