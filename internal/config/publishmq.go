package config

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/proxychain"
)

type PublishAWSSQSConfig struct {
	AccessKeyID     string `yaml:"access_key_id" env:"PUBLISH_AWS_SQS_ACCESS_KEY_ID" desc:"AWS Access Key ID for the SQS publish queue. Optional: omit (with the secret access key) to use the AWS SDK default credential chain, e.g. an IAM role." required:"N"`
	SecretAccessKey string `yaml:"secret_access_key" env:"PUBLISH_AWS_SQS_SECRET_ACCESS_KEY" desc:"AWS Secret Access Key for the SQS publish queue. Optional: omit (with the access key ID) to use the AWS SDK default credential chain, e.g. an IAM role." required:"N"`
	Region          string `yaml:"region" env:"PUBLISH_AWS_SQS_REGION" desc:"AWS Region for the SQS publish queue. Required if AWS SQS is the chosen publish MQ provider." required:"C"`
	Endpoint        string `yaml:"endpoint" env:"PUBLISH_AWS_SQS_ENDPOINT" desc:"Custom AWS SQS endpoint URL for the publish queue. Optional." required:"N"`
	Queue           string `yaml:"queue" env:"PUBLISH_AWS_SQS_QUEUE" desc:"Name of the SQS queue for publishing events. Required if AWS SQS is the chosen publish MQ provider." required:"C"`
	RoleARN         string `yaml:"role_arn" env:"PUBLISH_AWS_SQS_ROLE_ARN" desc:"ARN of an IAM role to assume for the SQS publish queue. Assumed with the access key, or with the AWS SDK default credential chain when no key is set. Optional." required:"N"`
	ExternalID      string `yaml:"external_id" env:"PUBLISH_AWS_SQS_EXTERNAL_ID" desc:"External ID sent when assuming the role in PUBLISH_AWS_SQS_ROLE_ARN. Optional." required:"N"`
	RoleSessionName string `yaml:"role_session_name" env:"PUBLISH_AWS_SQS_ROLE_SESSION_NAME" desc:"Session name used when assuming the role in PUBLISH_AWS_SQS_ROLE_ARN. Shown in the AWS CloudTrail logs of the role's account. Optional; defaults to 'outpost'." required:"N"`
	STSRegion       string `yaml:"sts_region" env:"PUBLISH_AWS_SQS_STS_REGION" desc:"AWS Region of the STS endpoint used to assume the role in PUBLISH_AWS_SQS_ROLE_ARN. Set it when the queue is in a Region that isn't enabled for the identity Outpost runs as, such as an opt-in Region. Optional; defaults to PUBLISH_AWS_SQS_REGION." required:"N"`
}

type PublishAzureServiceBusConfig struct {
	ConnectionString string `yaml:"connection_string" env:"PUBLISH_AZURE_SERVICEBUS_CONNECTION_STRING" desc:"Azure Service Bus connection string for the publish queue. Required if Azure Service Bus is the chosen publish MQ provider." required:"C"`
	Topic            string `yaml:"topic" env:"PUBLISH_AZURE_SERVICEBUS_TOPIC" desc:"Name of the Azure Service Bus topic for publishing events. Required if Azure Service Bus is the chosen publish MQ provider." required:"C"`
	Subscription     string `yaml:"subscription" env:"PUBLISH_AZURE_SERVICEBUS_SUBSCRIPTION" desc:"Name of the Azure Service Bus subscription to read published events from. Required if Azure Service Bus is the chosen publish MQ provider." required:"C"`
}

type PublishGCPPubSubConfig struct {
	Project                   string `yaml:"project" env:"PUBLISH_GCP_PUBSUB_PROJECT" desc:"GCP Project ID for the Pub/Sub publish topic. Required if GCP Pub/Sub is the chosen publish MQ provider." required:"C"`
	Topic                     string `yaml:"topic" env:"PUBLISH_GCP_PUBSUB_TOPIC" desc:"Name of the GCP Pub/Sub topic for publishing events. Required if GCP Pub/Sub is the chosen publish MQ provider." required:"C"`
	Subscription              string `yaml:"subscription" env:"PUBLISH_GCP_PUBSUB_SUBSCRIPTION" desc:"Name of the GCP Pub/Sub subscription to read published events from. Required if GCP Pub/Sub is the chosen publish MQ provider." required:"C"`
	ServiceAccountCredentials string `yaml:"service_account_credentials" env:"PUBLISH_GCP_PUBSUB_SERVICE_ACCOUNT_CREDENTIALS" desc:"GCP service account key JSON (inline) for the Pub/Sub publish topic. To use a key file, leave this empty and set GOOGLE_APPLICATION_CREDENTIALS. Required if GCP Pub/Sub is chosen and not using implicit credentials." required:"C"`
}

type PublishRabbitMQConfig struct {
	ServerURL string `yaml:"server_url" env:"PUBLISH_RABBITMQ_SERVER_URL" desc:"RabbitMQ server connection URL for the publish queue. Required if RabbitMQ is the chosen publish MQ provider." required:"C"`
	Exchange  string `yaml:"exchange" env:"PUBLISH_RABBITMQ_EXCHANGE" desc:"Name of the RabbitMQ exchange for the publish queue." required:"N"`
	Queue     string `yaml:"queue" env:"PUBLISH_RABBITMQ_QUEUE" desc:"Name of the RabbitMQ queue for publishing events. Required if RabbitMQ is the chosen publish MQ provider." required:"C"`
}

