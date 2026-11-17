package destregistry_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A template that parses but is not one of the accepted forms is rejected on
// create and update, and a stored destination that has one fails the attempt
// without evaluating it.
func TestRegistry_TemplateOutsideAcceptedForms(t *testing.T) {
	t.Parallel()

	notAccepted := "length(data)"

	s3, err := destawss3.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
	kafka, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
	kinesis, err := destawskinesis.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		provider destregistry.Provider
		field    string
		config   map[string]string
		creds    map[string]string
	}{
		{
			name:     "aws_s3",
			provider: s3,
			field:    "config.key_template",
			config:   map[string]string{"bucket": "my-bucket", "region": "us-east-1", "key_template": notAccepted},
			creds:    map[string]string{"key": "k", "secret": "s"},
		},
		{
			name:     "kafka",
			provider: kafka,
			field:    "config.partition_key_template",
			config:   map[string]string{"brokers": "localhost:9092", "topic": "t", "sasl_mechanism": "plain", "partition_key_template": notAccepted},
			creds:    map[string]string{"username": "u", "password": "p"},
		},
		{
			name:     "aws_kinesis",
			provider: kinesis,
			field:    "config.partition_key_template",
			config:   map[string]string{"stream_name": "my-stream", "region": "us-east-1", "partition_key_template": notAccepted},
			creds:    map[string]string{"key": "k", "secret": "s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType(tt.name),
				testutil.DestinationFactory.WithConfig(tt.config),
				testutil.DestinationFactory.WithCredentials(tt.creds),
			)

			var validationErr *destregistry.ErrDestinationValidation
			err := tt.provider.Validate(context.Background(), &destination)
			require.ErrorAs(t, err, &validationErr)
			require.Len(t, validationErr.Errors, 1)
			assert.Equal(t, tt.field, validationErr.Errors[0].Field)
			assert.Equal(t, "pattern", validationErr.Errors[0].Type)

			registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
			require.NoError(t, registry.RegisterProvider(tt.name, tt.provider))

			event := testutil.EventFactory.Any(testutil.EventFactory.WithData([]byte(`{"a":"b"}`)))
			attempt, err := registry.PublishEvent(context.Background(), &destination, &event)

			require.NotNil(t, attempt, "a failed attempt is recorded")
			assert.Equal(t, "failed", attempt.Status)
			assert.Equal(t, "ERR", attempt.Code)
			var pubErr *destregistry.ErrDestinationPublishAttempt
			require.ErrorAs(t, err, &pubErr)
			assert.Equal(t, "validation_failed", pubErr.Data["error"])
		})
	}
}
