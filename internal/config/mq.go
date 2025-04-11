package config

import (
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
)

// MQ Infrastructure configs
type AWSSQSConfig struct {
	AccessKeyID     string `yaml:"access_key_id" env:"AWS_SQS_ACCESS_KEY_ID"`
	SecretAccessKey string `yaml:"secret_access_key" env:"AWS_SQS_SECRET_ACCESS_KEY"`
	Region          string `yaml:"region" env:"AWS_SQS_REGION"`
	Endpoint        string `yaml:"endpoint" env:"AWS_SQS_ENDPOINT"`
	DeliveryQueue   string `yaml:"delivery_queue" env:"AWS_SQS_DELIVERY_QUEUE"`
	LogQueue        string `yaml:"log_queue" env:"AWS_SQS_LOG_QUEUE"`
}

type GCPPubSubConfig struct {
	Project                   string `yaml:"project" env:"GCP_PUBSUB_PROJECT"`
	ServiceAccountCredentials string `yaml:"service_account_credentials" env:"GCP_PUBSUB_SERVICE_ACCOUNT_CREDENTIALS"`
	DeliveryTopic             string `yaml:"delivery_topic" env:"GCP_PUBSUB_DELIVERY_TOPIC"`
	DeliverySubscription      string `yaml:"delivery_subscription" env:"GCP_PUBSUB_DELIVERY_SUBSCRIPTION"`
	LogTopic                  string `yaml:"log_topic" env:"GCP_PUBSUB_LOG_TOPIC"`
	LogSubscription           string `yaml:"log_subscription" env:"GCP_PUBSUB_LOG_SUBSCRIPTION"`
}

type RabbitMQConfig struct {
	ServerURL     string `yaml:"server_url" env:"RABBITMQ_SERVER_URL"`
	Exchange      string `yaml:"exchange" env:"RABBITMQ_EXCHANGE"`
	DeliveryQueue string `yaml:"delivery_queue" env:"RABBITMQ_DELIVERY_QUEUE"`
	LogQueue      string `yaml:"log_queue" env:"RABBITMQ_LOG_QUEUE"`
}

type MQsConfig struct {
	AWSSQS    AWSSQSConfig    `yaml:"aws_sqs"`
	GCPPubSub GCPPubSubConfig `yaml:"gcp_pubsub"`
	RabbitMQ  RabbitMQConfig  `yaml:"rabbitmq"`
}

func (c MQsConfig) GetInfraType() string {
	if hasAWSSQSConfig(c.AWSSQS) {
		return "awssqs"
	}
	if hasGCPPubSubConfig(c.GCPPubSub) {
		return "gcppubsub"
	}
	if hasRabbitMQConfig(c.RabbitMQ) {
		return "rabbitmq"
	}
	return ""
}

// getQueueConfig returns a queue config for the given queue type
// queueType can be "deliverymq" or "logmq"
func (c *MQsConfig) getQueueConfig(queueType string) *mqs.QueueConfig {
	if c == nil {
		return nil
	}

	infraType := c.GetInfraType()
	switch infraType {
	case "awssqs":
		queue := ""
		if queueType == "deliverymq" {
			queue = c.AWSSQS.DeliveryQueue
		} else if queueType == "logmq" {
			queue = c.AWSSQS.LogQueue
		}

		creds := fmt.Sprintf("%s:%s:", c.AWSSQS.AccessKeyID, c.AWSSQS.SecretAccessKey)
		return &mqs.QueueConfig{
			AWSSQS: &mqs.AWSSQSConfig{
				Endpoint:                  c.AWSSQS.Endpoint,
				Region:                    c.AWSSQS.Region,
				ServiceAccountCredentials: creds,
				Topic:                     queue,
			},
		}
	case "gcppubsub":
		topic := ""
		subscription := ""
		if queueType == "deliverymq" {
			topic = c.GCPPubSub.DeliveryTopic
			subscription = c.GCPPubSub.DeliverySubscription
		} else if queueType == "logmq" {
			topic = c.GCPPubSub.LogTopic
			subscription = c.GCPPubSub.LogSubscription
		}
		return &mqs.QueueConfig{
			GCPPubSub: &mqs.GCPPubSubConfig{
				ProjectID:                 c.GCPPubSub.Project,
				ServiceAccountCredentials: c.GCPPubSub.ServiceAccountCredentials,
				TopicID:                   topic,
				SubscriptionID:            subscription,
			},
		}
	case "rabbitmq":
		queue := ""
		if queueType == "deliverymq" {
			queue = c.RabbitMQ.DeliveryQueue
		} else if queueType == "logmq" {
			queue = c.RabbitMQ.LogQueue
		}
		return &mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: c.RabbitMQ.ServerURL,
				Exchange:  c.RabbitMQ.Exchange,
				Queue:     queue,
			},
		}
	default:
		return nil
	}
}

func (c MQsConfig) GetDeliveryQueueConfig() *mqs.QueueConfig {
	infraType := c.GetInfraType()
	switch infraType {
	case "awssqs":
		return c.getQueueConfig("deliverymq")
	case "gcppubsub":
		return c.getQueueConfig("deliverymq")
	case "rabbitmq":
		return c.getQueueConfig("deliverymq")
	default:
		return nil
	}
}

func (c MQsConfig) GetLogQueueConfig() *mqs.QueueConfig {
	infraType := c.GetInfraType()
	switch infraType {
	case "awssqs":
		return c.getQueueConfig("logmq")
	case "gcppubsub":
		return c.getQueueConfig("logmq")
	case "rabbitmq":
		return c.getQueueConfig("logmq")
	default:
		return nil
	}
}

// Helper functions to check for required fields
func hasAWSSQSConfig(config AWSSQSConfig) bool {
	return config.AccessKeyID != "" &&
		config.SecretAccessKey != "" && config.Region != ""
}

func hasGCPPubSubConfig(config GCPPubSubConfig) bool {
	return config.Project != ""
}

func hasRabbitMQConfig(config RabbitMQConfig) bool {
	return config.ServerURL != ""
}
