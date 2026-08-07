package destwebhook

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Formatters are built once in New() and shared by every publisher. This pins
// that sharing: a regression back to per-destination construction (rebuilding
// formatters inside CreatePublisher) makes the identity assertions fail.
//
// White-box on purpose — the formatter fields are unexported, and testutil
// can't be imported here without a cycle, so the destination is built inline.
func TestWebhookDestination_PublishersShareFormatters(t *testing.T) {
	t.Parallel()

	provider, err := New(metadata.NewMetadataLoader(""), nil,
		WithHeaderPrefix(DefaultHeaderPrefix),
		WithSignatureContentTemplate(DefaultSignatureContentTmpl),
		WithSignatureHeaderTemplate(DefaultSignatureHeaderTmpl),
		WithSignatureEncoding(DefaultEncoding),
		WithSignatureAlgorithm(DefaultAlgorithm),
		WithSigningSecretTemplate(DefaultSigningSecretTmpl),
	)
	require.NoError(t, err)

	now := time.Now()
	dest := models.Destination{
		ID:          "dest-formatter-sharing",
		TenantID:    "test-tenant",
		Type:        "webhook",
		Topics:      []string{"*"},
		Config:      map[string]string{"url": "http://localhost:8080/webhook"},
		Credentials: map[string]string{"secret": "test-secret"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	pub1, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err)
	defer pub1.Close()
	pub2, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err)
	defer pub2.Close()

	wp1, ok := pub1.(*WebhookPublisher)
	require.True(t, ok)
	wp2, ok := pub2.(*WebhookPublisher)
	require.True(t, ok)

	// Both publishers hold the provider's formatter instances, not copies.
	assert.Same(t, provider.signatureFormatter, wp1.sm.sigFormatter)
	assert.Same(t, provider.headerFormatter, wp1.sm.headerFormatter)
	assert.Same(t, wp1.sm.sigFormatter, wp2.sm.sigFormatter)
	assert.Same(t, wp1.sm.headerFormatter, wp2.sm.headerFormatter)
}
