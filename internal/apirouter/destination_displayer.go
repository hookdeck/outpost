package apirouter

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
)

type destinationDisplayer interface {
	Display(dest *models.Destination) (*destregistry.DestinationDisplay, error)
}

type registryDisplayer struct {
	registry destregistry.Registry
}

func newDestinationDisplayer(r destregistry.Registry) destinationDisplayer {
	return &registryDisplayer{registry: r}
}

func (d *registryDisplayer) Display(dest *models.Destination) (*destregistry.DestinationDisplay, error) {
	return d.registry.DisplayDestination(dest)
}
