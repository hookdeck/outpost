package mqs_test

import (
	"testing"

	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	t.Run("should validate without config", func(t *testing.T) {
		t.Parallel()
		config := mqs.QueueConfig{}
		err := config.Validate()
		assert.Nil(t, err, "QueueConfig should be valid without any config")
	})

	t.Run("should validate multiple configs", func(t *testing.T) {
		t.Parallel()
		config := mqs.QueueConfig{
			AWSSQS: &mqs.AWSSQSConfig{
				ServiceAccountCredentials: "test:test:",
				Topic:                     "topic",
				Region:                    "eu-central-1",
			},
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: "url",
				Exchange:  "exchange",
				Queue:     "queue",
			},
		}
		err := config.Validate()
		assert.ErrorContains(t, err,
			"only one of AWS SQS, GCP PubSub, Azure Service Bus, or RabbitMQ should be configured",
			"multiple config is not allowed",
		)
	})

	t.Run("should validate AWS SQS config", func(t *testing.T) {
		t.Parallel()
		config := mqs.QueueConfig{
			AWSSQS: &mqs.AWSSQSConfig{
				ServiceAccountCredentials: "",
				Topic:                     "topic",
			},
		}
		err := config.Validate()
		assert.ErrorContains(t, err, "AWS SQS Service Account Credentials is not set")
	})

	t.Run("should validate RabbitMQ config", func(t *testing.T) {
		t.Parallel()
		config := mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: "amqp://guest:guest@localhost:5672",
				Exchange:  "",
				Queue:     "queue",
			},
		}
		err := config.Validate()
		assert.ErrorContains(t, err, "RabbitMQ Exchange is not set")
	})
}

func TestConfig_Parse(t *testing.T) {
	t.Parallel()

	t.Run("should parse empty config without error", func(t *testing.T) {
		v := viper.New()
		config, err := mqs.ParseQueueConfig(v, "DELIVERY")
		assert.Nil(t, err, "should not return error")
		assert.NotNil(t, config, "should return config")
		assert.Nil(t, config.AWSSQS)
		assert.Nil(t, config.AzureServiceBus)
		assert.Nil(t, config.GCPPubSub)
		assert.Nil(t, config.RabbitMQ)
	})
}

func TestConfig_Parse_AWSSQS(t *testing.T) {
	t.Parallel()

	t.Run("should parse", func(t *testing.T) {
		v := viper.New()
		v.Set("DELIVERY_AWS_SQS_SERVICE_ACCOUNT_CREDS", "test:test:")
		v.Set("DELIVERY_AWS_SQS_REGION", "eu-central-1")
		v.Set("DELIVERY_AWS_SQS_TOPIC", "delivery")
		config, err := mqs.ParseQueueConfig(v, "DELIVERY")
		require.Nil(t, err, "should parse without error")
		assert.Equal(t, config.AWSSQS.ServiceAccountCredentials, "test:test:")
		assert.Equal(t, config.AWSSQS.Topic, "delivery")
		assert.Equal(t, config.AWSSQS.Region, "eu-central-1")
	})

	t.Run("should validate required config.topic", func(t *testing.T) {
		v := viper.New()
		v.Set("DELIVERY_AWS_SQS_SERVICE_ACCOUNT_CREDS", "test:test:")
		v.Set("DELIVERY_AWS_SQS_REGION", "eu-central-1")
		config, err := mqs.ParseQueueConfig(v, "DELIVERY")
		assert.Nil(t, config, "should return nil config")
		assert.ErrorContains(t, err, "AWS SQS Topic is not set")
	})

	t.Run("should validate credentails", func(t *testing.T) {
		v := viper.New()
		v.Set("DELIVERY_AWS_SQS_SERVICE_ACCOUNT_CREDS", "invalid")
		v.Set("DELIVERY_AWS_SQS_REGION", "eu-central-1")
		v.Set("DELIVERY_AWS_SQS_TOPIC", "delivery")
		config, err := mqs.ParseQueueConfig(v, "DELIVERY")
		assert.Nil(t, config, "should return nil config")
		assert.ErrorContains(t, err, "Invalid AWS Service Account Credentials")
	})
}

func TestConfig_Parse_RabbitMQ(t *testing.T) {
	t.Parallel()

	t.Run("should parse", func(t *testing.T) {
		v := viper.New()
		v.Set("DELIVERY_RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		v.Set("DELIVERY_RABBITMQ_EXCHANGE", "exchange")
		v.Set("DELIVERY_RABBITMQ_QUEUE", "queue")
		config, err := mqs.ParseQueueConfig(v, "DELIVERY")
		require.Nil(t, err, "should parse without error")
		assert.Equal(t, config.RabbitMQ.ServerURL, "amqp://guest:guest@localhost:5672")
		assert.Equal(t, config.RabbitMQ.Exchange, "exchange")
		assert.Equal(t, config.RabbitMQ.Queue, "queue")
	})
}
