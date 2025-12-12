package destwebhookstandard_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhookstandard"
	"github.com/hookdeck/outpost/internal/util/maputil"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardWebhookDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
		}),
	)

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, provider.Validate(context.Background(), &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := provider.Validate(context.Background(), &invalidDestination)
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
		err := provider.Validate(context.Background(), &invalidDestination)

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
		err := provider.Validate(context.Background(), &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.url", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate secret without whsec prefix", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"secret": "not-a-whsec-secret",
		}
		err := provider.Validate(context.Background(), &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.secret", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate secret with invalid base64", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"secret": "whsec_not-valid-base64!!!",
		}
		err := provider.Validate(context.Background(), &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.secret", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate previous_secret without whsec prefix", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"secret":                     "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			"previous_secret":            "not-a-whsec-secret",
			"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}
		err := provider.Validate(context.Background(), &invalidDestination)

		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.previous_secret", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate previous secret without invalid_at", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"secret":          "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			"previous_secret": "whsec_T2xkU2VjcmV0U3RyaW5nMTIz",
		}
		err := provider.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.previous_secret_invalid_at", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed previous_secret_invalid_at", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{
			"secret":                     "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			"previous_secret":            "whsec_T2xkU2VjcmV0U3RyaW5nMTIz",
			"previous_secret_invalid_at": "not-a-timestamp",
		}
		err := provider.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.previous_secret_invalid_at", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate valid destination with previous secret", func(t *testing.T) {
		t.Parallel()
		validDestWithPrevious := validDestination
		validDestWithPrevious.Credentials = map[string]string{
			"secret":                     "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			"previous_secret":            "whsec_T2xkU2VjcmV0U3RyaW5nMTIz",
			"previous_secret_invalid_at": "2024-01-02T00:00:00Z",
		}
		assert.NoError(t, provider.Validate(context.Background(), &validDestWithPrevious))
	})
}

func TestStandardWebhookDestination_ValidateCustomHeaders(t *testing.T) {
	t.Parallel()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should accept valid header names", func(t *testing.T) {
		t.Parallel()
		validHeaders := []string{
			"x-api-key",
			"X-Custom-Header",
			"Authorization",
			"x-tenant-id",
			"X_Custom_Header",
			"x123-header",
		}
		for _, header := range validHeaders {
			t.Run(header, func(t *testing.T) {
				t.Parallel()
				destination := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("webhook"),
					testutil.DestinationFactory.WithConfig(map[string]string{
						"url":            "https://example.com/webhook",
						"custom_headers": `{"` + header + `":"value"}`,
					}),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
					}),
				)

				err := provider.Validate(context.Background(), &destination)
				assert.NoError(t, err, "header name %q should be valid", header)
			})
		}
	})

	t.Run("should reject invalid header names", func(t *testing.T) {
		t.Parallel()
		invalidHeaders := []struct {
			name         string
			expectedType string
		}{
			{"header with space", "pattern"},
			{"header:colon", "pattern"},
			{"-starts-with-dash", "pattern"},
			{"_starts_with_underscore", "pattern"},
		}
		for _, tc := range invalidHeaders {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				destination := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("webhook"),
					testutil.DestinationFactory.WithConfig(map[string]string{
						"url":            "https://example.com/webhook",
						"custom_headers": `{"` + tc.name + `":"value"}`,
					}),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
					}),
				)

				err := provider.Validate(context.Background(), &destination)
				assert.Error(t, err, "header name %q should be invalid", tc.name)
				var validationErr *destregistry.ErrDestinationValidation
				assert.ErrorAs(t, err, &validationErr)
				assert.Equal(t, tc.expectedType, validationErr.Errors[0].Type)
			})
		}
	})

	t.Run("should reject reserved header names", func(t *testing.T) {
		t.Parallel()
		reservedHeaders := []string{
			"Content-Type",
			"content-type",
			"Content-Length",
			"Host",
			"Connection",
			"User-Agent",
		}
		for _, header := range reservedHeaders {
			t.Run(header, func(t *testing.T) {
				t.Parallel()
				destination := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("webhook"),
					testutil.DestinationFactory.WithConfig(map[string]string{
						"url":            "https://example.com/webhook",
						"custom_headers": `{"` + header + `":"value"}`,
					}),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
					}),
				)

				err := provider.Validate(context.Background(), &destination)
				assert.Error(t, err, "reserved header %q should be rejected", header)
				var validationErr *destregistry.ErrDestinationValidation
				assert.ErrorAs(t, err, &validationErr)
				assert.Equal(t, "forbidden", validationErr.Errors[0].Type)
			})
		}
	})

	t.Run("should reject empty header values", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{"x-api-key":""}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		err := provider.Validate(context.Background(), &destination)
		assert.Error(t, err)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.custom_headers.x-api-key", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should collect multiple validation errors", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{"Content-Type":"application/xml","x-valid":"ok","Host":"example.com"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		err := provider.Validate(context.Background(), &destination)
		assert.Error(t, err)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		// Should have errors for both Content-Type and Host (reserved headers)
		assert.GreaterOrEqual(t, len(validationErr.Errors), 2)
	})
}

func TestStandardWebhookDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should return url as target", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/webhook",
			}),
		)
		target := provider.ComputeTarget(&destination)
		assert.Equal(t, "https://example.com/webhook", target.Target)
	})
}

func TestStandardWebhookDestination_Preprocess(t *testing.T) {
	t.Parallel()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should generate default whsec secret if not provided", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
		)

		err := provider.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
		require.NoError(t, err)

		// Verify that a whsec_ secret was generated
		assert.True(t, strings.HasPrefix(destination.Credentials["secret"], "whsec_"))

		// Verify it's valid
		assert.NoError(t, provider.Validate(context.Background(), &destination))
	})

	t.Run("should preserve existing secret for admin", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_CustomSecretBase64EncodedString",
			}),
		)

		err := provider.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "admin"})
		require.NoError(t, err)

		// Verify that the custom secret was preserved
		assert.Equal(t, "whsec_CustomSecretBase64EncodedString", destination.Credentials["secret"])
	})

	t.Run("tenant should not be able to override existing secret", func(t *testing.T) {
		t.Parallel()
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_CurrentSecretBase64EncodedString",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/new",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_CustomSecretBase64EncodedString",
			}),
		)

		// Merge both config and credentials to simulate handler behavior
		newDestination.Config = maputil.MergeStringMaps(originalDestination.Config, newDestination.Config)
		newDestination.Credentials = maputil.MergeStringMaps(originalDestination.Credentials, newDestination.Credentials)

		err := provider.Preprocess(&newDestination, &originalDestination, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "credentials.secret", validationErr.Errors[0].Field)
		assert.Equal(t, "forbidden", validationErr.Errors[0].Type)
	})

	t.Run("tenant should be able to rotate secret", func(t *testing.T) {
		t.Parallel()
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_CurrentSecretBase64EncodedString",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/new",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"rotate_secret": "true",
			}),
		)

		// Merge both config and credentials to simulate handler behavior
		newDestination.Config = maputil.MergeStringMaps(originalDestination.Config, newDestination.Config)
		newDestination.Credentials = maputil.MergeStringMaps(originalDestination.Credentials, newDestination.Credentials)

		err := provider.Preprocess(&newDestination, &originalDestination, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
		require.NoError(t, err)

		// Verify that the current secret became the previous secret
		assert.Equal(t, "whsec_CurrentSecretBase64EncodedString", newDestination.Credentials["previous_secret"])

		// Verify that a new secret was generated with whsec_ prefix
		assert.NotEqual(t, "whsec_CurrentSecretBase64EncodedString", newDestination.Credentials["secret"])
		assert.True(t, strings.HasPrefix(newDestination.Credentials["secret"], "whsec_"))
		assert.NotEmpty(t, newDestination.Credentials["secret"])

		// Verify that previous_secret_invalid_at was set to ~24h from now
		invalidAt, err := time.Parse(time.RFC3339, newDestination.Credentials["previous_secret_invalid_at"])
		require.NoError(t, err)
		expectedTime := time.Now().Add(24 * time.Hour)
		assert.WithinDuration(t, expectedTime, invalidAt, 5*time.Second)
	})

	t.Run("admin should be able to set previous_secret directly", func(t *testing.T) {
		t.Parallel()
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_Q3VycmVudFNlY3JldFN0cmluZw==",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/new",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"previous_secret": "whsec_T2xkU2VjcmV0U3RyaW5nMTIz",
			}),
		)

		// Merge both config and credentials to simulate handler behavior
		newDestination.Config = maputil.MergeStringMaps(originalDestination.Config, newDestination.Config)
		newDestination.Credentials = maputil.MergeStringMaps(originalDestination.Credentials, newDestination.Credentials)

		err := provider.Preprocess(&newDestination, &originalDestination, &destregistry.PreprocessDestinationOpts{Role: "admin"})
		require.NoError(t, err)

		// Verify that previous_secret was kept
		assert.Equal(t, "whsec_T2xkU2VjcmV0U3RyaW5nMTIz", newDestination.Credentials["previous_secret"])
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
				"secret": "whsec_CurrentSecretBase64EncodedString",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/new",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"rotate_secret":              "true",
				"previous_secret_invalid_at": customInvalidAt,
			}),
		)

		// Merge both config and credentials to simulate handler behavior
		newDestination.Config = maputil.MergeStringMaps(originalDestination.Config, newDestination.Config)
		newDestination.Credentials = maputil.MergeStringMaps(originalDestination.Credentials, newDestination.Credentials)

		err := provider.Preprocess(&newDestination, &originalDestination, &destregistry.PreprocessDestinationOpts{})
		require.NoError(t, err)

		// Verify that the custom invalidation time was preserved
		assert.Equal(t, customInvalidAt, newDestination.Credentials["previous_secret_invalid_at"])
	})

	t.Run("should set default previous_secret_invalid_at when previous_secret is provided", func(t *testing.T) {
		t.Parallel()
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_Q3VycmVudFNlY3JldFN0cmluZw==",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/new",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret":          "whsec_Q3VycmVudFNlY3JldFN0cmluZw==",
				"previous_secret": "whsec_T2xkU2VjcmV0U3RyaW5nMTIz",
			}),
		)

		// Merge both config and credentials to simulate handler behavior
		newDestination.Config = maputil.MergeStringMaps(originalDestination.Config, newDestination.Config)
		newDestination.Credentials = maputil.MergeStringMaps(originalDestination.Credentials, newDestination.Credentials)

		err := provider.Preprocess(&newDestination, &originalDestination, &destregistry.PreprocessDestinationOpts{Role: "admin"})
		require.NoError(t, err)

		// Verify that previous_secret_invalid_at was set to ~24h from now
		invalidAt, err := time.Parse(time.RFC3339, newDestination.Credentials["previous_secret_invalid_at"])
		require.NoError(t, err)
		expectedTime := time.Now().Add(24 * time.Hour)
		assert.WithinDuration(t, expectedTime, invalidAt, 5*time.Second)
	})

	t.Run("should remove extra fields from credentials map", func(t *testing.T) {
		t.Parallel()
		originalDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_Q3VycmVudFNlY3JldFN0cmluZw==",
			}),
		)

		newDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/new",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret":                     "whsec_Q3VycmVudFNlY3JldFN0cmluZw==",
				"previous_secret":            "whsec_T2xkU2VjcmV0U3RyaW5nMTIz",
				"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				"extra_field":                "should be removed",
				"another_extra":              "also removed",
				"rotate_secret":              "false",
			}),
		)

		// Merge both config and credentials to simulate handler behavior
		newDestination.Config = maputil.MergeStringMaps(originalDestination.Config, newDestination.Config)
		newDestination.Credentials = maputil.MergeStringMaps(originalDestination.Credentials, newDestination.Credentials)

		err := provider.Preprocess(&newDestination, &originalDestination, &destregistry.PreprocessDestinationOpts{Role: "admin"})
		require.NoError(t, err)

		// Verify that only expected fields are present
		expectedFields := map[string]bool{
			"secret":                     true,
			"previous_secret":            true,
			"previous_secret_invalid_at": true,
		}

		// Check that only expected fields exist
		for key := range newDestination.Credentials {
			assert.True(t, expectedFields[key], "unexpected field %q found in credentials", key)
		}

		// Check that all expected fields are present
		assert.Equal(t, len(expectedFields), len(newDestination.Credentials), "credentials map has wrong number of fields")

		// Verify values are preserved for expected fields
		assert.Equal(t, "whsec_Q3VycmVudFNlY3JldFN0cmluZw==", newDestination.Credentials["secret"])
		assert.Equal(t, "whsec_T2xkU2VjcmV0U3RyaW5nMTIz", newDestination.Credentials["previous_secret"])
		assert.NotEmpty(t, newDestination.Credentials["previous_secret_invalid_at"])
	})
}
