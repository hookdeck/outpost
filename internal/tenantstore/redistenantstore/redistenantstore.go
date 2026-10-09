// Package redistenantstore provides a Redis-backed implementation of driver.TenantStore.
package redistenantstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

const defaultMaxDestinationsPerTenant = 20

// destinationTombstoneTTL is how long deleted tenants and destinations stay
// readable as deleted.
const destinationTombstoneTTL = 7 * 24 * time.Hour

const (
	defaultListTenantLimit = 20
	maxListTenantLimit     = 100
)

var rediSearchQuotedTagEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
)

type store struct {
	redisClient              redis.Cmdable
	cipher                   *aesCipher
	availableTopics          []string
	maxDestinationsPerTenant int
	typeLimits               map[string]int
	limitedTypes             []string // sorted keys of typeLimits
	indexedTypes             map[string]struct{}
	deploymentID             string
	listTenantSupported      bool
}

var _ driver.TenantStore = (*store)(nil)

// Option configures a redistenantstore.
type Option func(*store)

// WithSecret sets the encryption secret for credentials.
func WithSecret(secret string) Option {
	return func(s *store) {
		s.cipher = newAESCipher(secret)
	}
}

// WithAvailableTopics sets the available topics for destination validation.
func WithAvailableTopics(topics []string) Option {
	return func(s *store) {
		s.availableTopics = topics
	}
}

// WithMaxDestinationsPerTenant sets the maximum number of destinations per tenant.
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

// WithIndexedTypes maintains the cross-tenant destination indexes (see
// driver.TenantStore.ListIndexedDestinations) for these types.
func WithIndexedTypes(types ...string) Option {
	return func(s *store) {
		s.indexedTypes = make(map[string]struct{}, len(types))
		for _, typ := range types {
			s.indexedTypes[typ] = struct{}{}
		}
	}
}

// WithDeploymentID sets the deployment ID for key isolation.
func WithDeploymentID(deploymentID string) Option {
	return func(s *store) {
		s.deploymentID = deploymentID
	}
}

