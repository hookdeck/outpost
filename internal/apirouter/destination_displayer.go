package apirouter

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
)

type destinationDisplayer struct {
	registry destregistry.Registry
}

func newDestinationDisplayer(r destregistry.Registry) *destinationDisplayer {
	return &destinationDisplayer{registry: r}
}

func (d *destinationDisplayer) Display(dest *models.Destination) (*destregistry.DestinationDisplay, error) {
	return d.registry.DisplayDestination(dest)
}

func (d *destinationDisplayer) DisplayList(destinations []models.Destination) ([]*destregistry.DestinationDisplay, error) {
	result := make([]*destregistry.DestinationDisplay, len(destinations))
	for i := range destinations {
		display, err := d.Display(&destinations[i])
		if err != nil {
			return nil, err
		}
		result[i] = display
	}
	return result, nil
}
