package destwebhook_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// NewTestProvider creates a webhook provider with all required configuration.
// Tests can override specific options by passing additional options.
func NewTestProvider(t *testing.T, opts ...destwebhook.Option) *destwebhook.WebhookDestination {
	t.Helper()

	// Base options with defaults - tests can override these
	baseOpts := []destwebhook.Option{
		destwebhook.WithHeaderPrefix(destwebhook.DefaultHeaderPrefix),
		destwebhook.WithSignatureContentTemplate(destwebhook.DefaultSignatureContentTmpl),
		destwebhook.WithSignatureHeaderTemplate(destwebhook.DefaultSignatureHeaderTmpl),
		destwebhook.WithSignatureEncoding(destwebhook.DefaultEncoding),
		destwebhook.WithSignatureAlgorithm(destwebhook.DefaultAlgorithm),
		destwebhook.WithSigningSecretTemplate(destwebhook.DefaultSigningSecretTmpl),
	}

	// Append test-specific options (they override base options)
	baseOpts = append(baseOpts, opts...)

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil, baseOpts...)
	require.NoError(t, err)

	return provider
}
