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

type AzureServiceBusConfig struct {
}

type GCPPubSubConfig struct {
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
