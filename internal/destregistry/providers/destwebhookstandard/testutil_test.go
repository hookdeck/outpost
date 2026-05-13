package destwebhookstandard_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhookstandard"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// newTestProvider creates a standard webhook provider with the default "webhook-" prefix.
// Tests can override specific options by passing additional options.
func newTestProvider(t *testing.T, opts ...destwebhookstandard.Option) *destwebhookstandard.StandardWebhookDestination {
	t.Helper()

	baseOpts := []destwebhookstandard.Option{
		destwebhookstandard.WithHeaderPrefix("webhook-"),
	}
	baseOpts = append(baseOpts, opts...)

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil, baseOpts...)
	require.NoError(t, err)

	return provider
}
