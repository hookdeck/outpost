package destinationadapter

import (
	"errors"

	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	rabbitmqdestination "github.com/hookdeck/EventKit/internal/destinationadapter/adapters/rabbitmq"
	webhookdestination "github.com/hookdeck/EventKit/internal/destinationadapter/adapters/webhook"
)

type Destination = adapters.DestinationAdapterValue

func NewAdapater(destinationType string) (adapters.DestinationAdapter, error) {
	switch destinationType {
	case "rabbitmq":
		return rabbitmqdestination.New(), nil
	case "webhooks":
		return webhookdestination.New(), nil
	default:
		return nil, errors.New("invalid destination type")
	}
}
