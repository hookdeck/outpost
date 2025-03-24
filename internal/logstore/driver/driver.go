package driver

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

type LogStore interface {
	ListEvent(context.Context, ListEventRequest) (ListEventResponse, error)
	RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error)
	RetrieveEventByDestination(ctx context.Context, tenantID, destinationID, eventID string) (*models.Event, error)
	ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*models.Delivery, error)
	InsertManyDeliveryEvent(context.Context, []*models.DeliveryEvent) error
}

type ListEventRequest struct {
	Next           string
	Prev           string
	Limit          int
	Start          *time.Time // optional - lower bound, default End - 1h
	End            *time.Time // optional - upper bound, default now()
	TenantID       string     // required
	DestinationIDs []string   // optional
	Status         string     // optional, "success", "failed"
	Topics         []string   // optional
}

type ListEventByDestinationRequest struct {
	TenantID      string // required
	DestinationID string // required
	Status        string // optional, "success", "failed"
	Cursor        string
	Limit         int
}

type ListDeliveryRequest struct {
	EventID       string
	DestinationID string
}

type ListEventResponse struct {
	Data  []*models.Event
	Next  string
	Prev  string
	Count int64
}
