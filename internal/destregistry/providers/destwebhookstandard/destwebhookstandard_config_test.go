package destwebhookstandard_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhookstandard"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardWebhookDestination_CustomHeadersConfig(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t)

	t.Run("should parse config with valid custom_headers", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{"x-api-key":"secret123","x-tenant-id":"tenant-abc"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		err := provider.Validate(context.Background(), &destination)
		assert.NoError(t, err)
	})

	t.Run("should accept empty custom_headers object", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		err := provider.Validate(context.Background(), &destination)
		assert.NoError(t, err)
	})

	t.Run("should parse config without custom_headers field (backward compatibility)", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/webhook",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		err := provider.Validate(context.Background(), &destination)
		assert.NoError(t, err)
	})

	t.Run("should fail on invalid custom_headers JSON", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{invalid json}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		err := provider.Validate(context.Background(), &destination)
		assert.Error(t, err)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.custom_headers", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid", validationErr.Errors[0].Type)
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates provider with no options", func(t *testing.T) {
		t.Parallel()
		provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)
		assert.NotNil(t, provider)
	})

	t.Run("creates provider with user agent option", func(t *testing.T) {
		t.Parallel()
		provider, err := destwebhookstandard.New(
			testutil.Registry.MetadataLoader(),
			nil,
			destwebhookstandard.WithUserAgent("test-agent"),
		)
		require.NoError(t, err)
		assert.NotNil(t, provider)
	})

	t.Run("creates provider with proxy chain option", func(t *testing.T) {
		t.Parallel()
		provider, err := destwebhookstandard.New(
			testutil.Registry.MetadataLoader(),
			nil,
			destwebhookstandard.WithProxy(mustProxy(t, "http://proxy.example.com")),
		)
		require.NoError(t, err)
		assert.NotNil(t, provider)
	})

	t.Run("creates provider with header prefix option", func(t *testing.T) {
		t.Parallel()
		provider, err := destwebhookstandard.New(
			testutil.Registry.MetadataLoader(),
			nil,
			destwebhookstandard.WithHeaderPrefix("x-custom-"),
		)
		require.NoError(t, err)
		assert.NotNil(t, provider)
	})

	t.Run("creates provider with multiple options", func(t *testing.T) {
		t.Parallel()
		provider, err := destwebhookstandard.New(
			testutil.Registry.MetadataLoader(),
			nil,
			destwebhookstandard.WithUserAgent("test-agent"),
			destwebhookstandard.WithProxy(mustProxy(t, "http://proxy.example.com")),
			destwebhookstandard.WithHeaderPrefix("x-outpost-"),
		)
		require.NoError(t, err)
		assert.NotNil(t, provider)
	})
}

func mustProxy(t *testing.T, s string) []*url.URL {
	t.Helper()
	hops, err := destregistry.ParseProxyURL(s)
	require.NoError(t, err)
	return hops
}
