package driver

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// TimeFilter represents time-based filter criteria with support for
// both inclusive (GTE/LTE) and exclusive (GT/LT) comparisons.
type TimeFilter struct {
	GTE *time.Time // Greater than or equal (>=)
	LTE *time.Time // Less than or equal (<=)
	GT  *time.Time // Greater than (>)
	LT  *time.Time // Less than (<)
}

type LogStore interface {
	ListEvent(context.Context, ListEventRequest) (ListEventResponse, error)
	ListDelivery(context.Context, ListDeliveryRequest) (ListDeliveryResponse, error)
	RetrieveEvent(ctx context.Context, request RetrieveEventRequest) (*models.Event, error)
	RetrieveDelivery(ctx context.Context, request RetrieveDeliveryRequest) (*DeliveryRecord, error)
	InsertMany(context.Context, []*models.LogEntry) error
}

type ListEventRequest struct {
	Next           string
	Prev           string
	Limit          int
	TimeFilter     TimeFilter // optional - filter events by time
	TenantID       string     // optional - filter by tenant (if empty, returns all tenants)
	DestinationIDs []string   // optional
	Topics         []string   // optional
	SortOrder      string     // optional: "asc", "desc" (default: "desc")
}

type ListEventResponse struct {
	Data []*models.Event
	Next string
	Prev string
}

type ListDeliveryRequest struct {
	Next           string
	Prev           string
	Limit          int
	TimeFilter     TimeFilter // optional - filter deliveries by time
	TenantID       string     // optional - filter by tenant (if empty, returns all tenants)
	EventID        string     // optional - filter for specific event
	DestinationIDs []string   // optional
	Status         string     // optional: "success", "failed"
	Topics         []string   // optional
	SortOrder      string     // optional: "asc", "desc" (default: "desc")
}

type ListDeliveryResponse struct {
	Data []*DeliveryRecord
	Next string
	Prev string
}

type RetrieveEventRequest struct {
	TenantID      string // optional - filter by tenant (if empty, searches all tenants)
	EventID       string // required
	DestinationID string // optional - if provided, scopes to that destination
}

type RetrieveDeliveryRequest struct {
	TenantID   string // optional - filter by tenant (if empty, searches all tenants)
	DeliveryID string // required
}

// DeliveryRecord represents a delivery query result with optional Event population.
type DeliveryRecord struct {
	Delivery *models.Delivery
	Event    *models.Event // optionally populated for query results
}
