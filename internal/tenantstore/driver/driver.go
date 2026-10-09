// Package driver defines the TenantStore interface and associated types.
package driver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/models"
)

// TenantStore is the interface for tenant and destination storage.
type TenantStore interface {
	Init(ctx context.Context) error
	RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error)
	UpsertTenant(ctx context.Context, tenant models.Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenant(ctx context.Context, req ListTenantRequest) (*TenantPaginatedResult, error)
	ListDestination(ctx context.Context, req ListDestinationRequest) ([]models.Destination, error)
	RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error)
	// CreateDestination atomically checks that no live destination has the
	// same ID, the tenant's limits (MaxDestinationsPerTenant for types without
	// their own limit, TypeLimits and the buckets of WithBuckets) and the
	// revocation guards of WithNotDeletedSince and WithFence, then writes the
	// destination. It may replace a tombstone.
	CreateDestination(ctx context.Context, destination models.Destination, opts ...WriteOption) error
	// UpsertDestination writes the destination unconditionally, replacing a
	// tombstone. Prefer UpdateDestinationIfLive for read-modify-write updates.
	UpsertDestination(ctx context.Context, destination models.Destination) error
	DeleteDestination(ctx context.Context, tenantID, destinationID string) error
	// MatchEvent returns the enabled, unexpired destinations of the event's
	// tenant whose topics and filter match the event.
	MatchEvent(ctx context.Context, event models.Event, allowWildcards bool) ([]MatchedDestination, error)

	// UpdateDestinationIfLive overwrites a destination only while it is live
	// (not deleted) and still the generation created at expectedCreatedAt
	// (millisecond precision): ErrDestinationNotFound, ErrDestinationDeleted
	// or ErrDestinationConflict otherwise, and ErrDestinationRevoked when a
	// fence of WithFence is not older than WithNotDeletedSince. The created_at
	// of the stored generation is kept. With WithResumeParkedRetries and an
	// enabled destination, the parked-retries set is renamed to a fresh resume
	// set in the same step and UpdateResult.ResumeKey names it.
	UpdateDestinationIfLive(ctx context.Context, destination models.Destination, expectedCreatedAt time.Time, opts ...WriteOption) (UpdateResult, error)
	// DisableDestination sets disabled_at on a live destination and touches
	// nothing else. It returns changed=false when it was already disabled,
	// ErrDestinationNotFound or ErrDestinationDeleted when it is not live.
	DisableDestination(ctx context.Context, tenantID, destinationID string, at time.Time) (changed bool, err error)
	// EnableDestination clears disabled_at on a live destination and touches
	// nothing else, so a concurrent update (a refresh) is never reverted.
	// UpdateResult.WasDisabled reports whether it was disabled. With
	// WithResumeParkedRetries, the parked-retries set is renamed to a fresh
	// resume set in the same step and UpdateResult.ResumeKey names it; other
	// write options are refused. It returns ErrDestinationNotFound or
	// ErrDestinationDeleted when the destination is not live.
	EnableDestination(ctx context.Context, tenantID, destinationID string, opts ...WriteOption) (UpdateResult, error)
	// DeleteDestinationIf tombstones a live destination only when it matches
	// every set field of c, recording c.Reason. Deleting also leaves the
	// destination's buckets and drops its parked retries.
	DeleteDestinationIf(ctx context.Context, tenantID, destinationID string, c DeleteCondition) (DeleteResult, error)
	// WriteFence records at as the fence name of the tenant for ttl. A fence
	// only moves forward: an older at keeps the newer value (and refreshes the
	// TTL). See WithFence.
	WriteFence(ctx context.Context, tenantID, name string, at time.Time, ttl time.Duration) error

	// ListIndexedDestinations returns up to limit members of the index of typ
	// (topic "" for the global index) with a score <= maxScore, in ascending
	// score order. Only types configured as indexed are indexed. The index is
	// a hint: entries may outlive their destination, so callers check the
	// destination and remove stale entries with RemoveIndexedDestination.
	ListIndexedDestinations(ctx context.Context, typ, topic string, maxScore int64, limit int) ([]IndexedDestination, error)
	// ScanIndexedDestinations iterates an index incrementally (ZSCAN
	// semantics: start and end with cursor 0, entries may repeat).
	ScanIndexedDestinations(ctx context.Context, typ, topic string, cursor uint64, count int) ([]IndexedDestination, uint64, error)
	// RescoreIndexedDestination sets the score of ref to newScore in the
	// global index of typ and the index of each topic, wherever its score
	// still equals ref.Score.
	RescoreIndexedDestination(ctx context.Context, typ string, topics []string, ref IndexedDestination, newScore int64) error
	// RemoveIndexedDestination removes ref from the global index of typ and
	// the index of each topic, wherever its score still equals ref.Score.
	RemoveIndexedDestination(ctx context.Context, typ string, topics []string, ref IndexedDestination) error
	// CountIndexed counts the members of an index (topic "" for the global
	// index) with a score >= minScore.
	CountIndexed(ctx context.Context, typ, topic string, minScore int64) (int64, error)
	// ListIndexedTopics returns every topic that has had an index for typ.
	// Topics are never removed, so an entry may have an empty index.
	ListIndexedTopics(ctx context.Context, typ string) ([]string, error)

	// ParkRetry adds member to the destination's parked-retries set (at most
	// max members, expiring at expireAt) only while the destination is live
	// and disabled. See ParkResult.
	ParkRetry(ctx context.Context, tenantID, destinationID, member string, max int, expireAt time.Time) (ParkResult, error)
	// PopResumeMembers removes and returns up to n members of a resume set
	// returned by UpdateDestinationIfLive. An empty result means it is empty.
	PopResumeMembers(ctx context.Context, tenantID, key string, n int) ([]string, error)
	// DeleteResumeSet deletes a resume set returned by UpdateDestinationIfLive.
	DeleteResumeSet(ctx context.Context, tenantID, key string) error
}

