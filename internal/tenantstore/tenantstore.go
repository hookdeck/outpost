// Package tenantstore provides the TenantStore facade for tenant and destination storage.
package tenantstore

import (
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/tenantstore/memtenantstore"
	"github.com/hookdeck/outpost/internal/tenantstore/redistenantstore"
)

// Type aliases re-exported from driver.
type TenantStore = driver.TenantStore
type ListTenantRequest = driver.ListTenantRequest
type SeekPagination = driver.SeekPagination
type TenantPaginatedResult = driver.TenantPaginatedResult
type ListDestinationByTenantOpts = driver.ListDestinationByTenantOpts
type DestinationFilter = driver.DestinationFilter

// Error sentinels re-exported from driver.
var (
	ErrTenantNotFound                  = driver.ErrTenantNotFound
	ErrTenantDeleted                   = driver.ErrTenantDeleted
	ErrDuplicateDestination            = driver.ErrDuplicateDestination
	ErrDestinationNotFound             = driver.ErrDestinationNotFound
	ErrDestinationDeleted              = driver.ErrDestinationDeleted
	ErrMaxDestinationsPerTenantReached = driver.ErrMaxDestinationsPerTenantReached
	ErrListTenantNotSupported          = driver.ErrListTenantNotSupported
	ErrInvalidCursor                   = driver.ErrInvalidCursor
	ErrInvalidOrder                    = driver.ErrInvalidOrder
	ErrConflictingCursors              = driver.ErrConflictingCursors
)

// WithDestinationFilter creates a ListDestinationByTenantOpts with the given filter.
var WithDestinationFilter = driver.WithDestinationFilter

// Config holds the configuration for creating a TenantStore.
type Config struct {
	RedisClient              redis.Cmdable
	Secret                   string
	AvailableTopics          []string
	MaxDestinationsPerTenant int
	DeploymentID             string
}

// New creates a new Redis-backed TenantStore.
func New(cfg Config) TenantStore {
	var opts []redistenantstore.Option
	if cfg.Secret != "" {
		opts = append(opts, redistenantstore.WithSecret(cfg.Secret))
	}
	if len(cfg.AvailableTopics) > 0 {
		opts = append(opts, redistenantstore.WithAvailableTopics(cfg.AvailableTopics))
	}
	if cfg.MaxDestinationsPerTenant > 0 {
		opts = append(opts, redistenantstore.WithMaxDestinationsPerTenant(cfg.MaxDestinationsPerTenant))
	}
	if cfg.DeploymentID != "" {
		opts = append(opts, redistenantstore.WithDeploymentID(cfg.DeploymentID))
	}
	return redistenantstore.New(cfg.RedisClient, opts...)
}

// NewMemTenantStore creates an in-memory TenantStore for testing.
func NewMemTenantStore() TenantStore {
	return memtenantstore.New()
}
