package config

import (
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
)

type PublishAWSSQSConfig struct {
	AccessKeyID     string `yaml:"access_key_id" env:"PUBLISH_AWS_SQS_ACCESS_KEY_ID"`
	SecretAccessKey string `yaml:"secret_access_key" env:"PUBLISH_AWS_SQS_SECRET_ACCESS_KEY"`
	Region          string `yaml:"region" env:"PUBLISH_AWS_SQS_REGION"`
	Endpoint        string `yaml:"endpoint" env:"PUBLISH_AWS_SQS_ENDPOINT"`
	Queue           string `yaml:"queue" env:"PUBLISH_AWS_SQS_QUEUE"`
}

type PublishRabbitMQConfig struct {
	ServerURL string `yaml:"server_url" env:"PUBLISH_RABBITMQ_SERVER_URL"`
	Exchange  string `yaml:"exchange" env:"PUBLISH_RABBITMQ_EXCHANGE"`
	Queue     string `yaml:"queue" env:"PUBLISH_RABBITMQ_QUEUE"`
}

type PublishMQConfig struct {
	AWSSQS   PublishAWSSQSConfig   `yaml:"aws_sqs"`
	RabbitMQ PublishRabbitMQConfig `yaml:"rabbitmq"`
}

func (c PublishMQConfig) GetInfraType() string {
	if hasPublishAWSSQSConfig(c.AWSSQS) {
		return "awssqs"
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
	default:
		return nil
	}
}

func hasPublishAWSSQSConfig(config PublishAWSSQSConfig) bool {
	return config.AccessKeyID != "" &&
		config.SecretAccessKey != "" && config.Region != ""
}

func hasPublishRabbitMQConfig(config PublishRabbitMQConfig) bool {
	return config.ServerURL != ""
}