var (
	ErrTenantNotFound                  = errors.New("tenant does not exist")
	ErrTenantDeleted                   = errors.New("tenant has been deleted")
	ErrDuplicateDestination            = errors.New("destination already exists")
	ErrDestinationNotFound             = errors.New("destination does not exist")
	ErrDestinationDeleted              = errors.New("destination has been deleted")
	ErrMaxDestinationsPerTenantReached = errors.New("maximum number of destinations per tenant reached")
	ErrListTenantNotSupported          = errors.New("list tenant feature is not enabled")
	ErrInvalidCursor                   = cursor.ErrInvalidCursor
	ErrInvalidOrder                    = errors.New("invalid order: must be 'asc' or 'desc'")
	ErrConflictingCursors              = errors.New("cannot specify both next and prev cursors")
	// ErrDestinationRevoked rejects a write racing a revocation: the
	// destination was deleted, or a fence was written, at or after the
	// write's WithNotDeletedSince time.
	ErrDestinationRevoked = errors.New("destination has been revoked")
	// ErrDestinationConflict rejects a conditional write whose expected
	// generation (created_at) no longer matches the stored destination.
	ErrDestinationConflict = errors.New("destination has changed")
	// ErrInvalidResumeKey rejects a resume set key that does not belong to
	// the tenant's parked retries.
	ErrInvalidResumeKey = errors.New("invalid resume set key")
)

// ErrLimitReached rejects a CreateDestination that would put more than Max
// destinations in Bucket.
type ErrLimitReached struct {
	Bucket string
	Max    int
}

func (e *ErrLimitReached) Error() string {
	return fmt.Sprintf("maximum of %d destinations reached for %s", e.Max, e.Bucket)
}

// ListTenantRequest contains parameters for listing tenants.
type ListTenantRequest struct {
	Limit int      // Number of results per page (default: 20)
	Next  string   // Cursor for next page
	Prev  string   // Cursor for previous page
	Dir   string   // Sort direction: "asc" or "desc" (default: "desc")
	ID    []string // If non-empty, only tenants with these IDs are returned
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

// ListDestinationRequest contains parameters for listing destinations.
type ListDestinationRequest struct {
	TenantID       string   // required — destinations are always tenant-scoped
	IDs            []string // optional — filter to these destination IDs only
	Type           []string // optional — OR semantics (matches any)
	ExcludeTypes   []string // optional — drop destinations of these types
	Topics         []string // optional — AND semantics ("*" = wildcard-only)
	AllowWildcards bool     // whether persisted wildcard patterns participate in topic matching
}

// MatchedDestination is a destination matched by MatchEvent.
type MatchedDestination struct {
	ID   string
	Type string
}