// New creates a new Redis-backed TenantStore.
func New(redisClient redis.Cmdable, opts ...Option) driver.TenantStore {
	s := &store{
		redisClient:              redisClient,
		cipher:                   newAESCipher(""),
		availableTopics:          []string{},
		maxDestinationsPerTenant: defaultMaxDestinationsPerTenant,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// doCmd executes an arbitrary Redis command using the Do method.
func (s *store) doCmd(ctx context.Context, args ...interface{}) *redis.Cmd {
	if dc, ok := s.redisClient.(redis.DoContext); ok {
		return dc.Do(ctx, args...)
	}
	cmd := &redis.Cmd{}
	cmd.SetErr(errors.New("redis client does not support Do command"))
	return cmd
}

func (s *store) deploymentPrefix() string {
	if s.deploymentID == "" {
		return ""
	}
	return fmt.Sprintf("%s:", s.deploymentID)
}

func (s *store) redisTenantID(tenantID string) string {
	return fmt.Sprintf("%stenant:{%s}:tenant", s.deploymentPrefix(), tenantID)
}

func (s *store) redisTenantDestinationSummaryKey(tenantID string) string {
	return fmt.Sprintf("%stenant:{%s}:destinations", s.deploymentPrefix(), tenantID)
}

func (s *store) redisDestinationID(destinationID, tenantID string) string {
	return fmt.Sprintf("%stenant:{%s}:destination:%s", s.deploymentPrefix(), tenantID, destinationID)
}

// tenantScopedPrefix prefixes the tenant's other keys. They share the
// tenant's hash tag, so scripts and transactions can combine them on Redis
// Cluster, and avoid the ":destination:" infix matched by the migrations.
func (s *store) tenantScopedPrefix(tenantID string) string {
	return fmt.Sprintf("%stenant:{%s}:", s.deploymentPrefix(), tenantID)
}

// redisBucketKey is the set of destination IDs in a bucket.
func (s *store) redisBucketKey(tenantID, bucket string) string {
	return s.tenantScopedPrefix(tenantID) + "bucket:" + bucket
}

// redisBucketRegistryKey is the set of the tenant's bucket names, so that
// DeleteTenant can drop them.
func (s *store) redisBucketRegistryKey(tenantID string) string {
	return s.tenantScopedPrefix(tenantID) + "buckets"
}

// redisFenceKey holds a fence time (Unix ms) of the tenant.
func (s *store) redisFenceKey(tenantID, name string) string {
	return s.tenantScopedPrefix(tenantID) + "fence:" + name
}

// redisParkedRetriesKey is the set of a destination's parked retries. Resume
// sets are named after it (see resumeKeyInfix).
func (s *store) redisParkedRetriesKey(tenantID, destinationID string) string {
	return s.tenantScopedPrefix(tenantID) + "parked_retries:" + destinationID
}

func (s *store) tenantIndexName() string {
	return s.deploymentPrefix() + "tenant_idx"
}

func (s *store) tenantKeyPrefix() string {
	return s.deploymentPrefix() + "tenant:"
}

// Init initializes the store, probing for RediSearch support.
func (s *store) Init(ctx context.Context) error {
	_, err := s.doCmd(ctx, "FT._LIST").Result()
	if err != nil {
		s.listTenantSupported = false
		return nil
	}

	if err := s.ensureTenantIndex(ctx); err != nil {
		s.listTenantSupported = false
		return nil
	}

	s.listTenantSupported = true
	return nil
}

func (s *store) ensureTenantIndex(ctx context.Context) error {
	indexName := s.tenantIndexName()

	_, err := s.doCmd(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		return nil
	}

	prefix := s.tenantKeyPrefix()
	_, err = s.doCmd(ctx, "FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"FILTER", `@entity == "tenant"`,
		"SCHEMA",
		"id", "TAG",
		"entity", "TAG",
		"created_at", "NUMERIC", "SORTABLE",
		"deleted_at", "NUMERIC",
	).Result()

	if err != nil {
		return fmt.Errorf("failed to create tenant index: %w", err)
	}

	return nil
}

func (s *store) RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error) {
	pipe := s.redisClient.Pipeline()
	tenantCmd := pipe.HGetAll(ctx, s.redisTenantID(tenantID))
	destinationListCmd := pipe.HGetAll(ctx, s.redisTenantDestinationSummaryKey(tenantID))

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	tenantHash, err := tenantCmd.Result()
	if err != nil {
		return nil, err
	}
	if len(tenantHash) == 0 {
		return nil, nil
	}
	tenant, err := parseTenantHash(tenantHash)
	if err != nil {
		return nil, err
	}

	destinationSummaryList, err := parseListDestinationSummaryByTenantCmd(destinationListCmd, nil)
	if err != nil {
		return nil, err
	}
	tenant.DestinationsCount = len(destinationSummaryList)
	tenant.Topics = parseTenantTopics(destinationSummaryList)

	return tenant, nil
}

func (s *store) UpsertTenant(ctx context.Context, tenant models.Tenant) error {
	key := s.redisTenantID(tenant.ID)

	if err := s.redisClient.Persist(ctx, key).Err(); err != nil && err != redis.Nil {
		return err
	}

	if err := s.redisClient.HDel(ctx, key, "deleted_at").Err(); err != nil && err != redis.Nil {
		return err
	}

	now := time.Now()
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	if tenant.UpdatedAt.IsZero() {
		tenant.UpdatedAt = now
	}

	if err := s.redisClient.HSet(ctx, key,
		"id", tenant.ID,
		"entity", "tenant",
		"created_at", tenant.CreatedAt.UnixMilli(),
		"updated_at", tenant.UpdatedAt.UnixMilli(),
	).Err(); err != nil {
		return err
	}

	if tenant.Metadata != nil {
		if err := s.redisClient.HSet(ctx, key, "metadata", &tenant.Metadata).Err(); err != nil {
			return err
		}
	} else {
		if err := s.redisClient.HDel(ctx, key, "metadata").Err(); err != nil && err != redis.Nil {
			return err
		}
	}

	return nil
}

func (s *store) DeleteTenant(ctx context.Context, tenantID string) error {
	if exists, err := s.redisClient.Exists(ctx, s.redisTenantID(tenantID)).Result(); err != nil {
		return err
	} else if exists == 0 {
		return driver.ErrTenantNotFound
	}

	pipe := s.redisClient.Pipeline()
	summariesCmd := pipe.HGetAll(ctx, s.redisTenantDestinationSummaryKey(tenantID))
	bucketsCmd := pipe.SMembers(ctx, s.redisBucketRegistryKey(tenantID))
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return err
	}
	summaries, err := summariesCmd.Result()
	if err != nil && err != redis.Nil {
		return err
	}
	buckets, err := bucketsCmd.Result()
	if err != nil && err != redis.Nil {
		return err
	}

	_, err = s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		nowUnixMilli := time.Now().UnixMilli()

		for destinationID := range summaries {
			destKey := s.redisDestinationID(destinationID, tenantID)
			pipe.HSet(ctx, destKey, "deleted_at", nowUnixMilli, "deleted_reason", driver.DeleteReasonTenantDeleted)
			pipe.Expire(ctx, destKey, destinationTombstoneTTL)
			pipe.Del(ctx, s.redisParkedRetriesKey(tenantID, destinationID))
		}
		for _, bucket := range buckets {
			pipe.Del(ctx, s.redisBucketKey(tenantID, bucket))
		}
		pipe.Del(ctx, s.redisBucketRegistryKey(tenantID))

		pipe.Del(ctx, s.redisTenantDestinationSummaryKey(tenantID))
		pipe.HSet(ctx, s.redisTenantID(tenantID), "deleted_at", nowUnixMilli)
		pipe.Expire(ctx, s.redisTenantID(tenantID), destinationTombstoneTTL)

		return nil
	})
	if err != nil {
		return err
	}

	// The indexes live outside the tenant's hash slot, so they are cleaned
	// after the transaction, comparing scores so that a destination created
	// again meanwhile keeps its entry. Best effort: an entry left behind is
	// removed by whoever next finds its destination gone.
	for destinationID, raw := range summaries {
		var ds destinationSummary
		if err := ds.UnmarshalBinary([]byte(raw)); err != nil || !s.isIndexed(ds.Type) {
			continue
		}
		ref := driver.IndexedDestination{TenantID: tenantID, DestinationID: destinationID, Score: ds.indexScore()}
		_ = s.RemoveIndexedDestination(ctx, ds.Type, ds.Topics, ref)
	}

	return nil
}

