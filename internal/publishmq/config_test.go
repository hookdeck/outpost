package publishmq

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
		v.Set("PUBLISH_AWS_SQS_ACCESS_KEY_ID", "test-key")
		assert.Equal(t, "awssqs", detectInfraType(v))
	})

	t.Run("should detect RabbitMQ infrastructure", func(t *testing.T) {
		v := viper.New()
		v.Set("PUBLISH_RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		assert.Equal(t, "rabbitmq", detectInfraType(v))
	})

	t.Run("should not detect infrastructure when values are empty", func(t *testing.T) {
		v := viper.New()
		v.Set("PUBLISH_AWS_SQS_ACCESS_KEY_ID", "")
		v.Set("PUBLISH_RABBITMQ_SERVER_URL", "")
		assert.Equal(t, "", detectInfraType(v))
	})
}

func TestParseAWSSQSConfig(t *testing.T) {
	t.Run("should parse AWS SQS config", func(t *testing.T) {
		v := viper.New()
		v.Set("PUBLISH_AWS_SQS_ACCESS_KEY_ID", "test-key")
		v.Set("PUBLISH_AWS_SQS_SECRET_ACCESS_KEY", "test-secret")
		v.Set("PUBLISH_AWS_SQS_REGION", "eu-central-1")
		v.Set("PUBLISH_AWS_SQS_ENDPOINT", "http://localhost:4566")
		v.Set("PUBLISH_AWS_SQS_QUEUE", "publish-queue")

		parser := &awsSQSParser{viper: v}
		config, err := parser.parseQueue()
		require.NoError(t, err)
		require.NotNil(t, config)
		require.NotNil(t, config.AWSSQS)

		assert.Equal(t, "http://localhost:4566", config.AWSSQS.Endpoint)
		assert.Equal(t, "eu-central-1", config.AWSSQS.Region)
		assert.Equal(t, "test-key:test-secret:", config.AWSSQS.ServiceAccountCredentials)
		assert.Equal(t, "publish-queue", config.AWSSQS.Topic)
	})

	t.Run("should return error when config is incomplete", func(t *testing.T) {
		testCases := []struct {
			name        string
			setupViper  func(*viper.Viper)
			expectedErr string
		}{
			{
				name: "missing region",
				setupViper: func(v *viper.Viper) {
					v.Set("PUBLISH_AWS_SQS_ACCESS_KEY_ID", "test-key")
					v.Set("PUBLISH_AWS_SQS_SECRET_ACCESS_KEY", "test-secret")
					v.Set("PUBLISH_AWS_SQS_QUEUE", "publish-queue")
				},
				expectedErr: "PUBLISH_AWS_SQS_REGION is not set",
			},
			{
				name: "missing queue",
				setupViper: func(v *viper.Viper) {
					v.Set("PUBLISH_AWS_SQS_ACCESS_KEY_ID", "test-key")
					v.Set("PUBLISH_AWS_SQS_SECRET_ACCESS_KEY", "test-secret")
					v.Set("PUBLISH_AWS_SQS_REGION", "eu-central-1")
				},
				expectedErr: "PUBLISH_AWS_SQS_QUEUE is not set",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				v := viper.New()
				tc.setupViper(v)
				parser := &awsSQSParser{viper: v}
				config, err := parser.parseQueue()
				assert.Nil(t, config)
				assert.EqualError(t, err, tc.expectedErr)
			})
		}
	})
}

func TestParseRabbitMQConfig(t *testing.T) {
	t.Run("should parse RabbitMQ config", func(t *testing.T) {
		v := viper.New()
		v.Set("PUBLISH_RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
		v.Set("PUBLISH_RABBITMQ_EXCHANGE", "test-exchange")
		v.Set("PUBLISH_RABBITMQ_QUEUE", "publish-queue")

		parser := &rabbitMQParser{viper: v}
		config, err := parser.parseQueue()
		require.NoError(t, err)
		require.NotNil(t, config)
		require.NotNil(t, config.RabbitMQ)

		assert.Equal(t, "amqp://guest:guest@localhost:5672", config.RabbitMQ.ServerURL)
		assert.Equal(t, "test-exchange", config.RabbitMQ.Exchange)
		assert.Equal(t, "publish-queue", config.RabbitMQ.Queue)
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
					v.Set("PUBLISH_RABBITMQ_EXCHANGE", "test-exchange")
					v.Set("PUBLISH_RABBITMQ_QUEUE", "publish-queue")
				},
				expectedErr: "PUBLISH_RABBITMQ_SERVER_URL is not set",
			},
			{
				name: "missing queue",
				setupViper: func(v *viper.Viper) {
					v.Set("PUBLISH_RABBITMQ_SERVER_URL", "amqp://guest:guest@localhost:5672")
					v.Set("PUBLISH_RABBITMQ_EXCHANGE", "test-exchange")
				},
				expectedErr: "PUBLISH_RABBITMQ_QUEUE is not set",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				v := viper.New()
				tc.setupViper(v)
				parser := &rabbitMQParser{viper: v}
				config, err := parser.parseQueue()
				assert.Nil(t, config)
				assert.EqualError(t, err, tc.expectedErr)
			})
		}
	})
}
