package destregistry

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/models"
)

type Registry interface {
	GetProvider(destinationType string) (Provider, error)
	RegisterProvider(destinationType string, provider Provider) error
}

type Provider interface {
	// Validate destination configuration and credentials
	Validate(ctx context.Context, destination *models.Destination) error

	// Publish an event to the destination
	Publish(ctx context.Context, destination *models.Destination, event *models.Event) error
}

type registry struct {
	providers map[string]Provider
}

func NewRegistry() Registry {
	return &registry{
		providers: make(map[string]Provider),
	}
}

func (r *registry) GetProvider(destinationType string) (Provider, error) {
	provider, exists := r.providers[destinationType]
	if !exists {
		return nil, fmt.Errorf("unsupported destination type: %s", destinationType)
	}
	return provider, nil
}

func (r *registry) RegisterProvider(destinationType string, provider Provider) error {
	if _, exists := r.providers[destinationType]; exists {
		return fmt.Errorf("provider already registered for type: %s", destinationType)
	}
	r.providers[destinationType] = provider
	return nil
}
