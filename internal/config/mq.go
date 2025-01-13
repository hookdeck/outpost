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

type RabbitMQConfig struct {
	ServerURL     string `yaml:"server_url" env:"RABBITMQ_SERVER_URL"`
	Exchange      string `yaml:"exchange" env:"RABBITMQ_EXCHANGE"`
	DeliveryQueue string `yaml:"delivery_queue" env:"RABBITMQ_DELIVERY_QUEUE"`
	LogQueue      string `yaml:"log_queue" env:"RABBITMQ_LOG_QUEUE"`
}

type MQsConfig struct {
	AWSSQS             AWSSQSConfig   `yaml:"aws_sqs"`
	RabbitMQ           RabbitMQConfig `yaml:"rabbitmq"`
	DeliveryRetryLimit int            `yaml:"delivery_retry_limit" env:"DELIVERY_RETRY_LIMIT"`
	LogRetryLimit      int            `yaml:"log_retry_limit" env:"LOG_RETRY_LIMIT"`
}

func (c MQsConfig) GetInfraType() string {
	if hasAWSSQSConfig(c.AWSSQS) {
		return "awssqs"
	}
	if hasRabbitMQConfig(c.RabbitMQ) {
		return "rabbitmq"
	}
	return ""
}

func (c *MQsConfig) getQueueConfig(queueName string) *mqs.QueueConfig {
	if c == nil {
		return nil
	}

	infraType := c.GetInfraType()
	switch infraType {
	case "awssqs":
		creds := fmt.Sprintf("%s:%s:", c.AWSSQS.AccessKeyID, c.AWSSQS.SecretAccessKey)
		return &mqs.QueueConfig{
			AWSSQS: &mqs.AWSSQSConfig{
				Endpoint:                  c.AWSSQS.Endpoint,
				Region:                    c.AWSSQS.Region,
				ServiceAccountCredentials: creds,
				Topic:                     queueName,
			},
		}
	case "rabbitmq":
		return &mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: c.RabbitMQ.ServerURL,
				Exchange:  c.RabbitMQ.Exchange,
				Queue:     queueName,
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
		return c.getQueueConfig(c.AWSSQS.DeliveryQueue)
	case "rabbitmq":
		return c.getQueueConfig(c.RabbitMQ.DeliveryQueue)
	default:
		return nil
	}
}

func (c MQsConfig) GetLogQueueConfig() *mqs.QueueConfig {
	infraType := c.GetInfraType()
	switch infraType {
	case "awssqs":
		return c.getQueueConfig(c.AWSSQS.LogQueue)
	case "rabbitmq":
		return c.getQueueConfig(c.RabbitMQ.LogQueue)
	default:
		return nil
	}
}

// Helper functions to check for required fields
func hasAWSSQSConfig(config AWSSQSConfig) bool {
	return config.AccessKeyID != "" &&
		config.SecretAccessKey != "" && config.Region != ""
}

func hasRabbitMQConfig(config RabbitMQConfig) bool {
	return config.ServerURL != ""
}
