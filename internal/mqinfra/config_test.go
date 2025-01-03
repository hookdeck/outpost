package mqinfra

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfig(t *testing.T) {
	t.Run("should return error when no infra configured", func(t *testing.T) {
		v := viper.New()
		config, err := ParseConfig(v)
		assert.Nil(t, config)
		assert.EqualError(t, err, "no message queue infrastructure configured")
	})

	t.Run("should detect AWS SQS infrastructure", func(t *testing.T) {
		v := viper.New()
		v.Set("AWS_SQS_ACCESS_KEY_ID", "test-key")
		assert.Equal(t, "awssqs", detectInfraType(v))
	})

	t.Run("should detect RabbitMQ infrastructure", func(t *testing.T) {
		v := viper.New()
		v.Set("RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		assert.Equal(t, "rabbitmq", detectInfraType(v))
	})

	t.Run("should not detect infrastructure when values are empty", func(t *testing.T) {
		v := viper.New()
		v.Set("AWS_SQS_ACCESS_KEY_ID", "")
		v.Set("RABBITMQ_SERVER_URL", "")
		assert.Equal(t, "", detectInfraType(v))
	})
}

func TestParseConfigPolicy(t *testing.T) {
	setupInfraConfig := func(v *viper.Viper) {
		// Set AWS SQS config to make infrastructure detection work
		v.Set("AWS_SQS_ACCESS_KEY_ID", "test-key")
		v.Set("AWS_SQS_SECRET_ACCESS_KEY", "test-secret")
		v.Set("AWS_SQS_REGION", "eu-central-1")
		v.Set("AWS_SQS_DELIVERY_QUEUE", "delivery-queue")
		v.Set("AWS_SQS_LOG_QUEUE", "log-queue")
	}

	t.Run("should parse retry limits", func(t *testing.T) {
		v := viper.New()
		setupInfraConfig(v)

		v.Set("DELIVERY_RETRY_LIMIT", 5)
		v.Set("LOG_RETRY_LIMIT", 3)

		config, err := ParseConfig(v)
		require.NoError(t, err)
		require.NotNil(t, config)

		assert.Equal(t, 5, config.DeliveryMQ.Policy.RetryLimit)
		assert.Equal(t, 3, config.LogMQ.Policy.RetryLimit)
	})

	t.Run("should use default values when not set", func(t *testing.T) {
		v := viper.New()
		setupInfraConfig(v)

		config, err := ParseConfig(v)
		require.NoError(t, err)
		require.NotNil(t, config)

		assert.Equal(t, 0, config.DeliveryMQ.Policy.RetryLimit)
		assert.Equal(t, 0, config.LogMQ.Policy.RetryLimit)
	})
}
