package apirouter

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
)

type destinationDisplayer struct {
	registry             destregistry.Registry
	topicsAllowWildcards bool
}

func newDestinationDisplayer(r destregistry.Registry, topicsAllowWildcards bool) *destinationDisplayer {
	return &destinationDisplayer{
		registry:             r,
		topicsAllowWildcards: topicsAllowWildcards,
	}
}

func (d *destinationDisplayer) Display(dest *models.Destination) (*destregistry.DestinationDisplay, error) {
	display, err := d.registry.DisplayDestination(dest)
	if err != nil {
		return nil, err
	}
	if !d.topicsAllowWildcards {
		displayDestination := *display.Destination
		displayDestination.Topics = display.Destination.Topics.WithoutWildcardPatterns()
		display.Destination = &displayDestination
	}

	return display, nil
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
