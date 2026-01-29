// Package memtenantstore provides an in-memory implementation of driver.TenantStore.
package memtenantstore

import (
	"context"
	"fmt"
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
	destination models.Destination
	deletedAt   *time.Time
}

type store struct {
	mu sync.RWMutex

	tenants        map[string]*tenantRecord      // tenantID -> record
	destinations   map[string]*destinationRecord  // "tenantID\x00destID" -> record
	destsByTenant  map[string]map[string]struct{} // tenantID -> set of destIDs

	maxDestinationsPerTenant int
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

// New creates a new in-memory TenantStore.
func New(opts ...Option) driver.TenantStore {
	s := &store{
		tenants:                  make(map[string]*tenantRecord),
		destinations:             make(map[string]*destinationRecord),
		destsByTenant:            make(map[string]map[string]struct{}),
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
			}
		}
		delete(s.destsByTenant, tenantID)
	}

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
				data, err := cursor.Decode(c, "tnt", 1)
				if err != nil {
					return "", fmt.Errorf("%w: %v", driver.ErrInvalidCursor, err)
				}
				return data, nil
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
		cursorTs, err := strconv.ParseInt(q.CursorPos, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid timestamp", driver.ErrInvalidCursor)
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

func (s *store) ListDestinationByTenant(_ context.Context, tenantID string, options ...driver.ListDestinationByTenantOpts) ([]models.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var opts driver.ListDestinationByTenantOpts
	if len(options) > 0 {
		opts = options[0]
	}

	destIDs := s.destsByTenant[tenantID]
	if len(destIDs) == 0 {
		return []models.Destination{}, nil
	}

	var destinations []models.Destination
	for destID := range destIDs {
		drec, ok := s.destinations[destKey(tenantID, destID)]
		if !ok || drec.deletedAt != nil {
			continue
		}
		if opts.Filter != nil && !matchDestFilter(opts.Filter, drec.destination) {
			continue
		}
		destinations = append(destinations, drec.destination)
	}

	sort.Slice(destinations, func(i, j int) bool {
		return destinations[i].CreatedAt.Before(destinations[j].CreatedAt)
	})

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
	d := drec.destination
	return &d, nil
}

func (s *store) CreateDestination(_ context.Context, destination models.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := destKey(destination.TenantID, destination.ID)

	// Check for existing non-deleted destination
	if drec, ok := s.destinations[key]; ok && drec.deletedAt == nil {
		return driver.ErrDuplicateDestination
	}

	// Check max destinations
	destIDs := s.destsByTenant[destination.TenantID]
	if len(destIDs) >= s.maxDestinationsPerTenant {
		return driver.ErrMaxDestinationsPerTenantReached
	}

	return s.upsertDestinationLocked(destination)
}

func (s *store) UpsertDestination(_ context.Context, destination models.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upsertDestinationLocked(destination)
}

func (s *store) upsertDestinationLocked(destination models.Destination) error {
	now := time.Now()
	if destination.CreatedAt.IsZero() {
		destination.CreatedAt = now
	}
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}

	key := destKey(destination.TenantID, destination.ID)
	s.destinations[key] = &destinationRecord{destination: destination}

	// Update destsByTenant index
	if s.destsByTenant[destination.TenantID] == nil {
		s.destsByTenant[destination.TenantID] = make(map[string]struct{})
	}
	s.destsByTenant[destination.TenantID][destination.ID] = struct{}{}
	return nil
}

func (s *store) DeleteDestination(_ context.Context, tenantID, destinationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := destKey(tenantID, destinationID)
	drec, ok := s.destinations[key]
	if !ok {
		return driver.ErrDestinationNotFound
	}
	// Already deleted is OK (idempotent)
	now := time.Now()
	drec.deletedAt = &now

	// Remove from destsByTenant index
	if destIDs, ok := s.destsByTenant[tenantID]; ok {
		delete(destIDs, destinationID)
	}

	return nil
}

func (s *store) MatchEvent(_ context.Context, event models.Event) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	destIDs := s.destsByTenant[event.TenantID]
	var matched []string
	for destID := range destIDs {
		drec, ok := s.destinations[destKey(event.TenantID, destID)]
		if !ok || drec.deletedAt != nil {
			continue
		}
		if drec.destination.MatchEvent(event) {
			matched = append(matched, destID)
		}
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

func matchDestFilter(filter *driver.DestinationFilter, dest models.Destination) bool {
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
				if !slices.Contains(dest.Topics, topic) {
					return false
				}
			}
		}
	}
	return true
}
