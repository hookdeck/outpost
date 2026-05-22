package config

import (
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
)

type PublishAWSSQSConfig struct {
	AccessKeyID     string `yaml:"access_key_id" env:"PUBLISH_AWS_SQS_ACCESS_KEY_ID" desc:"AWS Access Key ID for the SQS publish queue. Required if AWS SQS is the chosen publish MQ provider." required:"C"`
	SecretAccessKey string `yaml:"secret_access_key" env:"PUBLISH_AWS_SQS_SECRET_ACCESS_KEY" desc:"AWS Secret Access Key for the SQS publish queue. Required if AWS SQS is the chosen publish MQ provider." required:"C"`
	Region          string `yaml:"region" env:"PUBLISH_AWS_SQS_REGION" desc:"AWS Region for the SQS publish queue. Required if AWS SQS is the chosen publish MQ provider." required:"C"`
	Endpoint        string `yaml:"endpoint" env:"PUBLISH_AWS_SQS_ENDPOINT" desc:"Custom AWS SQS endpoint URL for the publish queue. Optional." required:"N"`
	Queue           string `yaml:"queue" env:"PUBLISH_AWS_SQS_QUEUE" desc:"Name of the SQS queue for publishing events. Required if AWS SQS is the chosen publish MQ provider." required:"C"`
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
	ServiceAccountCredentials string `yaml:"service_account_credentials" env:"PUBLISH_GCP_PUBSUB_SERVICE_ACCOUNT_CREDENTIALS" desc:"JSON string or path to a file containing GCP service account credentials for the Pub/Sub publish topic. Required if GCP Pub/Sub is chosen and not using implicit credentials." required:"C"`
}

type PublishRabbitMQConfig struct {
	ServerURL string `yaml:"server_url" env:"PUBLISH_RABBITMQ_SERVER_URL" desc:"RabbitMQ server connection URL for the publish queue. Required if RabbitMQ is the chosen publish MQ provider." required:"C"`
	Exchange  string `yaml:"exchange" env:"PUBLISH_RABBITMQ_EXCHANGE" desc:"Name of the RabbitMQ exchange for the publish queue." required:"N"`
	Queue     string `yaml:"queue" env:"PUBLISH_RABBITMQ_QUEUE" desc:"Name of the RabbitMQ queue for publishing events. Required if RabbitMQ is the chosen publish MQ provider." required:"C"`
}

type PublishNATSAccountConfig struct {
	Name            string `yaml:"name" desc:"Account label used for logging and metrics. Must be unique within the publish source." required:"C"`
	CredentialsFile string `yaml:"credentials_file" desc:"Path to the NATS .creds file (JWT + NKey seed) for this account." required:"C"`
	Stream          string `yaml:"stream" desc:"JetStream stream name to consume from. Must be pre-created on the NATS side." required:"C"`
	Consumer        string `yaml:"consumer" desc:"Durable JetStream consumer name. Must be pre-created on the NATS side." required:"C"`
	TenantID        string `yaml:"tenant_id" desc:"Outpost tenant_id to stamp on every event from this account. Recommended for one-account-per-tenant setups; overrides any tenant_id in the payload." required:"N"`
}

type PublishNATSConfig struct {
	Servers     []string                   `yaml:"servers" env:"PUBLISH_NATS_SERVERS" envSeparator:"," desc:"NATS cluster URLs for the publish source (comma-separated, e.g. 'nats://a:4222,nats://b:4222'). Required if NATS is the chosen publish MQ provider." required:"C"`
	AccountsDir string                     `yaml:"accounts_dir" env:"PUBLISH_NATS_ACCOUNTS_DIR" desc:"Directory containing per-tenant NATS account subdirectories (each with a meta.yaml plus credentials file). Watched for runtime changes. Combined with 'accounts' if both are set." required:"N"`
	Accounts    []PublishNATSAccountConfig `yaml:"accounts" desc:"Static list of NATS accounts to consume from. Alternative or supplement to accounts_dir." required:"N"`
}

type PublishMQConfig struct {
	AWSSQS          PublishAWSSQSConfig          `yaml:"aws_sqs" desc:"Configuration for using AWS SQS as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	AzureServiceBus PublishAzureServiceBusConfig `yaml:"azure_servicebus" desc:"Configuration for using Azure Service Bus as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	GCPPubSub       PublishGCPPubSubConfig       `yaml:"gcp_pubsub" desc:"Configuration for using GCP Pub/Sub as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	RabbitMQ        PublishRabbitMQConfig        `yaml:"rabbitmq" desc:"Configuration for using RabbitMQ as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
	NATS            PublishNATSConfig            `yaml:"nats" desc:"Configuration for using NATS JetStream as the publish message queue. Only one publish MQ provider should be configured." required:"N"`
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
	if hasPublishNATSConfig(c.NATS) {
		return "nats"
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
			},
		}
	case "nats":
		accounts := make([]mqs.NATSAccountConfig, 0, len(c.NATS.Accounts))
		for _, a := range c.NATS.Accounts {
			accounts = append(accounts, mqs.NATSAccountConfig{
				Name:            a.Name,
				CredentialsFile: a.CredentialsFile,
				Stream:          a.Stream,
				Consumer:        a.Consumer,
				TenantID:        a.TenantID,
			})
		}
		return &mqs.QueueConfig{
			NATS: &mqs.NATSConfig{
				Servers:     c.NATS.Servers,
				AccountsDir: c.NATS.AccountsDir,
				Accounts:    accounts,
			},
		}
	default:
		return nil
	}
}

func hasPublishAWSSQSConfig(config PublishAWSSQSConfig) bool {
	return config.AccessKeyID != "" &&
		config.SecretAccessKey != "" && config.Region != ""
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

func hasPublishNATSConfig(config PublishNATSConfig) bool {
	if len(config.Servers) == 0 {
		return false
	}
	return config.AccountsDir != "" || len(config.Accounts) > 0
}
