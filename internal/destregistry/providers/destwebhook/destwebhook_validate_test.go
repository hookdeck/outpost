package destwebhook_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "test-secret",
		}),
	)

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, webhookDestination.Validate(nil, &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := webhookDestination.Validate(nil, &invalidDestination)
		assert.Error(t, err)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{}
		err := webhookDestination.Validate(nil, &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.url", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"url": "not-a-valid-url",
		}
		err := webhookDestination.Validate(nil, &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.url", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})
}

func TestWebhookDestination_ValidateSecrets(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "secret1",
		}),
	)

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, webhookDestination.Validate(nil, &validDestination))
	})

	t.Run("should validate missing secret", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := webhookDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.secret", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate previous secret without invalid_at", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"secret":          "secret1",
			"previous_secret": "secret2",
		}
		err := webhookDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.previous_secret_invalid_at", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed previous_secret_invalid_at", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"secret":                     "secret1",
			"previous_secret":            "secret2",
			"previous_secret_invalid_at": "not-a-timestamp",
		}
		err := webhookDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.previous_secret_invalid_at", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate valid destination with previous secret", func(t *testing.T) {
		t.Parallel()
		validDestWithPrevious := validDestination
		validDestWithPrevious.Credentials = map[string]string{
			"secret":                     "secret1",
			"previous_secret":            "secret2",
			"previous_secret_invalid_at": "2024-01-02T00:00:00Z",
		}
		assert.NoError(t, webhookDestination.Validate(nil, &validDestWithPrevious))
	})
}

func TestWebhookDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should return url as target", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/webhook",
			}),
		)
		target := webhookDestination.ComputeTarget(&destination)
		assert.Equal(t, "https://example.com/webhook", target)
	})
}
