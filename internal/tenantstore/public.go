package tenantstore

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
)

type publicStore struct {
	TenantStore
}

// NewPublicStore returns a store for the destinations API: destinations of an
// internal type answer as if they did not exist.
func NewPublicStore(store TenantStore) TenantStore {
	return &publicStore{TenantStore: store}
}

func (s *publicStore) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error) {
	destination, err := s.TenantStore.RetrieveDestination(ctx, tenantID, destinationID)
	if destination != nil && models.IsInternalDestinationType(destination.Type) {
		return nil, nil
	}
	return destination, err
}

func (s *publicStore) ListDestination(ctx context.Context, req ListDestinationRequest) ([]models.Destination, error) {
	req.ExcludeInternal = true
	return s.TenantStore.ListDestination(ctx, req)
}
