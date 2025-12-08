package logstore

import (
	"context"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
)

func NewNoopLogStore() LogStore {
	return &noopLogStore{}
}

type noopLogStore struct{}

var _ LogStore = (*noopLogStore)(nil)

func (l *noopLogStore) ListDeliveryEvent(ctx context.Context, request driver.ListDeliveryEventRequest) (driver.ListDeliveryEventResponse, error) {
	return driver.ListDeliveryEventResponse{}, nil
}

func (l *noopLogStore) RetrieveEvent(ctx context.Context, request driver.RetrieveEventRequest) (*models.Event, error) {
	return nil, nil
}

func (l *noopLogStore) RetrieveDeliveryEvent(ctx context.Context, request driver.RetrieveDeliveryEventRequest) (*models.DeliveryEvent, error) {
	return nil, nil
}

func (l *noopLogStore) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	return nil
}
