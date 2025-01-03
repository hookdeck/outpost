package mqinfra

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSSQSParser(t *testing.T) {
	t.Run("should parse queue config", func(t *testing.T) {
		v := viper.New()
		v.Set("AWS_SQS_ACCESS_KEY_ID", "test-key")
		v.Set("AWS_SQS_SECRET_ACCESS_KEY", "test-secret")
		v.Set("AWS_SQS_REGION", "eu-central-1")
		v.Set("AWS_SQS_ENDPOINT", "http://localhost:4566")
		v.Set("AWS_SQS_DELIVERY_QUEUE", "delivery-queue")

		parser := &awsSQSParser{viper: v}
		config, err := parser.parseQueue("DELIVERY")
		require.NoError(t, err)
		require.NotNil(t, config)
		require.NotNil(t, config.AWSSQS)

		assert.Equal(t, "http://localhost:4566", config.AWSSQS.Endpoint)
		assert.Equal(t, "eu-central-1", config.AWSSQS.Region)
		assert.Equal(t, "test-key:test-secret:", config.AWSSQS.ServiceAccountCredentials)
		assert.Equal(t, "delivery-queue", config.AWSSQS.Topic)
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
					v.Set("AWS_SQS_ACCESS_KEY_ID", "test-key")
					v.Set("AWS_SQS_SECRET_ACCESS_KEY", "test-secret")
					v.Set("AWS_SQS_DELIVERY_QUEUE", "delivery-queue")
				},
				expectedErr: "AWS_SQS_REGION is not set",
			},
			{
				name: "missing queue",
				setupViper: func(v *viper.Viper) {
					v.Set("AWS_SQS_ACCESS_KEY_ID", "test-key")
					v.Set("AWS_SQS_SECRET_ACCESS_KEY", "test-secret")
					v.Set("AWS_SQS_REGION", "eu-central-1")
				},
				expectedErr: "AWS_SQS_DELIVERY_QUEUE is not set",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				v := viper.New()
				tc.setupViper(v)
				parser := &awsSQSParser{viper: v}
				config, err := parser.parseQueue("DELIVERY")
				assert.Nil(t, config)
				assert.EqualError(t, err, tc.expectedErr)
			})
		}
	})
}
