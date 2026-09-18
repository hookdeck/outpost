package destregistrydefault_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

// Signature template validation lives in destwebhook.New, so registration is
// where a bad template fails startup — and only in default mode. In 'standard'
// mode the templates are fixed by the Standard Webhooks spec, the configured
// ones are never parsed, and registration must succeed regardless of their
// contents.
func TestRegisterDefault_WebhookSignatureTemplates(t *testing.T) {
	webhookConfig := func(mode string) *destregistrydefault.DestWebhookConfig {
		return &destregistrydefault.DestWebhookConfig{
			Mode:                     mode,
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  "v0={{.Body}}", // header templates have no .Body — invalid at render
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		}
	}

	t.Run("default mode rejects an invalid template", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
			Webhook: webhookConfig(""),
		})
		assert.ErrorContains(t, err, "can't evaluate field Body")
	})

	t.Run("standard mode ignores the configured templates", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
			Webhook: webhookConfig("standard"),
		})
		assert.NoError(t, err)
	})
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
