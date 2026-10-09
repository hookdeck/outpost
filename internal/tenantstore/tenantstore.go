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
type ListDestinationRequest = driver.ListDestinationRequest
type MatchedDestination = driver.MatchedDestination
type Bucket = driver.Bucket
type WriteOption = driver.WriteOption
type UpdateResult = driver.UpdateResult
type DeleteCondition = driver.DeleteCondition
type DeleteResult = driver.DeleteResult
type IndexedDestination = driver.IndexedDestination
type ParkResult = driver.ParkResult
type ErrLimitReached = driver.ErrLimitReached

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
	ErrDestinationRevoked              = driver.ErrDestinationRevoked
	ErrDestinationConflict             = driver.ErrDestinationConflict
	ErrInvalidResumeKey                = driver.ErrInvalidResumeKey
)

// Write options, constants and helpers re-exported from driver.
var (
	WithBuckets             = driver.WithBuckets
	WithNotDeletedSince     = driver.WithNotDeletedSince
	WithFence               = driver.WithFence
	WithResumeParkedRetries = driver.WithResumeParkedRetries
	TypeBucket              = driver.TypeBucket
	IndexScore              = driver.IndexScore
)

const (
	NoExpiryScore             = driver.NoExpiryScore
	DeleteReasonExpired       = driver.DeleteReasonExpired
	DeleteReasonUnsubscribed  = driver.DeleteReasonUnsubscribed
	DeleteReasonRevoked       = driver.DeleteReasonRevoked
	DeleteReasonTerminated    = driver.DeleteReasonTerminated
	DeleteReasonTenantDeleted = driver.DeleteReasonTenantDeleted
	ParkResultParked          = driver.ParkResultParked
	ParkResultEnabled         = driver.ParkResultEnabled
	ParkResultGone            = driver.ParkResultGone
	ParkResultFull            = driver.ParkResultFull
)

// Config holds the configuration for creating a TenantStore.
type Config struct {
	RedisClient              redis.Cmdable
	Secret                   string
	AvailableTopics          []string
	MaxDestinationsPerTenant int
	// TypeLimits gives destination types their own per-tenant limit; they
	// don't count toward MaxDestinationsPerTenant.
	TypeLimits map[string]int
	// IndexedTypes are the destination types kept in the cross-tenant
	// indexes (ListIndexedDestinations).
	IndexedTypes []string
	DeploymentID string
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
	if len(cfg.TypeLimits) > 0 {
		opts = append(opts, redistenantstore.WithTypeLimits(cfg.TypeLimits))
	}
	if len(cfg.IndexedTypes) > 0 {
		opts = append(opts, redistenantstore.WithIndexedTypes(cfg.IndexedTypes...))
	}
	if cfg.DeploymentID != "" {
		opts = append(opts, redistenantstore.WithDeploymentID(cfg.DeploymentID))
	}
	return redistenantstore.New(cfg.RedisClient, opts...)
}

// MemOption configures NewMemTenantStore.
type MemOption = memtenantstore.Option

// Options of NewMemTenantStore, matching the Config fields of New.
var (
	MemWithMaxDestinationsPerTenant = memtenantstore.WithMaxDestinationsPerTenant
	MemWithTypeLimits               = memtenantstore.WithTypeLimits
	MemWithIndexedTypes             = memtenantstore.WithIndexedTypes
)

// NewMemTenantStore creates an in-memory TenantStore for testing.
func NewMemTenantStore(opts ...MemOption) TenantStore {
	return memtenantstore.New(opts...)
}
