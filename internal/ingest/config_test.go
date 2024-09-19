package ingest_test

import (
	"testing"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	t.Run("should validate without config", func(t *testing.T) {
		t.Parallel()
		config := ingest.IngestConfig{}
		err := config.Validate()
		assert.Nil(t, err, "IngestConfig should be valid without any config")
	})

	t.Run("should validate multiple configs", func(t *testing.T) {
		t.Parallel()
		config := ingest.IngestConfig{
			AWSSQS: &ingest.AWSSQSConfig{
				ServiceAccountCredentials: "test:test:",
				DeliveryTopic:             "topic",
				Region:                    "eu-central-1",
			},
			RabbitMQ: &ingest.RabbitMQConfig{
				ServerURL:        "url",
				DeliveryExchange: "exchange",
				DeliveryQueue:    "queue",
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
		config := ingest.IngestConfig{
			AWSSQS: &ingest.AWSSQSConfig{
				ServiceAccountCredentials: "",
				DeliveryTopic:             "topic",
			},
		}
		err := config.Validate()
		assert.ErrorContains(t, err, "AWS SQS Service Account Credentials is not set")
	})

	t.Run("should validate RabbitMQ config", func(t *testing.T) {
		t.Parallel()
		config := ingest.IngestConfig{
			RabbitMQ: &ingest.RabbitMQConfig{
				ServerURL:        "amqp://guest:guest@localhost:5672",
				DeliveryExchange: "",
				DeliveryQueue:    "queue",
			},
		}
		err := config.Validate()
		assert.ErrorContains(t, err, "RabbitMQ Delivery Exchange is not set")
	})
}

func TestConfig_Parse(t *testing.T) {
	t.Parallel()

	t.Run("should parse empty config without error", func(t *testing.T) {
		v := viper.New()
		config, err := ingest.ParseIngestConfig(v)
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
		config, err := ingest.ParseIngestConfig(v)
		require.Nil(t, err, "should parse without error")
		assert.Equal(t, config.AWSSQS.ServiceAccountCredentials, "test:test:")
		assert.Equal(t, config.AWSSQS.DeliveryTopic, "delivery")
		assert.Equal(t, config.AWSSQS.Region, "eu-central-1")
	})

	t.Run("should validate required config.topic", func(t *testing.T) {
		v := viper.New()
		v.Set("DELIVERY_AWS_SQS_SERVICE_ACCOUNT_CREDS", "test:test:")
		v.Set("DELIVERY_AWS_SQS_REGION", "eu-central-1")
		config, err := ingest.ParseIngestConfig(v)
		assert.Nil(t, config, "should return nil config")
		assert.ErrorContains(t, err, "AWS SQS Delivery Topic is not set")
	})

	t.Run("should validate credentails", func(t *testing.T) {
		v := viper.New()
		v.Set("DELIVERY_AWS_SQS_SERVICE_ACCOUNT_CREDS", "invalid")
		v.Set("DELIVERY_AWS_SQS_REGION", "eu-central-1")
		v.Set("DELIVERY_AWS_SQS_TOPIC", "delivery")
		config, err := ingest.ParseIngestConfig(v)
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
		config, err := ingest.ParseIngestConfig(v)
		require.Nil(t, err, "should parse without error")
		assert.Equal(t, config.RabbitMQ.ServerURL, "amqp://guest:guest@localhost:5672")
		assert.Equal(t, config.RabbitMQ.DeliveryExchange, "exchange")
		assert.Equal(t, config.RabbitMQ.DeliveryQueue, "queue")
	})

	t.Run("should use default value", func(t *testing.T) {
		v := viper.New()
		v.Set("DELIVERY_RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		config, err := ingest.ParseIngestConfig(v)
		require.Nil(t, err, "should not return error")
		assert.Equal(t, config.RabbitMQ.ServerURL, "amqp://guest:guest@localhost:5672")
		assert.Equal(t, config.RabbitMQ.DeliveryExchange, ingest.DefaultRabbitMQDeliveryExchange)
		assert.Equal(t, config.RabbitMQ.DeliveryQueue, ingest.DefaultRabbitMQDeliveryQueue)
	})
}
