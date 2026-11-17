package destregistrydefault_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func customEndpointDestination(destType string, config map[string]string) *models.Destination {
	creds := map[string]string{"key": "k", "secret": "s"}
	if destType == "gcp_pubsub" {
		creds = map[string]string{"service_account_json": `{"type":"service_account"}`}
	}
	d := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType(destType),
		testutil.DestinationFactory.WithConfig(config),
		testutil.DestinationFactory.WithCredentials(creds),
	)
	return &d
}

func newCustomEndpointRegistry(t *testing.T, allow bool) destregistry.Registry {
	t.Helper()
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
		AWSEventBridge:       &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
		AllowCustomEndpoints: allow,
	}))
	return registry
}

var customEndpointCases = []struct {
	destType string
	config   map[string]string
	endpoint string
}{
	{"aws_eventbridge", map[string]string{"region": "us-east-1"}, "http://localhost:4566"},
	{"aws_kinesis", map[string]string{"stream_name": "s", "region": "us-east-1"}, "http://localhost:4566"},
	{"aws_s3", map[string]string{"bucket": "b", "region": "us-east-1"}, "http://localhost:4566"},
	{"aws_sqs", map[string]string{"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/q"}, "http://localhost:4566"},
	{"gcp_pubsub", map[string]string{"project_id": "p", "topic": "t"}, "localhost:8085"},
}

func withEndpoint(config map[string]string, endpoint string) map[string]string {
	out := map[string]string{"endpoint": endpoint}
	for k, v := range config {
		out[k] = v
	}
	return out
}

func TestRegistry_CustomEndpointOff_RejectsOnValidate(t *testing.T) {
	registry := newCustomEndpointRegistry(t, false)
	for _, tc := range customEndpointCases {
		t.Run(tc.destType, func(t *testing.T) {
			err := registry.ValidateDestination(context.Background(), customEndpointDestination(tc.destType, withEndpoint(tc.config, tc.endpoint)))
			var validationErr *destregistry.ErrDestinationValidation
			require.ErrorAs(t, err, &validationErr)
			assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "config.endpoint", Type: "forbidden"}}, validationErr.Errors)

			assert.NoError(t, registry.ValidateDestination(context.Background(), customEndpointDestination(tc.destType, tc.config)))
		})
	}
}

func TestRegistry_CustomEndpointOn_AcceptsOnValidate(t *testing.T) {
	registry := newCustomEndpointRegistry(t, true)
	for _, tc := range customEndpointCases {
		t.Run(tc.destType, func(t *testing.T) {
			assert.NoError(t, registry.ValidateDestination(context.Background(), customEndpointDestination(tc.destType, withEndpoint(tc.config, tc.endpoint))))
		})
	}
}

func TestRegistry_CustomEndpointOff_StoredDestinationFailsAttempt(t *testing.T) {
	registry := newCustomEndpointRegistry(t, false)
	for _, tc := range customEndpointCases {
		t.Run(tc.destType, func(t *testing.T) {
			dest := customEndpointDestination(tc.destType, withEndpoint(tc.config, tc.endpoint))
			event := testutil.EventFactory.Any()
			attempt, err := registry.PublishEvent(context.Background(), dest, &event)
			require.Error(t, err)
			require.NotNil(t, attempt)
			assert.Equal(t, "failed", attempt.Status)
			assert.Equal(t, "ERR", attempt.Code)
			assert.Contains(t, attempt.ResponseData["error"], "validation failed")
			assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "config.endpoint", Type: "forbidden"}}, attempt.ResponseData["errors"])

			var attemptErr *destregistry.ErrDestinationPublishAttempt
			require.ErrorAs(t, err, &attemptErr)
			assert.Equal(t, "validation_failed", attemptErr.Data["error"])
			assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "config.endpoint", Type: "forbidden"}}, attemptErr.Data["errors"])
		})
	}
}

func TestRegistry_CustomEndpointOff_SQSQueueURL(t *testing.T) {
	sqsConfig := func(queueURL string) map[string]string { return map[string]string{"queue_url": queueURL} }
	localStack := "http://localhost:4566/000000000000/q"

	t.Run("off rejects a non-AWS queue_url on validate and fails the stored attempt", func(t *testing.T) {
		registry := newCustomEndpointRegistry(t, false)
		want := []destregistry.ValidationErrorDetail{{Field: "config.queue_url", Type: "forbidden"}}
		for _, queueURL := range []string{localStack, "https://sqs.us-east-1.amazonaws.com.evil.example/1/q", "https://queue.example.com/1/q"} {
			dest := customEndpointDestination("aws_sqs", sqsConfig(queueURL))
			err := registry.ValidateDestination(context.Background(), dest)
			var validationErr *destregistry.ErrDestinationValidation
			require.ErrorAs(t, err, &validationErr, queueURL)
			assert.Equal(t, want, validationErr.Errors, queueURL)

			event := testutil.EventFactory.Any()
			attempt, err := registry.PublishEvent(context.Background(), dest, &event)
			require.Error(t, err, queueURL)
			require.NotNil(t, attempt)
			assert.Equal(t, want, attempt.ResponseData["errors"], queueURL)
		}
	})

	t.Run("on accepts a non-AWS queue_url without endpoint", func(t *testing.T) {
		registry := newCustomEndpointRegistry(t, true)
		assert.NoError(t, registry.ValidateDestination(context.Background(), customEndpointDestination("aws_sqs", sqsConfig(localStack))))
	})
}

func TestRegistry_CustomEndpointOff_OmitsEndpointFromMetadata(t *testing.T) {
	hasEndpoint := func(registry destregistry.Registry, destType string) bool {
		meta, err := registry.RetrieveProviderMetadata(destType)
		require.NoError(t, err)
		for _, f := range meta.ConfigFields {
			if f.Key == "endpoint" {
				return true
			}
		}
		return false
	}
	off := newCustomEndpointRegistry(t, false)
	on := newCustomEndpointRegistry(t, true)
	for _, destType := range []string{"aws_eventbridge", "aws_kinesis", "gcp_pubsub"} {
		assert.False(t, hasEndpoint(off, destType), destType)
		assert.True(t, hasEndpoint(on, destType), destType)
	}
}
