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

// deletedRecordTTL is how long deleted tenants and destinations stay readable.
const deletedRecordTTL = 7 * 24 * time.Hour

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

// upsertTenantScript replaces a tenant's fields and revives it if deleted, in one step.
// KEYS: tenant hash.
// ARGV: id, created_at, updated_at, has metadata (0 or 1), metadata.
var upsertTenantScript = redis.NewScript(`
redis.call('PERSIST', KEYS[1])
redis.call('HDEL', KEYS[1], 'deleted_at')
redis.call('HSET', KEYS[1], 'id', ARGV[1], 'entity', 'tenant', 'created_at', ARGV[2], 'updated_at', ARGV[3])
if ARGV[4] == '1' then
	redis.call('HSET', KEYS[1], 'metadata', ARGV[5])
else
	redis.call('HDEL', KEYS[1], 'metadata')
end
return 0
`)

func (s *store) UpsertTenant(ctx context.Context, tenant models.Tenant) error {
	now := time.Now()
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	if tenant.UpdatedAt.IsZero() {
		tenant.UpdatedAt = now
	}

	hasMetadata, metadata := 0, interface{}("")
	if tenant.Metadata != nil {
		hasMetadata, metadata = 1, &tenant.Metadata
	}
	return upsertTenantScript.Run(ctx, s.redisClient, []string{s.redisTenantID(tenant.ID)},
		tenant.ID, tenant.CreatedAt.UnixMilli(), tenant.UpdatedAt.UnixMilli(), hasMetadata, metadata).Err()
}

const (
	deleteTenantOK       = 0
	deleteTenantRetry    = 1
	deleteTenantNotFound = 4

	maxDeleteTenantAttempts = 10
)

// deleteTenantScript marks the tenant and its destinations deleted and drops the
// summary, provided the summary still lists exactly the destinations the caller
// saw; otherwise it changes nothing and the caller retries with a fresh list.
// KEYS: tenant hash, tenant summary hash, then one destination hash per ID.
// ARGV: deleted_at, expiry in seconds, number of destinations, then their IDs.
// Returns deleteTenantOK, deleteTenantRetry or deleteTenantNotFound.
var deleteTenantScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
	return 4
end
local n = tonumber(ARGV[3])
local want = {}
for i = 1, n do
	want[ARGV[3 + i]] = true
end
local ids = redis.call('HKEYS', KEYS[2])
if #ids ~= n then
	return 1
end
for _, id in ipairs(ids) do
	if not want[id] then
		return 1
	end
end
for i = 1, n do
	redis.call('HSET', KEYS[2 + i], 'deleted_at', ARGV[1])
	redis.call('HINCRBY', KEYS[2 + i], 'version', 1)
	redis.call('EXPIRE', KEYS[2 + i], ARGV[2])
