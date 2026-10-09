// Package memtenantstore provides an in-memory implementation of driver.TenantStore.
package memtenantstore

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

const defaultMaxDestinationsPerTenant = 20

const (
	defaultListTenantLimit = 20
	maxListTenantLimit     = 100
)

type tenantRecord struct {
	tenant    models.Tenant
	deletedAt *time.Time
}

type destinationRecord struct {
	destination   models.Destination
	deletedAt     *time.Time
	deletedReason string
	buckets       []string // buckets joined at creation
}

// snapshot returns a copy of the destination that shares no time pointers
// with the record.
func (r *destinationRecord) snapshot() models.Destination {
	d := r.destination
	d.DisabledAt = cloneTime(d.DisabledAt)
	d.ExpiresAt = cloneTime(d.ExpiresAt)
	return d
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

type store struct {
	mu sync.RWMutex

	tenants       map[string]*tenantRecord       // tenantID -> record
	destinations  map[string]*destinationRecord  // "tenantID\x00destID" -> record
	destsByTenant map[string]map[string]struct{} // tenantID -> set of destIDs

	buckets     map[string]map[string]map[string]struct{} // tenantID -> bucket -> set of destIDs
	fences      map[string]fence                          // "tenantID\x00name" -> fence
	parked      map[string]*expiringSet                   // "tenantID\x00destID" -> parked retries
	resumeSets  map[string]*expiringSet                   // resume key -> parked retries
	index       map[string]map[string]int64               // indexKey -> member -> score
	indexTopics map[string]map[string]struct{}            // type -> topics

	maxDestinationsPerTenant int
	typeLimits               map[string]int
	limitedTypes             []string
	indexedTypes             map[string]struct{}
}

var _ driver.TenantStore = (*store)(nil)

// Option configures a memtenantstore.
type Option func(*store)

// WithMaxDestinationsPerTenant sets the max destinations per tenant.
func WithMaxDestinationsPerTenant(max int) Option {
	return func(s *store) {
		s.maxDestinationsPerTenant = max
	}
}

// WithTypeLimits gives destination types their own per-tenant limit. A
// limited type is counted in the bucket driver.TypeBucket(type) instead of
// toward the max destinations per tenant. Entries <= 0 are ignored.
func WithTypeLimits(limits map[string]int) Option {
	return func(s *store) {
		s.typeLimits = make(map[string]int, len(limits))
		s.limitedTypes = nil
		for typ, max := range limits {
			if max > 0 {
				s.typeLimits[typ] = max
				s.limitedTypes = append(s.limitedTypes, typ)
			}
		}
		sort.Strings(s.limitedTypes)
	}
}

// WithIndexedTypes maintains the cross-tenant destination indexes for these
// types.
func WithIndexedTypes(types ...string) Option {
	return func(s *store) {
		s.indexedTypes = make(map[string]struct{}, len(types))
		for _, typ := range types {
			s.indexedTypes[typ] = struct{}{}
		}
	}
}

// New creates a new in-memory TenantStore.
func New(opts ...Option) driver.TenantStore {
	s := &store{
		tenants:                  make(map[string]*tenantRecord),
		destinations:             make(map[string]*destinationRecord),
		destsByTenant:            make(map[string]map[string]struct{}),
		buckets:                  make(map[string]map[string]map[string]struct{}),
		fences:                   make(map[string]fence),
		parked:                   make(map[string]*expiringSet),
		resumeSets:               make(map[string]*expiringSet),
		index:                    make(map[string]map[string]int64),
		indexTopics:              make(map[string]map[string]struct{}),
		maxDestinationsPerTenant: defaultMaxDestinationsPerTenant,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func destKey(tenantID, destID string) string {
	return tenantID + "\x00" + destID
}

func (s *store) Init(_ context.Context) error {
	return nil
}

func (s *store) RetrieveTenant(_ context.Context, tenantID string) (*models.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.tenants[tenantID]
	if !ok {
		return nil, nil
	}
	if rec.deletedAt != nil {
		return nil, driver.ErrTenantDeleted
	}

	t := rec.tenant
	destIDs := s.destsByTenant[tenantID]
	t.DestinationsCount = len(destIDs)
	t.Topics = s.computeTenantTopics(tenantID)
	return &t, nil
}

func (s *store) UpsertTenant(_ context.Context, tenant models.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	if tenant.UpdatedAt.IsZero() {
		tenant.UpdatedAt = now
	}

	s.tenants[tenant.ID] = &tenantRecord{tenant: tenant}
	return nil
}

func (s *store) DeleteTenant(_ context.Context, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.tenants[tenantID]
	if !ok {
		return driver.ErrTenantNotFound
	}
	// Already deleted is OK (idempotent)
	now := time.Now()
	rec.deletedAt = &now

	// Delete all destinations
	if destIDs, ok := s.destsByTenant[tenantID]; ok {
		for destID := range destIDs {
			if drec, ok := s.destinations[destKey(tenantID, destID)]; ok {
				drec.deletedAt = &now
				drec.deletedReason = driver.DeleteReasonTenantDeleted
				s.removeIndexedLocked(&drec.destination)
			}
			delete(s.parked, destKey(tenantID, destID))
		}
		delete(s.destsByTenant, tenantID)
	}
	delete(s.buckets, tenantID)

	return nil
}

func (s *store) ListTenant(ctx context.Context, req driver.ListTenantRequest) (*driver.TenantPaginatedResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if req.Next != "" && req.Prev != "" {
		return nil, driver.ErrConflictingCursors
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultListTenantLimit
	}
	if limit > maxListTenantLimit {
		limit = maxListTenantLimit
	}

	dir := req.Dir
	if dir == "" {
		dir = "desc"
	}
	if dir != "asc" && dir != "desc" {
		return nil, driver.ErrInvalidOrder
	}

	// Collect non-deleted tenants
	var activeTenants []models.Tenant
	for _, rec := range s.tenants {
		if rec.deletedAt != nil {
			continue
		}
		activeTenants = append(activeTenants, rec.tenant)
	}

	// Filter by ID when specified
	if len(req.ID) > 0 {
		idSet := make(map[string]struct{}, len(req.ID))
		for _, id := range req.ID {
			idSet[id] = struct{}{}
		}
		filtered := activeTenants[:0]
		for _, t := range activeTenants {
			if _, ok := idSet[t.ID]; ok {
				filtered = append(filtered, t)
			}
		}
		activeTenants = filtered
	}
	totalCount := len(activeTenants)

	result, err := pagination.Run(ctx, pagination.Config[models.Tenant]{
		Limit: limit,
		Order: dir,
		Next:  req.Next,
		Prev:  req.Prev,
		Cursor: pagination.Cursor[models.Tenant]{
			Encode: func(t models.Tenant) string {
				return cursor.Encode("tnt", 1, strconv.FormatInt(t.CreatedAt.UnixMilli(), 10))
			},
			Decode: func(c string) (string, error) {
				return cursor.Decode(c, "tnt", 1)
			},
		},
		Fetch: func(_ context.Context, q pagination.QueryInput) ([]models.Tenant, error) {
			return s.fetchTenants(activeTenants, q)
		},
	})
	if err != nil {
		return nil, err
	}

	tenants := result.Items

	// Enrich with DestinationsCount and Topics
	for i := range tenants {
		destIDs := s.destsByTenant[tenants[i].ID]
		tenants[i].DestinationsCount = len(destIDs)
		tenants[i].Topics = s.computeTenantTopics(tenants[i].ID)
	}

	var nextCursor, prevCursor *string
	if result.Next != "" {
		nextCursor = &result.Next
	}
	if result.Prev != "" {
		prevCursor = &result.Prev
	}

	return &driver.TenantPaginatedResult{
		Models: tenants,
		Pagination: driver.SeekPagination{
			OrderBy: "created_at",
			Dir:     dir,
			Limit:   limit,
			Next:    nextCursor,
			Prev:    prevCursor,
		},
		Count: totalCount,
	}, nil
}

func (s *store) fetchTenants(activeTenants []models.Tenant, q pagination.QueryInput) ([]models.Tenant, error) {
	var filtered []models.Tenant

	if q.CursorPos == "" {
		filtered = append(filtered, activeTenants...)
	} else {
		cursorTs, err := cursor.ParseTimeMs(q.CursorPos)
		if err != nil {
			return nil, err
		}
		for _, t := range activeTenants {
			ts := t.CreatedAt.UnixMilli()
			if q.Compare == "<" && ts < cursorTs {
				filtered = append(filtered, t)
			} else if q.Compare == ">" && ts > cursorTs {
				filtered = append(filtered, t)
			}
		}
	}

	// Sort
	if q.SortDir == "desc" {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		})
	} else {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		})
	}

	// Apply limit
	if len(filtered) > q.Limit {
		filtered = filtered[:q.Limit]
	}

	return filtered, nil
}

