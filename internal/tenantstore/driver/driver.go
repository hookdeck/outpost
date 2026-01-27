// Package driver defines the TenantStore interface and associated types.
package driver

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/models"
)

// TenantStore is the interface for tenant and destination storage.
type TenantStore interface {
	Init(ctx context.Context) error
	RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error)
	UpsertTenant(ctx context.Context, tenant models.Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenant(ctx context.Context, req ListTenantRequest) (*TenantPaginatedResult, error)
	ListDestinationByTenant(ctx context.Context, tenantID string, options ...ListDestinationByTenantOpts) ([]models.Destination, error)
	RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error)
	CreateDestination(ctx context.Context, destination models.Destination) error
	UpsertDestination(ctx context.Context, destination models.Destination) error
	DeleteDestination(ctx context.Context, tenantID, destinationID string) error
	MatchEvent(ctx context.Context, event models.Event) ([]models.DestinationSummary, error)
}

var (
	ErrTenantNotFound                  = errors.New("tenant does not exist")
	ErrTenantDeleted                   = errors.New("tenant has been deleted")
	ErrDuplicateDestination            = errors.New("destination already exists")
	ErrDestinationNotFound             = errors.New("destination does not exist")
	ErrDestinationDeleted              = errors.New("destination has been deleted")
	ErrMaxDestinationsPerTenantReached = errors.New("maximum number of destinations per tenant reached")
	ErrListTenantNotSupported          = errors.New("list tenant feature is not enabled")
	ErrInvalidCursor                   = errors.New("invalid cursor")
	ErrInvalidOrder                    = errors.New("invalid order: must be 'asc' or 'desc'")
	ErrConflictingCursors              = errors.New("cannot specify both next and prev cursors")
)

// ListTenantRequest contains parameters for listing tenants.
type ListTenantRequest struct {
	Limit int    // Number of results per page (default: 20)
	Next  string // Cursor for next page
	Prev  string // Cursor for previous page
	Dir   string // Sort direction: "asc" or "desc" (default: "desc")
}

// SeekPagination represents cursor-based pagination metadata for list responses.
type SeekPagination struct {
	OrderBy string  `json:"order_by"`
	Dir     string  `json:"dir"`
	Limit   int     `json:"limit"`
	Next    *string `json:"next"`
	Prev    *string `json:"prev"`
}

// TenantPaginatedResult contains the paginated list of tenants.
type TenantPaginatedResult struct {
	Models     []models.Tenant `json:"models"`
	Pagination SeekPagination  `json:"pagination"`
	Count      int             `json:"count"`
}

// ListDestinationByTenantOpts contains options for listing destinations.
type ListDestinationByTenantOpts struct {
	Filter *DestinationFilter
}

// DestinationFilter specifies criteria for filtering destinations.
type DestinationFilter struct {
	Type   []string
	Topics []string
}

// WithDestinationFilter creates a ListDestinationByTenantOpts with the given filter.
func WithDestinationFilter(filter DestinationFilter) ListDestinationByTenantOpts {
	return ListDestinationByTenantOpts{Filter: &filter}
}
