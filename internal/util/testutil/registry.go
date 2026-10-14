package testutil

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

// PrivateCloudCIDRs permits the local emulator networks used by integration tests.
const PrivateCloudCIDRs = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

func PrivateCloudEndpointPolicy() *destregistry.CloudEndpointPolicy {
	policy, err := destregistry.NewCloudEndpointPolicy(PrivateCloudCIDRs)
	if err != nil {
		panic(err)
	}
	return policy
}

var Registry destregistry.Registry

func init() {
	Registry = destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: "",
	}, logging.NewTestLogger(zap.NewNop()))
	err := destregistrydefault.RegisterDefault(Registry, destregistrydefault.RegisterDefaultDestinationOptions{
		CloudAllowedCIDRs: PrivateCloudCIDRs,
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