type PublishMQConfig struct {
	AWSSQS          PublishAWSSQSConfig          `yaml:"aws_sqs" desc:"Configuration for using AWS SQS as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	AzureServiceBus PublishAzureServiceBusConfig `yaml:"azure_servicebus" desc:"Configuration for using Azure Service Bus as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	GCPPubSub       PublishGCPPubSubConfig       `yaml:"gcp_pubsub" desc:"Configuration for using GCP Pub/Sub as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	RabbitMQ        PublishRabbitMQConfig        `yaml:"rabbitmq" desc:"Configuration for using RabbitMQ as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	ProxyURL        string                       `yaml:"proxy_url" env:"PUBLISH_PROXY_URL" desc:"HTTP CONNECT forward proxy for the publish queue connection, e.g. 'http://user:pass@proxy:10000'. Multiple whitespace-separated URLs are tunneled in order, nearest first. RabbitMQ only; ignored for other providers." required:"N"`
}

func (c PublishMQConfig) GetInfraType() string {
	if hasPublishAWSSQSConfig(c.AWSSQS) {
		return "awssqs"
	}
	if hasPublishAzureServiceBusConfig(c.AzureServiceBus) {
		return "azureservicebus"
	}
	if hasPublishGCPPubSubConfig(c.GCPPubSub) {
		return "gcppubsub"
	}
	if hasPublishRabbitMQConfig(c.RabbitMQ) {
		return "rabbitmq"
	}
	return ""
}

func (c *PublishMQConfig) GetQueueConfig() *mqs.QueueConfig {
	infraType := c.GetInfraType()
	switch infraType {
	case "awssqs":
		creds := fmt.Sprintf("%s:%s:", c.AWSSQS.AccessKeyID, c.AWSSQS.SecretAccessKey)
		return &mqs.QueueConfig{
			AWSSQS: &mqs.AWSSQSConfig{
				Endpoint:                  c.AWSSQS.Endpoint,
				Region:                    c.AWSSQS.Region,
				ServiceAccountCredentials: creds,
				Topic:                     c.AWSSQS.Queue,
				RoleARN:                   c.AWSSQS.RoleARN,
				ExternalID:                c.AWSSQS.ExternalID,
				RoleSessionName:           c.AWSSQS.RoleSessionName,
				STSRegion:                 c.AWSSQS.STSRegion,
			},
		}
	case "azureservicebus":
		return &mqs.QueueConfig{
			AzureServiceBus: &mqs.AzureServiceBusConfig{
				ConnectionString: c.AzureServiceBus.ConnectionString,
				Topic:            c.AzureServiceBus.Topic,
				Subscription:     c.AzureServiceBus.Subscription,
			},
		}
	case "gcppubsub":
		return &mqs.QueueConfig{
			GCPPubSub: &mqs.GCPPubSubConfig{
				ProjectID:                 c.GCPPubSub.Project,
				TopicID:                   c.GCPPubSub.Topic,
				SubscriptionID:            c.GCPPubSub.Subscription,
				ServiceAccountCredentials: c.GCPPubSub.ServiceAccountCredentials,
			},
		}
	case "rabbitmq":
		return &mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: c.RabbitMQ.ServerURL,
				Exchange:  c.RabbitMQ.Exchange,
				Queue:     c.RabbitMQ.Queue,
				Dial:      c.proxyDial(),
			},
		}
	default:
		return nil
	}
}

var errPublishAWSSQSRoleOptionsWithoutRole = errors.New("publish AWS SQS: external_id, role_session_name or sts_region is set without role_arn")

// Validate enforces the AWS SQS credential rules for the selected provider
// and rejects a malformed proxy chain.
func (c *PublishMQConfig) Validate() error {
	if _, err := proxychain.Parse(c.ProxyURL); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPublishProxyURL, err)
	}
	if c.GetInfraType() == "awssqs" {
		if (c.AWSSQS.AccessKeyID == "") != (c.AWSSQS.SecretAccessKey == "") {
			return errPartialAWSSQSCredentials
		}
		if (c.AWSSQS.ExternalID != "" || c.AWSSQS.RoleSessionName != "" || c.AWSSQS.STSRegion != "") && c.AWSSQS.RoleARN == "" {
			return errPublishAWSSQSRoleOptionsWithoutRole
		}
	}
	return nil
}

// hasPublishAWSSQSConfig selects SQS on Region alone; keys are optional so
// IAM-role auth can use the AWS SDK default credential chain.
func hasPublishAWSSQSConfig(config PublishAWSSQSConfig) bool {
	return config.Region != ""
}

func hasPublishAzureServiceBusConfig(config PublishAzureServiceBusConfig) bool {
	return config.ConnectionString != "" && config.Topic != "" && config.Subscription != ""
}

func hasPublishGCPPubSubConfig(config PublishGCPPubSubConfig) bool {
	return config.Project != ""
}

func hasPublishRabbitMQConfig(config PublishRabbitMQConfig) bool {
	return config.ServerURL != ""
}

// ProxyIgnored reports whether a proxy is configured for a provider that
// connects directly.
func (c *PublishMQConfig) ProxyIgnored() bool {
	infraType := c.GetInfraType()
	return strings.TrimSpace(c.ProxyURL) != "" && infraType != "" && infraType != "rabbitmq"
}

func (c *PublishMQConfig) proxyDial() proxychain.DialFunc {
	hops, err := proxychain.Parse(c.ProxyURL)
	if err != nil {
		// Validate rejects this at startup; never fall back to a direct dial.
		return func(context.Context, string, string) (net.Conn, error) { return nil, err }
	}
	if len(hops) == 0 {
		return nil
	}
	return proxychain.NewDialer(hops, nil, nil).DialContext
}
