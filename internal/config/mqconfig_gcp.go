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
	DeliveryDLQTopic          string `yaml:"delivery_dlq_topic" env:"GCP_PUBSUB_DELIVERY_DLQ_TOPIC" desc:"Name of the GCP Pub/Sub dead-letter topic for delivery events. Optional; defaults to '<delivery_topic>-dlq' if unset." required:"N"`
	DeliveryDLQSubscription   string `yaml:"delivery_dlq_subscription" env:"GCP_PUBSUB_DELIVERY_DLQ_SUBSCRIPTION" desc:"Name of the GCP Pub/Sub subscription on the delivery dead-letter topic. Optional; defaults to '<delivery_dlq_topic>-sub' if unset." required:"N"`
	LogDLQTopic               string `yaml:"log_dlq_topic" env:"GCP_PUBSUB_LOG_DLQ_TOPIC" desc:"Name of the GCP Pub/Sub dead-letter topic for log events. Optional; defaults to '<log_topic>-dlq' if unset." required:"N"`
	LogDLQSubscription        string `yaml:"log_dlq_subscription" env:"GCP_PUBSUB_LOG_DLQ_SUBSCRIPTION" desc:"Name of the GCP Pub/Sub subscription on the log dead-letter topic. Optional; defaults to '<log_dlq_topic>-sub' if unset." required:"N"`
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

func (c *GCPPubSubConfig) getDLQTopicByQueueType(queueType string) string {
	var dlqTopic string
	switch queueType {
	case "deliverymq":
		dlqTopic = c.DeliveryDLQTopic
	case "logmq":
		dlqTopic = c.LogDLQTopic
	default:
		return ""
	}
	if dlqTopic != "" {
		return dlqTopic
	}
	if topic := c.getTopicByQueueType(queueType); topic != "" {
		return mqinfra.DefaultGCPPubSubDLQTopicName(topic)
	}
	return ""
}

func (c *GCPPubSubConfig) getDLQSubscriptionByQueueType(queueType string) string {
	var dlqSub string
	switch queueType {
	case "deliverymq":
		dlqSub = c.DeliveryDLQSubscription
	case "logmq":
		dlqSub = c.LogDLQSubscription
	default:
		return ""
	}
	if dlqSub != "" {
		return dlqSub
	}
	if dlqTopic := c.getDLQTopicByQueueType(queueType); dlqTopic != "" {
		return mqinfra.DefaultGCPPubSubDLQSubscriptionName(dlqTopic)
	}
	return ""
}

func (c *GCPPubSubConfig) ToInfraConfig(queueType string) *mqinfra.MQInfraConfig {
	return &mqinfra.MQInfraConfig{
		GCPPubSub: &mqinfra.GCPPubSubInfraConfig{
			ProjectID:                 c.Project,
			ServiceAccountCredentials: c.ServiceAccountCredentials,
			TopicID:                   c.getTopicByQueueType(queueType),
			SubscriptionID:            c.getSubscriptionByQueueType(queueType),
			DLQTopicID:                c.getDLQTopicByQueueType(queueType),
			DLQSubscriptionID:         c.getDLQSubscriptionByQueueType(queueType),
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