func (s *store) ListTenant(ctx context.Context, req driver.ListTenantRequest) (*driver.TenantPaginatedResult, error) {
	if !s.listTenantSupported {
		return nil, driver.ErrListTenantNotSupported
	}

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

	baseFilter := "@entity:{tenant} -@deleted_at:[1 +inf]"
	if len(req.ID) > 0 {
		// Use DIALECT 2 quoted tags so IDs with punctuation are matched exactly.
		escaped := make([]string, len(req.ID))
		for i, id := range req.ID {
			escaped[i] = `"` + rediSearchQuotedTagEscaper.Replace(id) + `"`
		}
		baseFilter += " @id:{" + strings.Join(escaped, "|") + "}"
	}

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
		Fetch: func(ctx context.Context, q pagination.QueryInput) ([]models.Tenant, error) {
			return s.fetchTenants(ctx, baseFilter, q)
		},
	})
	if err != nil {
		return nil, err
	}

	tenants := result.Items

	if len(tenants) > 0 {
		pipe := s.redisClient.Pipeline()
		cmds := make([]*redis.MapStringStringCmd, len(tenants))
		for i, t := range tenants {
			cmds[i] = pipe.HGetAll(ctx, s.redisTenantDestinationSummaryKey(t.ID))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, fmt.Errorf("failed to fetch destination summaries: %w", err)
		}

		for i := range tenants {
			destinationSummaryList, err := parseListDestinationSummaryByTenantCmd(cmds[i], nil)
			if err != nil {
				return nil, err
			}
			tenants[i].DestinationsCount = len(destinationSummaryList)
			tenants[i].Topics = parseTenantTopics(destinationSummaryList)
		}
	}

	var totalCount int
	countResult, err := s.doCmd(ctx, "FT.SEARCH", s.tenantIndexName(),
		baseFilter,
		"LIMIT", 0, 0,
		"DIALECT", 2,
	).Result()
	if err == nil {
		_, totalCount, _ = parseSearchResult(countResult)
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

func (s *store) fetchTenants(ctx context.Context, baseFilter string, q pagination.QueryInput) ([]models.Tenant, error) {
	var query string
	sortDir := "DESC"
	if q.SortDir == "asc" {
		sortDir = "ASC"
	}

	if q.CursorPos == "" {
		query = baseFilter
	} else {
		cursorTimestamp, err := cursor.ParseTimeMs(q.CursorPos)
		if err != nil {
			return nil, err
		}

		if q.Compare == "<" {
			query = fmt.Sprintf("(@created_at:[0 %d]) %s", cursorTimestamp-1, baseFilter)
		} else {
			query = fmt.Sprintf("(@created_at:[%d +inf]) %s", cursorTimestamp+1, baseFilter)
		}
	}

	result, err := s.doCmd(ctx, "FT.SEARCH", s.tenantIndexName(),
		query,
		"SORTBY", "created_at", sortDir,
		"LIMIT", 0, q.Limit,
		"DIALECT", 2,
	).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to search tenants: %w", err)
	}

	tenants, _, err := parseSearchResult(result)
	if err != nil {
		return nil, err
	}

	return tenants, nil
}

func (s *store) listDestinationSummaryByTenant(ctx context.Context, tenantID string, filter *destinationFilter) ([]destinationSummary, error) {
	return parseListDestinationSummaryByTenantCmd(s.redisClient.HGetAll(ctx, s.redisTenantDestinationSummaryKey(tenantID)), filter)
}

