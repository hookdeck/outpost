package destregistry

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

type Registry interface {
	RegisterProvider(destinationType string, provider Provider) error
	GetProvider(destinationType string) (Provider, error)
	RetrieveProviderMetadata(providerType string) (*metadata.ProviderMetadata, error)
	ListProviderMetadata() map[string]*metadata.ProviderMetadata
}

type Provider interface {
	// Metadata returns the metadata for the provider
	Metadata() *metadata.ProviderMetadata

	// Validate destination configuration and credentials
	Validate(ctx context.Context, destination *models.Destination) error

	// Publish an event to the destination
	Publish(ctx context.Context, destination *models.Destination, event *models.Event) error
}

type registry struct {
	providers map[string]Provider
	metadata  map[string]*metadata.ProviderMetadata
}

func NewRegistry() Registry {
	return &registry{
		providers: make(map[string]Provider),
		metadata:  make(map[string]*metadata.ProviderMetadata),
	}
}

func (r *registry) RegisterProvider(destinationType string, provider Provider) error {
	r.providers[destinationType] = provider
	r.metadata[destinationType] = provider.Metadata()
	return nil
}

func (r *registry) GetProvider(destinationType string) (Provider, error) {
	provider, exists := r.providers[destinationType]
	if !exists {
		return nil, fmt.Errorf("unsupported destination type: %s", destinationType)
	}
	return provider, nil
}

func (r *registry) RetrieveProviderMetadata(providerType string) (*metadata.ProviderMetadata, error) {
	meta, ok := r.metadata[providerType]
	if !ok {
		return nil, fmt.Errorf("metadata for provider %s not found", providerType)
	}
	return meta, nil
}

func (r *registry) ListProviderMetadata() map[string]*metadata.ProviderMetadata {
	// Return a copy to prevent modification of internal state
	metadataCopy := make(map[string]*metadata.ProviderMetadata, len(r.metadata))
	for k, v := range r.metadata {
		metadataCopy[k] = v
	}
	return metadataCopy
}
