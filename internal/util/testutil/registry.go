package testutil

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

var Registry destregistry.Registry

func init() {
	Registry = destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: "",
	}, logging.NewTestLogger(zap.NewNop()))
	destregistrydefault.RegisterDefault(Registry, destregistrydefault.RegisterDefaultDestinationOptions{})
}
