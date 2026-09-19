package destregistrydefault_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Signature template validation lives in destwebhook.New, so registration is
// where a bad template fails startup.
func TestRegisterDefault_WebhookSignatureTemplates(t *testing.T) {
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  "v0={{.Body}}", // header templates have no .Body — invalid at render
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
	})
	assert.ErrorContains(t, err, "can't evaluate field Body")
}

// Standard mode swaps the provider's metadata for the entry that carries the
// Standard Webhooks verification instructions.
func TestRegisterDefault_WebhookStandardMetadata(t *testing.T) {
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			MetadataName:             destwebhook.StandardMetadataName,
			HeaderPrefix:             destwebhook.StandardHeaderPrefix,
			SignatureContentTemplate: destwebhook.StandardSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.StandardSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.StandardEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.StandardSigningSecretTmpl,
			SignatureSecretEncoding:  destwebhook.StandardSecretEncoding,
			SignatureSecretPrefix:    destwebhook.StandardSecretPrefix,
		},
	})
	require.NoError(t, err)

	provider, err := registry.ResolveProvider(&models.Destination{Type: "webhook"})
	require.NoError(t, err)
	instructions := provider.Metadata().Instructions
	assert.Contains(t, instructions, "webhook-signature")
}

func TestRegisterDefault_WebhookCompatSignature(t *testing.T) {
	webhookConfig := func(compat *destwebhook.CompatSignatureConfig) *destregistrydefault.DestWebhookConfig {
		return &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
			Compat:                   compat,
		}
	}

	t.Run("registers with a valid compat signature", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
			Webhook: webhookConfig(&destwebhook.CompatSignatureConfig{
				SignatureHeaderName:      "x-legacy-signature",
				SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
				SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
				SignatureEncoding:        destwebhook.DefaultEncoding,
				SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			}),
		})
		assert.NoError(t, err)
	})

	t.Run("rejects an invalid compat signature", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
			Webhook: webhookConfig(&destwebhook.CompatSignatureConfig{
				SignatureHeaderName:      "x-legacy-signature",
				SignatureContentTemplate: "{{.Nope}}",
				SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
				SignatureEncoding:        destwebhook.DefaultEncoding,
				SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			}),
		})
		assert.ErrorContains(t, err, "compat signature")
	})
}
