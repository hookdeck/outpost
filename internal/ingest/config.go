package ingest

import (
	"errors"

	"github.com/spf13/viper"
)

type IngestConfig struct {
	AWSSQS          *AWSSQSConfig
	AzureServiceBus *AzureServiceBusConfig
	GCPPubSub       *GCPPubSubConfig
	RabbitMQ        *RabbitMQConfig
	// mainly for testing purposes
	InMemory *InMemoryConfig
}

type AWSSQSConfig struct {
	ServiceAccountCredentials string
	PublishTopic              string
}

type AzureServiceBusConfig struct {
}

type GCPPubSubConfig struct {
}

type RabbitMQConfig struct {
	ServerURL       string
	PublishExchange string
	PublishQueue    string
}

type InMemoryConfig struct {
	Name string
}

func ParseIngestConfig(viper *viper.Viper) (*IngestConfig, error) {
	config := &IngestConfig{}
	config.GCPPubSub = nil
	config.AzureServiceBus = nil

	config.parseAWSSQSConfig(viper)
	config.parseRabbitMQConfig(viper)

	validationErr := config.Validate()
	if validationErr != nil {
		return nil, validationErr
	}

	return config, nil
}

func (c *IngestConfig) Validate() error {
	configCount := 0

	if c.AWSSQS != nil {
		configCount++
		if err := c.validateAWSSQSConfig(); err != nil {
			return err
		}
	}

	if c.AzureServiceBus != nil {
		configCount++
		if err := c.validateAzureServiceBusConfig(); err != nil {
			return err
		}
	}

	if c.GCPPubSub != nil {
		configCount++
		if err := c.validateAWSSQSConfig(); err != nil {
			return err
		}
	}

	if c.RabbitMQ != nil {
		configCount++
		if err := c.validateRabbitMQConfig(); err != nil {
			return err
		}
	}

	if configCount > 1 {
		return errors.New("only one of AWS SQS, GCP PubSub, Azure Service Bus, or RabbitMQ should be configured")
	}

	return nil
}

// ==================================== AWS SQS ====================================

func (c *IngestConfig) parseAWSSQSConfig(viper *viper.Viper) {
	if !viper.IsSet("AWS_SQS_SERVICE_ACCOUNT_CREDS") {
		return
	}

	config := &AWSSQSConfig{}
	config.ServiceAccountCredentials = viper.GetString("AWS_SQS_SERVICE_ACCOUNT_CREDS")
	config.PublishTopic = viper.GetString("AWS_SQS_PUBLISH_TOPIC")

	c.AWSSQS = config
}

func (c *IngestConfig) validateAWSSQSConfig() error {
	if c.AWSSQS == nil {
		return nil
	}

	if c.AWSSQS.ServiceAccountCredentials == "" {
		return errors.New("AWS SQS Service Account Credentials is not set")
	}

	if c.AWSSQS.PublishTopic == "" {
		return errors.New("AWS SQS Publish Topic is not set")
	}

	return nil
}

// ==================================== Azure Service Bus ====================================

func (c *IngestConfig) parseAzureServiceBusConfig(viper *viper.Viper) {
	if !viper.IsSet("AZURE_SERVICE_ACCOUNT_CREDS") {
		return
	}

	config := &AzureServiceBusConfig{}

	c.AzureServiceBus = config
}

func (c *IngestConfig) validateAzureServiceBusConfig() error {
	if c.AzureServiceBus == nil {
		return nil
	}

	return nil
}

// ==================================== GCP PubSub ====================================

func (c *IngestConfig) parseGCPPubSubConfig(viper *viper.Viper) {
	if !viper.IsSet("GCP_PUBSUB_SERVICE_ACCOUNT_CREDS") {
		return
	}

	config := &GCPPubSubConfig{}

	c.GCPPubSub = config
}

func (c *IngestConfig) validateGCPPubSubConfig() error {
	if c.GCPPubSub == nil {
		return nil
	}

	return nil
}

// ==================================== RabbitMQ ====================================

const (
	DefaultRabbitMQPublishExchange = "eventkit"
	DefaultRabbitMQPublishQueue    = "eventkit.publish"
)

func (c *IngestConfig) parseRabbitMQConfig(viper *viper.Viper) {
	if !viper.IsSet("RABBITMQ_SERVER_URL") {
		return
	}

	config := &RabbitMQConfig{}
	config.ServerURL = viper.GetString("RABBITMQ_SERVER_URL")

	if viper.IsSet("RABBITMQ_PUBLISH_EXCHANGE") {
		config.PublishExchange = viper.GetString("RABBITMQ_PUBLISH_EXCHANGE")
	} else {
		config.PublishExchange = DefaultRabbitMQPublishExchange
	}

	if viper.IsSet("RABBITMQ_PUBLISH_QUEUE") {
		config.PublishQueue = viper.GetString("RABBITMQ_PUBLISH_QUEUE")
	} else {
		config.PublishQueue = DefaultRabbitMQPublishQueue
	}

	c.RabbitMQ = config
}

func (c *IngestConfig) validateRabbitMQConfig() error {
	if c.RabbitMQ == nil {
		return nil
	}

	if c.RabbitMQ.ServerURL == "" {
		return errors.New("RabbitMQ Server URL is not set")
	}

	if c.RabbitMQ.PublishExchange == "" {
		return errors.New("RabbitMQ Publish Exchange is not set")
	}

	if c.RabbitMQ.PublishQueue == "" {
		return errors.New("RabbitMQ Publish Queue is not set")
	}

	return nil
}
