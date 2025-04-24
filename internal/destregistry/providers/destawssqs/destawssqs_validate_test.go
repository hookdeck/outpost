package destawssqs_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSSQSDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_sqs"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
			"endpoint":  "https://sqs.us-east-1.amazonaws.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test-key",
			"secret":  "test-secret",
			"session": "test-session",
		}),
	)

	awsSQSDestination, err := destawssqs.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, awsSQSDestination.Validate(context.Background(), &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := awsSQSDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing queue_url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"endpoint": "https://sqs.us-east-1.amazonaws.com",
		}
		err := awsSQSDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.queue_url", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed queue_url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"queue_url": "not-a-valid-url",
			"endpoint":  "https://sqs.us-east-1.amazonaws.com",
		}
		err := awsSQSDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.queue_url", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed endpoint", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
			"endpoint":  "not-a-valid-url",
		}
		err := awsSQSDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.endpoint", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing credentials", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := awsSQSDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		// Could be either key or secret that's reported first
		assert.Contains(t, []string{"credentials.key", "credentials.secret"}, validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})
}

func TestAWSSQSDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	awsSQSDestination, err := destawssqs.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should return queue_url as target", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_sqs"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
				"endpoint":  "https://sqs.us-east-1.amazonaws.com",
			}),
		)
		target := awsSQSDestination.ComputeTarget(&destination)
		assert.Equal(t, "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue", target.Target)
	})
}