end
redis.call('DEL', KEYS[2])
redis.call('HSET', KEYS[1], 'deleted_at', ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 0
`)

func (s *store) DeleteTenant(ctx context.Context, tenantID string) error {
	for attempt := 0; attempt < maxDeleteTenantAttempts; attempt++ {
		destinationIDs, err := s.redisClient.HKeys(ctx, s.redisTenantDestinationSummaryKey(tenantID)).Result()
		if err != nil {
			return err
		}
		switch code, err := s.deleteTenantOnce(ctx, tenantID, destinationIDs); {
		case err != nil:
			return err
		case code == deleteTenantNotFound:
			return driver.ErrTenantNotFound
		case code == deleteTenantOK:
			return nil
		}
	}
	return fmt.Errorf("tenant %q destinations kept changing while deleting", tenantID)
}

func (s *store) deleteTenantOnce(ctx context.Context, tenantID string, destinationIDs []string) (int, error) {
	keys := []string{s.redisTenantID(tenantID), s.redisTenantDestinationSummaryKey(tenantID)}
	args := []interface{}{time.Now().UnixMilli(), int(deletedRecordTTL.Seconds()), len(destinationIDs)}
	for _, id := range destinationIDs {
		keys = append(keys, s.redisDestinationID(id, tenantID))
		args = append(args, id)
	}
	return deleteTenantScript.Run(ctx, s.redisClient, keys, args...).Int()
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
	hasFilter := len(req.Type) > 0 || len(req.Topics) > 0
	if hasFilter {
		filter = &destinationFilter{
			Type:           req.Type,
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

const (
	writeDestinationOK           = 0
	writeDestinationDuplicate    = 1
	writeDestinationLimitReached = 2
	writeDestinationNoTenant     = 3
	writeDestinationNotFound     = 4
	writeDestinationConflict     = 5
)

const (
	writeModeUpsert = 0
	writeModeCreate = 1
	writeModeUpdate = 2
)

// writeDestinationScript writes a destination and its tenant summary entry in one
// step and bumps the destination's version. Depending on mode it first rejects:
//   - create: a missing or deleted tenant, a live destination with the same ID,
//     or a full tenant;
//   - update: a missing or deleted destination, or one whose version differs
//     from the expected one (a missing version counts as 0).
//
// KEYS: destination hash, tenant summary hash, tenant hash.
// ARGV: mode, limit, summary, id, expected version, number of fields to unset,
// those fields, then field/value pairs to set.
var writeDestinationScript = redis.NewScript(`
local mode, limit, summary, id, version, nunset = tonumber(ARGV[1]), tonumber(ARGV[2]), ARGV[3], ARGV[4], tonumber(ARGV[5]), tonumber(ARGV[6])
if mode == 1 then
	if redis.call('EXISTS', KEYS[3]) == 0 or redis.call('HEXISTS', KEYS[3], 'deleted_at') == 1 then
		return 3
	end
	if redis.call('EXISTS', KEYS[1]) == 1 and redis.call('HEXISTS', KEYS[1], 'deleted_at') == 0 then
		return 1
	end
	if redis.call('HLEN', KEYS[2]) >= limit then
		return 2
	end
elseif mode == 2 then
	if redis.call('EXISTS', KEYS[1]) == 0 or redis.call('HEXISTS', KEYS[1], 'deleted_at') == 1 then
		return 4
	end
	if tonumber(redis.call('HGET', KEYS[1], 'version') or 0) ~= version then
		return 5
	end
end
redis.call('PERSIST', KEYS[1])
for i = 1, nunset do
	redis.call('HDEL', KEYS[1], ARGV[6 + i])
end
for i = 7 + nunset, #ARGV, 2 do
	redis.call('HSET', KEYS[1], ARGV[i], ARGV[i + 1])
end
redis.call('HINCRBY', KEYS[1], 'version', 1)
redis.call('HSET', KEYS[2], id, summary)
return 0
`)

func (s *store) CreateDestination(ctx context.Context, destination models.Destination) error {
	return destinationWriteError(s.writeDestination(ctx, destination, writeModeCreate))
}

func (s *store) UpsertDestination(ctx context.Context, destination models.Destination) error {
	return destinationWriteError(s.writeDestination(ctx, destination, writeModeUpsert))
}

func (s *store) UpdateDestination(ctx context.Context, destination models.Destination) error {
	return destinationWriteError(s.writeDestination(ctx, destination, writeModeUpdate))
}

func destinationWriteError(code int, err error) error {
	if err != nil {
		return err
	}
	switch code {
	case writeDestinationNoTenant:
		return driver.ErrTenantNotFound
	case writeDestinationDuplicate:
		return driver.ErrDuplicateDestination
	case writeDestinationLimitReached:
		return driver.ErrMaxDestinationsPerTenantReached
	case writeDestinationNotFound:
		return driver.ErrDestinationNotFound
	case writeDestinationConflict:
		return driver.ErrDestinationConflict
	}
	return nil
}

func (s *store) writeDestination(ctx context.Context, destination models.Destination, mode int) (int, error) {
	fields, unset, err := s.prepareDestinationFields(&destination)
	if err != nil {
		return 0, err
	}

	args := []interface{}{mode, s.maxDestinationsPerTenant, newDestinationSummary(destination), destination.ID, destination.Version, len(unset)}
	for _, f := range unset {
		args = append(args, f)
	}
	args = append(args, fields...)

	keys := []string{
		s.redisDestinationID(destination.ID, destination.TenantID),
		s.redisTenantDestinationSummaryKey(destination.TenantID),
		s.redisTenantID(destination.TenantID),
	}
	return writeDestinationScript.Run(ctx, s.redisClient, keys, args...).Int()
}

func (s *store) prepareDestinationFields(destination *models.Destination) (fields []interface{}, unset []string, err error) {
	credentialsBytes, err := destination.Credentials.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid destination credentials: %w", err)
	}
	encryptedCredentials, err := s.cipher.encrypt(credentialsBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encrypt destination credentials: %w", err)
	}

	var encryptedDeliveryMetadata []byte
	if destination.DeliveryMetadata != nil {
		deliveryMetadataBytes, err := destination.DeliveryMetadata.MarshalBinary()
		if err != nil {
			return nil, nil, fmt.Errorf("invalid destination delivery_metadata: %w", err)
		}
		encryptedDeliveryMetadata, err = s.cipher.encrypt(deliveryMetadataBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encrypt destination delivery_metadata: %w", err)
		}
	}

	now := time.Now()
	if destination.CreatedAt.IsZero() {
		destination.CreatedAt = now
	}
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}

	unset = []string{"deleted_at"}
	fields = []interface{}{
		"id", destination.ID,
		"entity", "destination",
		"type", destination.Type,
		"topics", &destination.Topics,
		"config", &destination.Config,
		"credentials", encryptedCredentials,
		"created_at", destination.CreatedAt.UnixMilli(),
		"updated_at", destination.UpdatedAt.UnixMilli(),
	}

	if destination.DisabledAt != nil {
		fields = append(fields, "disabled_at", destination.DisabledAt.UnixMilli())
	} else {
		unset = append(unset, "disabled_at")
	}

	if destination.DeliveryMetadata != nil {
		fields = append(fields, "delivery_metadata", encryptedDeliveryMetadata)
	} else {
		unset = append(unset, "delivery_metadata")
	}

	if destination.Metadata != nil {
		fields = append(fields, "metadata", &destination.Metadata)
	} else {
		unset = append(unset, "metadata")
	}

	if len(destination.Filter) > 0 {
		fields = append(fields, "filter", &destination.Filter)
	} else {
		unset = append(unset, "filter")
	}

	return fields, unset, nil
}

const (
	deleteDestinationOK       = 0
	deleteDestinationNotFound = 4
)

// deleteDestinationScript marks a destination deleted, bumps its version and
// drops it from the tenant summary.
// KEYS: destination hash, tenant summary hash.
// ARGV: destination ID, deleted_at, expiry in seconds.
// Returns deleteDestinationOK, or deleteDestinationNotFound when it does not exist.
var deleteDestinationScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
	return 4
end
redis.call('HDEL', KEYS[2], ARGV[1])
redis.call('HSET', KEYS[1], 'deleted_at', ARGV[2])
redis.call('HINCRBY', KEYS[1], 'version', 1)
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 0
`)

func (s *store) DeleteDestination(ctx context.Context, tenantID, destinationID string) error {
	keys := []string{
		s.redisDestinationID(destinationID, tenantID),
		s.redisTenantDestinationSummaryKey(tenantID),
	}
	code, err := deleteDestinationScript.Run(ctx, s.redisClient, keys,
		destinationID, time.Now().UnixMilli(), int(deletedRecordTTL.Seconds())).Int()
	if err != nil {
		return err
	}
	if code == deleteDestinationNotFound {
		return driver.ErrDestinationNotFound
	}
	return nil
}

func (s *store) MatchEvent(ctx context.Context, event models.Event, allowWildcards bool) ([]string, error) {
	destinationSummaryList, err := s.listDestinationSummaryByTenant(ctx, event.TenantID, nil)
	if err != nil {
		return nil, err
	}

	var matched []string

	for _, ds := range destinationSummaryList {
		if ds.Disabled {
			continue
		}
		if event.Topic != "" && !ds.Topics.MatchTopic(event.Topic, allowWildcards) {
			continue
		}
		if !models.MatchFilter(ds.Filter, event) {
			continue
		}
		matched = append(matched, ds.ID)
	}

	return matched, nil
}
