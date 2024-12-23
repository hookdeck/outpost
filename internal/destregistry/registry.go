package destregistry

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/lru"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

// Registry manages providers, their metadata, and publishers
type Registry interface {
	// Operations
	ValidateDestination(ctx context.Context, destination *models.Destination) error
	PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) error
	DisplayDestination(destination *models.Destination) (*DestinationDisplay, error)

	// Provider management
	RegisterProvider(destinationType string, provider Provider) error
	ResolveProvider(destination *models.Destination) (Provider, error)
	ResolvePublisher(ctx context.Context, destination *models.Destination) (Publisher, error)

	// Metadata access
	MetadataLoader() metadata.MetadataLoader
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
	// ObfuscateDestination returns a copy of the destination with sensitive fields masked
	ObfuscateDestination(destination *models.Destination) *models.Destination
	// ComputeTarget returns a human-readable target string for the destination
	ComputeTarget(destination *models.Destination) string
}

type Publisher interface {
	Publish(ctx context.Context, event *models.Event) error
	Close() error
}

type registry struct {
	metadataLoader metadata.MetadataLoader
	metadata       map[string]*metadata.ProviderMetadata
	providers      map[string]Provider
	publishers     *lru.Cache[string, Publisher]
	config         Config
}

type Config struct {
	DestinationMetadataPath string
	PublisherCacheSize      int
	PublisherTTL            time.Duration
	DeliveryTimeout         time.Duration
}

func NewRegistry(cfg *Config, logger *otelzap.Logger) *registry {
	if cfg.PublisherCacheSize == 0 {
		cfg.PublisherCacheSize = defaultPublisherCacheSize
	}
	if cfg.PublisherTTL == 0 {
		cfg.PublisherTTL = defaultPublisherTTL
	}
	if cfg.DeliveryTimeout == 0 {
		cfg.DeliveryTimeout = defaultDeliveryTimeout
	}

	onEvict := func(key string, p Publisher) {
		if err := p.Close(); err != nil {
			logger.Error("failed to close publisher on eviction",
				zap.String("key", key),
				zap.Error(err),
			)
		}
	}

	cache := lru.New[string, Publisher](cfg.PublisherCacheSize, cfg.PublisherTTL, onEvict)

	return &registry{
		metadataLoader: metadata.NewMetadataLoader(cfg.DestinationMetadataPath),
		metadata:       make(map[string]*metadata.ProviderMetadata),
		providers:      make(map[string]Provider),
		publishers:     cache,
		config:         *cfg,
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

	// Create a new context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, r.config.DeliveryTimeout)
	defer cancel()

	if err := publisher.Publish(timeoutCtx, event); err != nil {
		var publishErr *ErrDestinationPublishAttempt
		if errors.As(err, &publishErr) {
			// Check if the wrapped error is a timeout
			if errors.Is(publishErr.Err, context.DeadlineExceeded) {
				return &ErrDestinationPublishAttempt{
					Err:      publishErr.Err,
					Provider: destination.Type,
					Data: map[string]interface{}{
						"error":   "timeout",
						"timeout": r.config.DeliveryTimeout.String(),
					},
				}
			}
			return publishErr
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &ErrDestinationPublishAttempt{
				Err:      err,
				Provider: destination.Type,
				Data: map[string]interface{}{
					"error":   "timeout",
					"timeout": r.config.DeliveryTimeout.String(),
				},
			}
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

	if publisher, ok := r.publishers.Get(key); ok {
		return publisher, nil
	}

	provider, err := r.ResolveProvider(destination)
	if err != nil {
		return nil, err
	}

	publisher, err := provider.CreatePublisher(ctx, destination)
	if err != nil {
		return nil, fmt.Errorf("failed to create publisher: %w", err)
	}

	r.publishers.Add(key, publisher)
	return publisher, nil
}

func (r *registry) MetadataLoader() metadata.MetadataLoader {
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

func (r *registry) ObfuscateDestination(destination *models.Destination) (*models.Destination, error) {
	provider, err := r.ResolveProvider(destination)
	if err != nil {
		return nil, err
	}
	return provider.ObfuscateDestination(destination), nil
}

func (r *registry) DisplayDestination(destination *models.Destination) (*DestinationDisplay, error) {
	provider, err := r.ResolveProvider(destination)
	if err != nil {
		return nil, err
	}

	// First obfuscate the destination
	obfuscated := provider.ObfuscateDestination(destination)

	// Then compute the target
	target := provider.ComputeTarget(destination)

	return &DestinationDisplay{
		Destination: obfuscated,
		Target:      target,
	}, nil
}

var (
	defaultPublisherCacheSize = 10000
	defaultPublisherTTL       = time.Minute
	defaultDeliveryTimeout    = 5 * time.Second
)
