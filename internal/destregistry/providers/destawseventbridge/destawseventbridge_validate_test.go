package destawseventbridge_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawseventbridge"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSEventBridgeDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_eventbridge"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"event_bus_name": "my-bus",
			"region":         "us-east-1",
			"endpoint":       "https://events.us-east-1.amazonaws.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test-key",
			"secret":  "test-secret",
			"session": "test-session",
		}),
	)

	provider, err := destawseventbridge.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, provider.Validate(context.Background(), &validDestination))
	})

	t.Run("should validate valid destination with no event_bus_name (default bus)", func(t *testing.T) {
		t.Parallel()
		destination := validDestination
		destination.Config = map[string]string{
			"region": "us-east-1",
		}
		assert.NoError(t, provider.Validate(context.Background(), &destination))
	})

	t.Run("should validate valid destination with no credentials", func(t *testing.T) {
		t.Parallel()
		destination := validDestination
		destination.Credentials = map[string]string{}
		assert.NoError(t, provider.Validate(context.Background(), &destination),
			"credentials are optional so a destination with none configured must still validate")
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := provider.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing region", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"event_bus_name": "my-bus",
		}
		err := provider.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed region", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"event_bus_name": "my-bus",
			"region":         "not-a-region",
		}
		err := provider.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed endpoint", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"event_bus_name": "my-bus",
			"region":         "us-east-1",
			"endpoint":       "not-a-valid-url",
		}
		err := provider.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.endpoint", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})
}

func TestAWSEventBridgeDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	provider, err := destawseventbridge.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should return event_bus_name and region as target, with a console URL for a plain name", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_eventbridge"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"event_bus_name": "my-bus",
				"region":         "us-east-1",
			}),
		)
		target := provider.ComputeTarget(&destination)
		assert.Equal(t, "my-bus in us-east-1", target.Target)
		assert.Contains(t, target.TargetURL, "us-east-1.console.aws.amazon.com")
		assert.Contains(t, target.TargetURL, "my-bus")
	})

	t.Run("should not build a console URL for an ARN event bus", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_eventbridge"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"event_bus_name": "arn:aws:events:us-east-1:123456789012:event-bus/my-bus",
				"region":         "us-east-1",
			}),
		)
		target := provider.ComputeTarget(&destination)
		assert.Empty(t, target.TargetURL)
	})
}
