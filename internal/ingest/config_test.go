package ingest_test

import (
	"testing"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
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
				ServiceAccountCredentials: "credentials",
				PublishTopic:              "topic",
			},
			RabbitMQ: &ingest.RabbitMQConfig{
				ServerURL:       "url",
				PublishExchange: "exchange",
				PublishQueue:    "queue",
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
				PublishTopic:              "topic",
			},
		}
		err := config.Validate()
		assert.ErrorContains(t, err, "AWS SQS Service Account Credentials is not set")
	})

	t.Run("should validate RabbitMQ config", func(t *testing.T) {
		t.Parallel()
		config := ingest.IngestConfig{
			RabbitMQ: &ingest.RabbitMQConfig{
				ServerURL:       "amqp://guest:guest@localhost:5672",
				PublishExchange: "",
				PublishQueue:    "queue",
			},
		}
		err := config.Validate()
		assert.ErrorContains(t, err, "RabbitMQ Publish Exchange is not set")
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
		v.Set("AWS_SQS_SERVICE_ACCOUNT_CREDS", "credentials")
		v.Set("AWS_SQS_PUBLISH_TOPIC", "publish")
		config, err := ingest.ParseIngestConfig(v)
		if err != nil {
			t.Fatal(err)
		}
		if config == nil {
			t.Fatal("config is nil")
		}
		assert.Equal(t, config.AWSSQS.ServiceAccountCredentials, "credentials")
		assert.Equal(t, config.AWSSQS.PublishTopic, "publish")
	})

	t.Run("should validate", func(t *testing.T) {
		v := viper.New()
		v.Set("AWS_SQS_SERVICE_ACCOUNT_CREDS", "credentials")
		config, err := ingest.ParseIngestConfig(v)
		assert.Nil(t, config, "should return nil config")
		assert.ErrorContains(t, err, "AWS SQS Publish Topic is not set")
	})
}

func TestConfig_Parse_RabbitMQ(t *testing.T) {
	t.Parallel()

	t.Run("should parse", func(t *testing.T) {
		v := viper.New()
		v.Set("RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		v.Set("RABBITMQ_PUBLISH_EXCHANGE", "exchange")
		v.Set("RABBITMQ_PUBLISH_QUEUE", "queue")
		config, err := ingest.ParseIngestConfig(v)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, config.RabbitMQ.ServerURL, "amqp://guest:guest@localhost:5672")
		assert.Equal(t, config.RabbitMQ.PublishExchange, "exchange")
		assert.Equal(t, config.RabbitMQ.PublishQueue, "queue")
	})

	t.Run("should use default value", func(t *testing.T) {
		v := viper.New()
		v.Set("RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		config, err := ingest.ParseIngestConfig(v)
		assert.Nil(t, err, "should not return error")
		assert.Equal(t, config.RabbitMQ.ServerURL, "amqp://guest:guest@localhost:5672")
		assert.Equal(t, config.RabbitMQ.PublishExchange, ingest.DefaultRabbitMQPublishExchange)
		assert.Equal(t, config.RabbitMQ.PublishQueue, ingest.DefaultRabbitMQPublishQueue)
	})
}
