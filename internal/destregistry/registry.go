package destregistry

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

// Registry manages providers, their metadata, and publishers
type Registry interface {
	// Operations
	ValidateDestination(ctx context.Context, destination *models.Destination) error
	PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) error

	// Provider management
	RegisterProvider(destinationType string, provider Provider) error
	ResolveProvider(destination *models.Destination) (Provider, error)
	ResolvePublisher(ctx context.Context, destination *models.Destination) (Publisher, error)

	// Metadata access
	MetadataLoader() *metadata.MetadataLoader
	RetrieveProviderMetadata(providerType string) (*metadata.ProviderMetadata, error)
	ListProviderMetadata() map[string]*metadata.ProviderMetadata
}

// Provider interface handles validation and publisher creation
type Provider interface {
	// Validate destination configuration using metadata
	Validate(ctx context.Context, destination *models.Destination) error
	// Create a new publisher instance
	CreatePublisher(ctx context.Context, destination *models.Destination) (Publisher, error)
	// Get provider metadata
	Metadata() *metadata.ProviderMetadata
}

type Publisher interface {
	Publish(ctx context.Context, event *models.Event) error
	Close() error
}

type registry struct {
	metadataLoader *metadata.MetadataLoader
	metadata       map[string]*metadata.ProviderMetadata
	providers      map[string]Provider  // Set during init
	publishers     map[string]Publisher // Need mutex for concurrent access
	mu             sync.RWMutex         // Protects publishers map
}

type Config struct {
	DestinationMetadataPath string
}

func NewRegistry(cfg *Config) Registry {
	return &registry{
		metadataLoader: metadata.NewMetadataLoader(cfg.DestinationMetadataPath),
		metadata:       make(map[string]*metadata.ProviderMetadata),
		providers:      make(map[string]Provider),
		publishers:     make(map[string]Publisher),
	}
}

func (r *registry) ValidateDestination(ctx context.Context, destination *models.Destination) error {
	provider, err := r.ResolveProvider(destination)
	if err != nil {
		return err
	}
	if err := provider.Validate(ctx, destination); err != nil {
		var validateErr *ErrDestinationValidation
		if errors.As(err, &validateErr) {
			return validateErr
		}
		return NewErrDestinationValidation([]ValidationErrorDetail{
			{
				Field: "root",
				Type:  "unknown",
			},
		})
	}
	return nil
}

func (r *registry) PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) error {
	publisher, err := r.ResolvePublisher(ctx, destination)
	if err != nil {
		return err
	}
	if err := publisher.Publish(ctx, event); err != nil {
		var publishErr *ErrDestinationPublishAttempt
		if errors.As(err, &publishErr) {
			return publishErr
		}
		return &ErrUnexpectedPublishError{Err: err}
	}
	return nil
}

func (r *registry) RegisterProvider(destinationType string, provider Provider) error {
	r.providers[destinationType] = provider
	r.metadata[destinationType] = provider.Metadata()
	return nil
}

func (r *registry) ResolveProvider(destination *models.Destination) (Provider, error) {
	provider, exists := r.providers[destination.Type]
	if !exists {
		return nil, fmt.Errorf("no provider registered for destination type: %s", destination.Type)
	}
	return provider, nil
}

// MakePublisherKey creates a unique key for a destination that includes type and config
func MakePublisherKey(dest *models.Destination) string {
	h := fnv.New64a()
	for k, v := range dest.Config {
		h.Write([]byte(k))
		h.Write([]byte(v))
	}
	h.Write([]byte(dest.Type))
	return dest.ID + "." + strconv.FormatUint(h.Sum64(), 36)
}

func (r *registry) ResolvePublisher(ctx context.Context, destination *models.Destination) (Publisher, error) {
	key := MakePublisherKey(destination)

	r.mu.RLock()
	publisher, exists := r.publishers[key]
	r.mu.RUnlock()
	if exists {
		return publisher, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if publisher, exists = r.publishers[key]; exists {
		return publisher, nil
	}

	provider, err := r.ResolveProvider(destination)
	if err != nil {
		return nil, err
	}

	publisher, err = provider.CreatePublisher(ctx, destination)
	if err != nil {
		return nil, fmt.Errorf("failed to create publisher: %w", err)
	}

	r.publishers[key] = publisher
	return publisher, nil
}

func (r *registry) MetadataLoader() *metadata.MetadataLoader {
	return r.metadataLoader
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
