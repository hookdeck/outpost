package destinationadapter

import (
	"errors"

	"github.com/hookdeck/outpost/internal/destinationadapter/adapters"
	awsdestination "github.com/hookdeck/outpost/internal/destinationadapter/adapters/aws"
	rabbitmqdestination "github.com/hookdeck/outpost/internal/destinationadapter/adapters/rabbitmq"
	webhookdestination "github.com/hookdeck/outpost/internal/destinationadapter/adapters/webhook"
)

type Destination = adapters.DestinationAdapterValue

func NewAdapater(destinationType string) (adapters.DestinationAdapter, error) {
	switch destinationType {
	case "aws":
		return awsdestination.New(), nil
	case "rabbitmq":
		return rabbitmqdestination.New(), nil
	case "webhook":
		return webhookdestination.New(), nil
	default:
		return nil, errors.New("invalid destination type")
	}
}
