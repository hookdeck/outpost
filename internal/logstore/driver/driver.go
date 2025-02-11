package driver

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
)

type LogStore interface {
	ListEvent(context.Context, ListEventRequest) ([]*models.Event, string, error)
	RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error)
	ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*models.Delivery, error)
	InsertManyEvent(context.Context, []*models.Event) error
	InsertManyDelivery(context.Context, []*models.Delivery) error
}

type ListEventRequest struct {
	TenantID string
	Cursor   string
	Limit    int
}

type ListDeliveryRequest struct {
	EventID string
}
