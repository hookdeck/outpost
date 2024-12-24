package destwebhook_test

import (
	"encoding/hex"
	"testing"
	"time"

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

func TestWebhookDestination_Preprocess(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should generate default secret if not provided", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
		)

		err := webhookDestination.Preprocess(&destination, nil)
		require.NoError(t, err)

		// Verify that a secret was generated
		assert.NotEmpty(t, destination.Credentials["secret"])
		// Verify that the secret is a valid hex string of length 64 (32 bytes)
		assert.Len(t, destination.Credentials["secret"], 64)
		_, err = hex.DecodeString(destination.Credentials["secret"])
		assert.NoError(t, err, "generated secret should be a valid hex string")
	})

	t.Run("should not override existing secret", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "existing-secret",
			}),
		)

		err := webhookDestination.Preprocess(&destination, nil)
		require.NoError(t, err)

		// Verify that the existing secret was not changed
		assert.Equal(t, "existing-secret", destination.Credentials["secret"])
	})

	t.Run("should initialize credentials map if nil", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
		)
		destination.Credentials = nil

		err := webhookDestination.Preprocess(&destination, nil)
		require.NoError(t, err)

		// Verify that credentials map was initialized and a secret was generated
		assert.NotNil(t, destination.Credentials)
		assert.NotEmpty(t, destination.Credentials["secret"])
	})

	t.Run("should rotate secret when requested", func(t *testing.T) {
		t.Parallel()
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "current-secret",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"rotate_secret": "true",
			}),
		)

		err := webhookDestination.Preprocess(&newDestination, &originalDestination)
		require.NoError(t, err)

		// Verify that the current secret became the previous secret
		assert.Equal(t, "current-secret", newDestination.Credentials["previous_secret"])

		// Verify that a new secret was generated
		assert.NotEqual(t, "current-secret", newDestination.Credentials["secret"])
		assert.NotEmpty(t, newDestination.Credentials["secret"])
		assert.Len(t, newDestination.Credentials["secret"], 64)
		_, err = hex.DecodeString(newDestination.Credentials["secret"])
		assert.NoError(t, err, "generated secret should be a valid hex string")

		// Verify that previous_secret_invalid_at was set to ~24h from now
		invalidAt, err := time.Parse(time.RFC3339, newDestination.Credentials["previous_secret_invalid_at"])
		require.NoError(t, err)
		expectedTime := time.Now().Add(24 * time.Hour)
		assert.WithinDuration(t, expectedTime, invalidAt, 5*time.Second)
	})

	t.Run("should respect custom invalidation time during rotation", func(t *testing.T) {
		t.Parallel()
		customInvalidAt := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "current-secret",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"rotate_secret":              "true",
				"previous_secret_invalid_at": customInvalidAt,
			}),
		)

		err := webhookDestination.Preprocess(&newDestination, &originalDestination)
		require.NoError(t, err)

		// Verify that the custom invalidation time was preserved
		assert.Equal(t, customInvalidAt, newDestination.Credentials["previous_secret_invalid_at"])
	})

	t.Run("should fail to rotate secret during initial creation", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"rotate_secret": "true",
			}),
		)

		err := webhookDestination.Preprocess(&destination, nil)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.rotate_secret", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid", validationErr.Errors[0].Type)
	})

	t.Run("should fail to rotate secret when original has no secret", func(t *testing.T) {
		t.Parallel()
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"rotate_secret": "true",
			}),
		)

		err := webhookDestination.Preprocess(&newDestination, &originalDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.secret", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should set default previous_secret_invalid_at when previous_secret is provided", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret":          "current-secret",
				"previous_secret": "old-secret",
			}),
		)

		err := webhookDestination.Preprocess(&destination, nil)
		require.NoError(t, err)

		// Verify that previous_secret_invalid_at was set to ~24h from now
		invalidAt, err := time.Parse(time.RFC3339, destination.Credentials["previous_secret_invalid_at"])
		require.NoError(t, err)
		expectedTime := time.Now().Add(24 * time.Hour)
		assert.WithinDuration(t, expectedTime, invalidAt, 5*time.Second)
	})

	t.Run("should rotate secret when requested with various truthy values", func(t *testing.T) {
		t.Parallel()
		truthyValues := []string{"true", "1", "on", "yes", "TRUE", "ON", "Yes"}

		for _, value := range truthyValues {
			t.Run(value, func(t *testing.T) {
				t.Parallel()
				originalDestination := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("webhook"),
					testutil.DestinationFactory.WithConfig(map[string]string{
						"url": "https://example.com",
					}),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"secret": "current-secret",
					}),
				)

				newDestination := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("webhook"),
					testutil.DestinationFactory.WithConfig(map[string]string{
						"url": "https://example.com",
					}),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"rotate_secret": value,
					}),
				)

				err := webhookDestination.Preprocess(&newDestination, &originalDestination)
				require.NoError(t, err)

				// Verify that the current secret became the previous secret
				assert.Equal(t, "current-secret", newDestination.Credentials["previous_secret"])

				// Verify that a new secret was generated
				assert.NotEqual(t, "current-secret", newDestination.Credentials["secret"])
				assert.NotEmpty(t, newDestination.Credentials["secret"])
				assert.Len(t, newDestination.Credentials["secret"], 64)
				_, err = hex.DecodeString(newDestination.Credentials["secret"])
				assert.NoError(t, err, "generated secret should be a valid hex string")

				// Verify that previous_secret_invalid_at was set to ~24h from now
				invalidAt, err := time.Parse(time.RFC3339, newDestination.Credentials["previous_secret_invalid_at"])
				require.NoError(t, err)
				expectedTime := time.Now().Add(24 * time.Hour)
				assert.WithinDuration(t, expectedTime, invalidAt, 5*time.Second)
			})
		}
	})

	t.Run("should not rotate secret with falsy values", func(t *testing.T) {
		t.Parallel()
		falsyValues := []string{"false", "0", "off", "no", "FALSE", "OFF", "No", ""}

		for _, value := range falsyValues {
			t.Run(value, func(t *testing.T) {
				t.Parallel()
				originalDestination := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("webhook"),
					testutil.DestinationFactory.WithConfig(map[string]string{
						"url": "https://example.com",
					}),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"secret": "current-secret",
					}),
				)

				newDestination := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("webhook"),
					testutil.DestinationFactory.WithConfig(map[string]string{
						"url": "https://example.com",
					}),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"secret":        "current-secret",
						"rotate_secret": value,
					}),
				)

				err := webhookDestination.Preprocess(&newDestination, &originalDestination)
				require.NoError(t, err)

				// Verify that the secret was not changed
				assert.Equal(t, "current-secret", newDestination.Credentials["secret"])
				// Verify that no previous_secret was set
				assert.Empty(t, newDestination.Credentials["previous_secret"])
			})
		}
	})

	t.Run("should remove extra fields from credentials map", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret":                     "current-secret",
				"previous_secret":            "old-secret",
				"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				"extra_field":                "should be removed",
				"another_extra":              "also removed",
				"rotate_secret":              "false",
			}),
		)

		err := webhookDestination.Preprocess(&destination, nil)
		require.NoError(t, err)

		// Verify that only expected fields are present
		expectedFields := map[string]bool{
			"secret":                     true,
			"previous_secret":            true,
			"previous_secret_invalid_at": true,
		}

		// Check that only expected fields exist
		for key := range destination.Credentials {
			assert.True(t, expectedFields[key], "unexpected field %q found in credentials", key)
		}

		// Check that all expected fields are present
		assert.Equal(t, len(expectedFields), len(destination.Credentials), "credentials map has wrong number of fields")

		// Verify values are preserved for expected fields
		assert.Equal(t, "current-secret", destination.Credentials["secret"])
		assert.Equal(t, "old-secret", destination.Credentials["previous_secret"])
		assert.NotEmpty(t, destination.Credentials["previous_secret_invalid_at"])
	})
}
