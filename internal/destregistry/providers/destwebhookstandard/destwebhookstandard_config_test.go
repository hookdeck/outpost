package destwebhookstandard_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhookstandard"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates provider with defaults", func(t *testing.T) {
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

	t.Run("creates provider with proxy URL option", func(t *testing.T) {
		t.Parallel()
		provider, err := destwebhookstandard.New(
			testutil.Registry.MetadataLoader(),
			nil,
			destwebhookstandard.WithProxyURL("http://proxy.example.com"),
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
			destwebhookstandard.WithProxyURL("http://proxy.example.com"),
		)
		require.NoError(t, err)
		assert.NotNil(t, provider)
	})
}
