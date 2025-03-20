package logstore

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
)

func NewNoopLogStore() LogStore {
	return &noopLogStore{}
}

type noopLogStore struct{}

func (l *noopLogStore) ListEvent(ctx context.Context, request ListEventRequest) (ListEventResponse, error) {
	return ListEventResponse{}, nil
}

func (l *noopLogStore) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	return nil, nil
}

func (l *noopLogStore) RetrieveEventByDestination(ctx context.Context, tenantID, destinationID, eventID string) (*models.Event, error) {
	return nil, nil
}

func (l *noopLogStore) ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*models.Delivery, error) {
	return nil, nil
}

func (l *noopLogStore) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	return nil
}
