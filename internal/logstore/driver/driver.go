package driver

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
)

type LogStore interface {
	ListEvent(context.Context, ListEventRequest) ([]*models.Event, string, error)
	RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error)
	RetrieveEventByDestination(ctx context.Context, tenantID, destinationID, eventID string) (*models.Event, error)
	ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*models.Delivery, error)
	InsertManyDeliveryEvent(context.Context, []*models.DeliveryEvent) error
}

type ListEventRequest struct {
	TenantID       string   // required
	DestinationIDs []string // optional
	Status         string   // optional, "success", "failed"
	Cursor         string
	Limit          int
}

type ListEventByDestinationRequest struct {
	TenantID      string // required
	DestinationID string // required
	Status        string // optional, "success", "failed"
	Cursor        string
	Limit         int
}

type ListDeliveryRequest struct {
	EventID string
}