func (s *store) ListDestination(ctx context.Context, req driver.ListDestinationRequest) ([]models.Destination, error) {
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

	var summaries []destinationSummary

	if len(req.IDs) > 0 {
		// Batch-by-ID: use HMGET on the summary key to fetch only requested IDs.
		summaryKey := s.redisTenantDestinationSummaryKey(req.TenantID)
		vals, err := s.redisClient.HMGet(ctx, summaryKey, req.IDs...).Result()
		if err != nil {
			return nil, err
		}
		for _, val := range vals {
			if val == nil {
				continue
			}
			str, ok := val.(string)
			if !ok {
				continue
			}
			var ds destinationSummary
			if err := ds.UnmarshalBinary([]byte(str)); err != nil {
				return nil, err
			}
			if hasFilter && !matchDestinationFilter(filter, ds) {
				continue
			}
			summaries = append(summaries, ds)
		}
	} else {
		// List-all: HGETALL summary key, filter in-memory.
		var err error
		summaries, err = s.listDestinationSummaryByTenant(ctx, req.TenantID, filter)
		if err != nil {
			return nil, err
		}
	}

	if len(summaries) == 0 {
		return []models.Destination{}, nil
	}

	// Pipeline fetch full destination hashes.
	pipe := s.redisClient.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(summaries))
	for i, ds := range summaries {
		cmds[i] = pipe.HGetAll(ctx, s.redisDestinationID(ds.ID, req.TenantID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	var destinations []models.Destination
	for _, cmd := range cmds {
		dest, err := parseDestinationHash(cmd, req.TenantID, s.cipher)
		if err != nil {
			if err == redis.Nil || err == driver.ErrDestinationDeleted {
				continue
			}
			return nil, err
		}
		destinations = append(destinations, *dest)
	}

	sort.Slice(destinations, func(i, j int) bool {
		return destinations[i].CreatedAt.Before(destinations[j].CreatedAt)
	})

	if destinations == nil {
		destinations = []models.Destination{}
	}

	return destinations, nil
}

func (s *store) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error) {
	cmd := s.redisClient.HGetAll(ctx, s.redisDestinationID(destinationID, tenantID))
	destination, err := parseDestinationHash(cmd, tenantID, s.cipher)
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return destination, nil
}

func (s *store) UpsertDestination(ctx context.Context, destination models.Destination) error {
	now := time.Now()
	if destination.CreatedAt.IsZero() {
		destination.CreatedAt = now
	}
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}

	w, err := s.newDestinationWrite(&destination)
	if err != nil {
		return err
	}

	// Index first, so that an index entry may outlive its destination but a
	// live destination is never missing from the index.
	if s.isIndexed(destination.Type) {
		if err := s.addIndexed(ctx, &destination); err != nil {
			return err
		}
	}

	key := s.redisDestinationID(destination.ID, destination.TenantID)
	summaryKey := s.redisTenantDestinationSummaryKey(destination.TenantID)

	_, err = s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Persist(ctx, key)
		pipe.HDel(ctx, key, "deleted_at", "deleted_reason")
		pipe.HSet(ctx, key, w.set...)
		if len(w.del) > 0 {
			pipe.HDel(ctx, key, w.del...)
		}
		pipe.HSet(ctx, summaryKey, destination.ID, w.summary)
		return nil
	})

	return err
}

func (s *store) DeleteDestination(ctx context.Context, tenantID, destinationID string) error {
	status, _, err := s.deleteDestinationIf(ctx, tenantID, destinationID, driver.DeleteCondition{})
	if err != nil {
		return err
	}
	if status == deleteStatusMissing {
		return driver.ErrDestinationNotFound
	}
	return nil
}

func (s *store) MatchEvent(ctx context.Context, event models.Event, allowWildcards bool) ([]driver.MatchedDestination, error) {
	destinationSummaryList, err := s.listDestinationSummaryByTenant(ctx, event.TenantID, nil)
	if err != nil {
		return nil, err
	}

	nowMs := time.Now().UnixMilli()
	// Built for the first filter that needs it, then shared by all.
	var input models.FilterInput
	var matched []driver.MatchedDestination

	for _, ds := range destinationSummaryList {
		if ds.Disabled || ds.expired(nowMs) {
			continue
		}
		if !models.MatchDestinationTopic(ds.Type, ds.Topics, event.Topic, allowWildcards) {
			continue
		}
		// Filters are decoded only for the entries that got this far.
		filter, err := ds.filter()
		if err != nil {
			return nil, fmt.Errorf("invalid filter of destination %s: %w", ds.ID, err)
		}
		if len(filter) > 0 {
			if input == nil {
				input = models.NewFilterInput(event)
			}
			if !models.MatchFilterInput(filter, input) {
				continue
			}
		}
		matched = append(matched, driver.MatchedDestination{ID: ds.ID, Type: ds.Type})
	}

	return matched, nil
}
