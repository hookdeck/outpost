package logstore

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
)

func NewNoopLogStore() LogStore {
	return &noopLogStore{}
}

type noopLogStore struct{}

func (l *noopLogStore) ListEvent(ctx context.Context, request ListEventRequest) ([]*models.Event, string, error) {
	return nil, "", nil
}

func (l *noopLogStore) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	return nil, nil
}

func (l *noopLogStore) ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*models.Delivery, error) {
	return nil, nil
}

func (l *noopLogStore) InsertManyEvent(ctx context.Context, events []*models.Event) error {
	return nil
}

func (l *noopLogStore) InsertManyDelivery(ctx context.Context, deliveries []*models.Delivery) error {
	return nil
}
