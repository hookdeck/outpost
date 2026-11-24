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

	provider, err := newTestProvider(opts...)
	require.NoError(t, err)

	return provider
}

// newTestProvider is NewTestProvider without the assertion, for tests that
// expect construction to fail.
func newTestProvider(opts ...destwebhook.Option) (*destwebhook.WebhookDestination, error) {
	baseOpts := []destwebhook.Option{
		destwebhook.WithHeaderPrefix(destwebhook.DefaultHeaderPrefix),
		destwebhook.WithSignatureContentTemplate(destwebhook.DefaultSignatureContentTmpl),
		destwebhook.WithSignatureHeaderTemplate(destwebhook.DefaultSignatureHeaderTmpl),
		destwebhook.WithSignatureEncoding(destwebhook.DefaultEncoding),
		destwebhook.WithSignatureAlgorithm(destwebhook.DefaultAlgorithm),
		destwebhook.WithSigningSecretTemplate(destwebhook.DefaultSigningSecretTmpl),
	}
	baseOpts = append(baseOpts, opts...)
	return destwebhook.New(testutil.Registry.MetadataLoader(), nil, baseOpts...)
}
