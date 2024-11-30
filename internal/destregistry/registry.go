package destregistry

import (
	"errors"

	"github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destaws"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
)

type ErrDestinationPublish = providers.ErrDestinationPublish

func GetProvider(destinationType string) (providers.DestinationProvider, error) {
	switch destinationType {
	case "aws":
		return destaws.New(), nil
	case "rabbitmq":
		return destrabbitmq.New(), nil
	case "webhook":
		return destwebhook.New(), nil
	default:
		return nil, errors.New("invalid destination type")
	}
}
