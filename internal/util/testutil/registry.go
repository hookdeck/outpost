package testutil

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

var Registry destregistry.Registry

func init() {
	Registry = destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: "",
	}, logging.NewTestLogger(zap.NewNop()))
	err := destregistrydefault.RegisterDefault(Registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
		AWSEventBridge: &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
	})
	if err != nil {
		panic(err)
	}
}
