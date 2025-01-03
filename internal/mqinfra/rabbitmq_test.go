package mqinfra

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQParser(t *testing.T) {
	t.Run("should parse queue config", func(t *testing.T) {
		v := viper.New()
		v.Set("RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		v.Set("RABBITMQ_EXCHANGE", "test-exchange")
		v.Set("RABBITMQ_DELIVERY_QUEUE", "delivery-queue")

		parser := &rabbitMQParser{viper: v}
		config, err := parser.parseQueue("DELIVERY")
		require.NoError(t, err)
		require.NotNil(t, config)
		require.NotNil(t, config.RabbitMQ)

		assert.Equal(t, "amqp://guest:guest@localhost:5672", config.RabbitMQ.ServerURL)
		assert.Equal(t, "test-exchange", config.RabbitMQ.Exchange)
		assert.Equal(t, "delivery-queue", config.RabbitMQ.Queue)
	})

	t.Run("should return error when config is incomplete", func(t *testing.T) {
		testCases := []struct {
			name        string
			setupViper  func(*viper.Viper)
			expectedErr string
		}{
			{
				name: "missing server URL",
				setupViper: func(v *viper.Viper) {
					v.Set("RABBITMQ_EXCHANGE", "test-exchange")
					v.Set("RABBITMQ_DELIVERY_QUEUE", "delivery-queue")
				},
				expectedErr: "RABBITMQ_SERVER_URL is not set",
			},
			{
				name: "missing queue",
				setupViper: func(v *viper.Viper) {
					v.Set("RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
					v.Set("RABBITMQ_EXCHANGE", "test-exchange")
				},
				expectedErr: "RABBITMQ_DELIVERY_QUEUE is not set",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				v := viper.New()
				tc.setupViper(v)
				parser := &rabbitMQParser{viper: v}
				config, err := parser.parseQueue("DELIVERY")
				assert.Nil(t, config)
				assert.EqualError(t, err, tc.expectedErr)
			})
		}
	})
}
