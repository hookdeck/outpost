package destregistry

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

// BaseProvider provides common functionality for all destination providers
type BaseProvider struct {
	metadata *metadata.ProviderMetadata
}

// NewBaseProvider creates a new base provider with loaded metadata
func NewBaseProvider(providerType string) (*BaseProvider, error) {
	loader := metadata.NewMetadataLoader("")
	meta, err := loader.Load(providerType)
	if err != nil {
		return nil, fmt.Errorf("loading provider metadata: %w", err)
	}

	return &BaseProvider{
		metadata: meta,
	}, nil
}

// Metadata returns the provider metadata
func (p *BaseProvider) Metadata() *metadata.ProviderMetadata {
	return p.metadata
}

// Validate performs schema validation using the provider's metadata
func (p *BaseProvider) Validate(ctx context.Context, destination *models.Destination) error {
	if err := p.metadata.Validation.Validate(map[string]interface{}{
		"config":      destination.Config,
		"credentials": destination.Credentials,
	}); err != nil {
		return NewErrDestinationValidation(err)
	}
	return nil
}
