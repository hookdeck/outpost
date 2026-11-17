package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishMQConfig_AWSSQSRoleFromEnv(t *testing.T) {
	cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: map[string]string{
		"PUBLISH_AWS_SQS_REGION":            "us-east-1",
		"PUBLISH_AWS_SQS_QUEUE":             "publish",
		"PUBLISH_AWS_SQS_ROLE_ARN":          "arn:aws:iam::123456789012:role/queue-reader",
		"PUBLISH_AWS_SQS_EXTERNAL_ID":       "ext-123",
		"PUBLISH_AWS_SQS_ROLE_SESSION_NAME": "reader",
	}})
	require.NoError(t, err)

	queue := cfg.PublishMQ.GetQueueConfig()
	require.NotNil(t, queue)
	require.NotNil(t, queue.AWSSQS)
	assert.Equal(t, "arn:aws:iam::123456789012:role/queue-reader", queue.AWSSQS.RoleARN)
	assert.Equal(t, "ext-123", queue.AWSSQS.ExternalID)
	assert.Equal(t, "reader", queue.AWSSQS.RoleSessionName)
}
