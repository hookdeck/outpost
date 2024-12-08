package testutil

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

var Registry destregistry.Registry

func init() {
	Registry = destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: "",
	}, otelzap.L())
	destregistrydefault.RegisterDefault(Registry, destregistrydefault.RegisterDefaultDestinationOptions{})
}
