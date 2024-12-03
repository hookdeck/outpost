package testutil

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
)

var Registry destregistry.Registry

func init() {
	Registry = destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: "",
	})
	destregistrydefault.RegisterDefault(Registry)
}
