package driver

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// ErrInvalidCursor is returned when the cursor is malformed or doesn't match
// the current query parameters (e.g., different sort order).
var ErrInvalidCursor = errors.New("invalid cursor")

type LogStore interface {
	ListDeliveryEvent(context.Context, ListDeliveryEventRequest) (ListDeliveryEventResponse, error)
	RetrieveEvent(ctx context.Context, request RetrieveEventRequest) (*models.Event, error)
	RetrieveDeliveryEvent(ctx context.Context, request RetrieveDeliveryEventRequest) (*models.DeliveryEvent, error)
	InsertManyDeliveryEvent(context.Context, []*models.DeliveryEvent) error
}

type ListDeliveryEventRequest struct {
	Next           string
	Prev           string
	Limit          int
	EventStart     *time.Time // optional - filter events created after this time
	EventEnd       *time.Time // optional - filter events created before this time
	DeliveryStart  *time.Time // optional - filter deliveries after this time
	DeliveryEnd    *time.Time // optional - filter deliveries before this time
	TenantID       string     // required
	EventID        string     // optional - filter for specific event
	DestinationIDs []string   // optional
	Status         string     // optional: "success", "failed"
	Topics         []string   // optional
	SortBy         string     // optional: "event_time", "delivery_time" (default: "delivery_time")
	SortOrder      string     // optional: "asc", "desc" (default: "desc")
}

type ListDeliveryEventResponse struct {
	Data []*models.DeliveryEvent
	Next string
	Prev string
}

type RetrieveEventRequest struct {
	TenantID      string // required
	EventID       string // required
	DestinationID string // optional - if provided, scopes to that destination
}

type RetrieveDeliveryEventRequest struct {
	TenantID   string // required
	DeliveryID string // required
}