func (s *store) ListDestination(_ context.Context, req driver.ListDestinationRequest) ([]models.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filter *destinationFilter
	hasFilter := len(req.Type) > 0 || len(req.ExcludeTypes) > 0 || len(req.Topics) > 0
	if hasFilter {
		filter = &destinationFilter{
			Type:           req.Type,
			ExcludeTypes:   req.ExcludeTypes,
			Topics:         req.Topics,
			AllowWildcards: req.AllowWildcards,
		}
	}

	var destinations []models.Destination

	if len(req.IDs) > 0 {
		for _, id := range req.IDs {
			drec, ok := s.destinations[destKey(req.TenantID, id)]
			if !ok || drec.deletedAt != nil {
				continue
			}
			if hasFilter && !matchDestFilter(filter, drec.destination) {
				continue
			}
			destinations = append(destinations, drec.snapshot())
		}
	} else {
		destIDs := s.destsByTenant[req.TenantID]
		for destID := range destIDs {
			drec, ok := s.destinations[destKey(req.TenantID, destID)]
			if !ok || drec.deletedAt != nil {
				continue
			}
			if hasFilter && !matchDestFilter(filter, drec.destination) {
				continue
			}
			destinations = append(destinations, drec.snapshot())
		}
	}

	sort.Slice(destinations, func(i, j int) bool {
		return destinations[i].CreatedAt.Before(destinations[j].CreatedAt)
	})

	if destinations == nil {
		destinations = []models.Destination{}
	}

	return destinations, nil
}

