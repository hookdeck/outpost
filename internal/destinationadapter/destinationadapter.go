package destinationadapter

import (
	"errors"

	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	webhookdestination "github.com/hookdeck/EventKit/internal/destinationadapter/adapters/webhook"
)

type Destination = adapters.DestinationAdapterValue

func NewAdapater(destinationType string) (adapters.DestinationAdapter, error) {
	switch destinationType {
	case "webhooks":
		return webhookdestination.New(), nil
	default:
		return nil, errors.New("invalid destination type")
	}
}
