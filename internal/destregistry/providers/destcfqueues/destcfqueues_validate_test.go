package destcfqueues_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destcfqueues"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudflareQueuesDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("cloudflare_queues"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"account_id": testAccountID,
			"queue_id":   testQueueID,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"api_token": "test-api-token",
		}),
	)

	cloudflareQueuesDestination, err := destcfqueues.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, cloudflareQueuesDestination.Validate(context.Background(), &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := cloudflareQueuesDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing account_id", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"queue_id": testQueueID,
		}
		err := cloudflareQueuesDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.account_id", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing queue_id", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"account_id": testAccountID,
		}
		err := cloudflareQueuesDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.queue_id", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate account_id format", func(t *testing.T) {
		t.Parallel()
		for _, accountID := range []string{
			"my-account",
			"023e105f-4ece-f8ad-9ca3-1a8372d0c353",
			testAccountID + "0",
			testAccountID[:31],
			"023e105f4ecef8ad9ca31a8372d0c35g",
			strings.ToUpper(testAccountID),
		} {
			invalidDestination := validDestination
			invalidDestination.Config = map[string]string{
				"account_id": accountID,
				"queue_id":   testQueueID,
			}
			err := cloudflareQueuesDestination.Validate(context.Background(), &invalidDestination)
			var validationErr *destregistry.ErrDestinationValidation
			require.ErrorAs(t, err, &validationErr, accountID)
			assert.Equal(t, "config.account_id", validationErr.Errors[0].Field, accountID)
			assert.Equal(t, "pattern", validationErr.Errors[0].Type, accountID)
		}
	})

	t.Run("should validate queue_id format", func(t *testing.T) {
		t.Parallel()
		for _, queueID := range []string{
			"my-queue",
			"9d7d4cf8-a3a1-4d9a-aeb5-0c3e74e2f4b1",
			testQueueID + "0",
			testQueueID[:31],
			"x/../../../user/tokens/verify?",
			strings.ToUpper(testQueueID),
		} {
			invalidDestination := validDestination
			invalidDestination.Config = map[string]string{
				"account_id": testAccountID,
				"queue_id":   queueID,
			}
			err := cloudflareQueuesDestination.Validate(context.Background(), &invalidDestination)
			var validationErr *destregistry.ErrDestinationValidation
			require.ErrorAs(t, err, &validationErr, queueID)
			assert.Equal(t, "config.queue_id", validationErr.Errors[0].Field, queueID)
			assert.Equal(t, "pattern", validationErr.Errors[0].Type, queueID)
		}
	})

	t.Run("should validate missing api_token", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := cloudflareQueuesDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.api_token", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})
}

func TestCloudflareQueuesDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	cloudflareQueuesDestination, err := destcfqueues.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should return queue_id as target and dashboard URL", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("cloudflare_queues"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"account_id": "my-account-123",
				"queue_id":   "my-queue-456",
			}),
		)
		target := cloudflareQueuesDestination.ComputeTarget(&destination)
		assert.Equal(t, "my-queue-456", target.Target)
		assert.Equal(t, "https://dash.cloudflare.com/my-account-123/workers/queues", target.TargetURL)
	})

	t.Run("should return empty target URL when account_id is missing", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("cloudflare_queues"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"queue_id": "my-queue-456",
			}),
		)
		target := cloudflareQueuesDestination.ComputeTarget(&destination)
		assert.Equal(t, "my-queue-456", target.Target)
		assert.Equal(t, "", target.TargetURL)
	})

	t.Run("should return empty target URL when queue_id is missing", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("cloudflare_queues"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"account_id": "my-account-123",
			}),
		)
		target := cloudflareQueuesDestination.ComputeTarget(&destination)
		assert.Equal(t, "", target.Target)
		assert.Equal(t, "", target.TargetURL)
	})
}