func (s *store) RetrieveDestination(_ context.Context, tenantID, destinationID string) (*models.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	drec, ok := s.destinations[destKey(tenantID, destinationID)]
	if !ok {
		return nil, nil
	}
	if drec.deletedAt != nil {
		return nil, driver.ErrDestinationDeleted
	}
	d := drec.snapshot()
	return &d, nil
}

func (s *store) UpsertDestination(_ context.Context, destination models.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var buckets []string
	if drec, ok := s.destinations[destKey(destination.TenantID, destination.ID)]; ok {
		buckets = drec.buckets
	}
	s.upsertDestinationLocked(destination, buckets)
	return nil
}

func (s *store) upsertDestinationLocked(destination models.Destination, buckets []string) {
	now := time.Now()
	if destination.CreatedAt.IsZero() {
		destination.CreatedAt = now
	}
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}
	destination.DisabledAt = cloneTime(destination.DisabledAt)
	destination.ExpiresAt = cloneTime(destination.ExpiresAt)

	key := destKey(destination.TenantID, destination.ID)
	s.destinations[key] = &destinationRecord{destination: destination, buckets: buckets}

	// Update destsByTenant index
	if s.destsByTenant[destination.TenantID] == nil {
		s.destsByTenant[destination.TenantID] = make(map[string]struct{})
	}
	s.destsByTenant[destination.TenantID][destination.ID] = struct{}{}

	if s.isIndexed(destination.Type) {
		s.addIndexedLocked(&destination)
	}
}

func (s *store) DeleteDestination(_ context.Context, tenantID, destinationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.destinations[destKey(tenantID, destinationID)]; !ok {
		return driver.ErrDestinationNotFound
	}
	// Already deleted is OK (idempotent)
	s.deleteIfLocked(tenantID, destinationID, driver.DeleteCondition{})
	return nil
}

func (s *store) MatchEvent(_ context.Context, event models.Event, allowWildcards bool) ([]driver.MatchedDestination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	// Built for the first filter that needs it, then shared by all.
	var input models.FilterInput
	var matched []driver.MatchedDestination
	for destID := range s.destsByTenant[event.TenantID] {
		drec, ok := s.destinations[destKey(event.TenantID, destID)]
		if !ok || drec.deletedAt != nil {
			continue
		}
		d := &drec.destination
		if d.DisabledAt != nil || d.IsExpired(now) {
			continue
		}
		if !models.MatchDestinationTopic(d.Type, d.Topics, event.Topic, allowWildcards) {
			continue
		}
		if len(d.Filter) > 0 {
			if input == nil {
				input = models.NewFilterInput(event)
			}
			if !models.MatchFilterInput(d.Filter, input) {
				continue
			}
		}
		matched = append(matched, driver.MatchedDestination{ID: destID, Type: d.Type})
	}
	return matched, nil
}

func (s *store) computeTenantTopics(tenantID string) []string {
	destIDs := s.destsByTenant[tenantID]
	all := false
	topicsSet := make(map[string]struct{})
	for destID := range destIDs {
		drec, ok := s.destinations[destKey(tenantID, destID)]
		if !ok || drec.deletedAt != nil {
			continue
		}
		for _, topic := range drec.destination.Topics {
			if topic == "*" {
				all = true
				break
			}
			topicsSet[topic] = struct{}{}
		}
	}

	if all {
		return []string{"*"}
	}

	topics := make([]string, 0, len(topicsSet))
	for topic := range topicsSet {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

// destinationFilter specifies criteria for filtering destinations (package-private).
type destinationFilter struct {
	Type           []string
	ExcludeTypes   []string
	Topics         []string
	AllowWildcards bool
}

func matchDestFilter(filter *destinationFilter, dest models.Destination) bool {
	if slices.Contains(filter.ExcludeTypes, dest.Type) {
		return false
	}
	if len(filter.Type) > 0 && !slices.Contains(filter.Type, dest.Type) {
		return false
	}
	if len(filter.Topics) > 0 {
		filterMatchesAll := len(filter.Topics) == 1 && filter.Topics[0] == "*"
		if !dest.Topics.MatchesAll() {
			if filterMatchesAll {
				return false
			}
			for _, topic := range filter.Topics {
				if !dest.Topics.MatchTopic(topic, filter.AllowWildcards) {
					return false
				}
			}
		}
	}
	return true
}
