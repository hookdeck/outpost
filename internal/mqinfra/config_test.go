package mqinfra

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
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
