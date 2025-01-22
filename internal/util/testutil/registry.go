package testutil

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

var Registry destregistry.Registry

func init() {
	otelLogger := otelzap.L()
	Registry = destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: "",
	}, &logging.Logger{Logger: otelLogger})
	destregistrydefault.RegisterDefault(Registry, destregistrydefault.RegisterDefaultDestinationOptions{})
}
