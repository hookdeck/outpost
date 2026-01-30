// Package redistenantstore provides a Redis-backed implementation of driver.TenantStore.
package redistenantstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

const defaultMaxDestinationsPerTenant = 20

const (
	defaultListTenantLimit = 20
	maxListTenantLimit     = 100
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

	destinationSummaryList, err := parseListDestinationSummaryByTenantCmd(destinationListCmd, driver.ListDestinationByTenantOpts{})
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

	destinationIDs, err := s.redisClient.HKeys(ctx, s.redisTenantDestinationSummaryKey(tenantID)).Result()
	if err != nil {
		return err
	}

	_, err = s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		nowUnixMilli := time.Now().UnixMilli()

		for _, destinationID := range destinationIDs {
			destKey := s.redisDestinationID(destinationID, tenantID)
			pipe.HSet(ctx, destKey, "deleted_at", nowUnixMilli)
			pipe.Expire(ctx, destKey, 7*24*time.Hour)
		}

		pipe.Del(ctx, s.redisTenantDestinationSummaryKey(tenantID))
		pipe.HSet(ctx, s.redisTenantID(tenantID), "deleted_at", nowUnixMilli)
		pipe.Expire(ctx, s.redisTenantID(tenantID), 7*24*time.Hour)

		return nil
	})

	return err
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
			destinationSummaryList, err := parseListDestinationSummaryByTenantCmd(cmds[i], driver.ListDestinationByTenantOpts{})
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
		cursorTimestamp, err := strconv.ParseInt(q.CursorPos, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid timestamp", driver.ErrInvalidCursor)
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

func (s *store) listDestinationSummaryByTenant(ctx context.Context, tenantID string, opts driver.ListDestinationByTenantOpts) ([]destinationSummary, error) {
	return parseListDestinationSummaryByTenantCmd(s.redisClient.HGetAll(ctx, s.redisTenantDestinationSummaryKey(tenantID)), opts)
}

func (s *store) ListDestinationByTenant(ctx context.Context, tenantID string, options ...driver.ListDestinationByTenantOpts) ([]models.Destination, error) {
	var opts driver.ListDestinationByTenantOpts
	if len(options) > 0 {
		opts = options[0]
	}

	destinationSummaryList, err := s.listDestinationSummaryByTenant(ctx, tenantID, opts)
	if err != nil {
		return nil, err
	}

	pipe := s.redisClient.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(destinationSummaryList))
	for i, destinationSummary := range destinationSummaryList {
		cmds[i] = pipe.HGetAll(ctx, s.redisDestinationID(destinationSummary.ID, tenantID))
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	destinations := make([]models.Destination, len(destinationSummaryList))
	for i, cmd := range cmds {
		destination, err := parseDestinationHash(cmd, tenantID, s.cipher)
		if err != nil {
			return []models.Destination{}, err
		}
		destinations[i] = *destination
	}

	sort.Slice(destinations, func(i, j int) bool {
		return destinations[i].CreatedAt.Before(destinations[j].CreatedAt)
	})

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

func (s *store) CreateDestination(ctx context.Context, destination models.Destination) error {
	key := s.redisDestinationID(destination.ID, destination.TenantID)
	if fields, err := s.redisClient.HGetAll(ctx, key).Result(); err != nil {
		return err
	} else if len(fields) > 0 {
		if _, isDeleted := fields["deleted_at"]; !isDeleted {
			return driver.ErrDuplicateDestination
		}
	}

	count, err := s.redisClient.HLen(ctx, s.redisTenantDestinationSummaryKey(destination.TenantID)).Result()
	if err != nil {
		return err
	}
	if count >= int64(s.maxDestinationsPerTenant) {
		return driver.ErrMaxDestinationsPerTenantReached
	}

	return s.UpsertDestination(ctx, destination)
}

func (s *store) UpsertDestination(ctx context.Context, destination models.Destination) error {
	key := s.redisDestinationID(destination.ID, destination.TenantID)

	credentialsBytes, err := destination.Credentials.MarshalBinary()
	if err != nil {
		return fmt.Errorf("invalid destination credentials: %w", err)
	}
	encryptedCredentials, err := s.cipher.encrypt(credentialsBytes)
	if err != nil {
		return fmt.Errorf("failed to encrypt destination credentials: %w", err)
	}

	var encryptedDeliveryMetadata []byte
	if destination.DeliveryMetadata != nil {
		deliveryMetadataBytes, err := destination.DeliveryMetadata.MarshalBinary()
		if err != nil {
			return fmt.Errorf("invalid destination delivery_metadata: %w", err)
		}
		encryptedDeliveryMetadata, err = s.cipher.encrypt(deliveryMetadataBytes)
		if err != nil {
			return fmt.Errorf("failed to encrypt destination delivery_metadata: %w", err)
		}
	}

	now := time.Now()
	if destination.CreatedAt.IsZero() {
		destination.CreatedAt = now
	}
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}

	summaryKey := s.redisTenantDestinationSummaryKey(destination.TenantID)

	_, err = s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Persist(ctx, key)
		pipe.HDel(ctx, key, "deleted_at")

		pipe.HSet(ctx, key, "id", destination.ID)
		pipe.HSet(ctx, key, "entity", "destination")
		pipe.HSet(ctx, key, "type", destination.Type)
		pipe.HSet(ctx, key, "topics", &destination.Topics)
		pipe.HSet(ctx, key, "config", &destination.Config)
		pipe.HSet(ctx, key, "credentials", encryptedCredentials)
		pipe.HSet(ctx, key, "created_at", destination.CreatedAt.UnixMilli())
		pipe.HSet(ctx, key, "updated_at", destination.UpdatedAt.UnixMilli())

		if destination.DisabledAt != nil {
			pipe.HSet(ctx, key, "disabled_at", destination.DisabledAt.UnixMilli())
		} else {
			pipe.HDel(ctx, key, "disabled_at")
		}

		if destination.DeliveryMetadata != nil {
			pipe.HSet(ctx, key, "delivery_metadata", encryptedDeliveryMetadata)
		} else {
			pipe.HDel(ctx, key, "delivery_metadata")
		}

		if destination.Metadata != nil {
			pipe.HSet(ctx, key, "metadata", &destination.Metadata)
		} else {
			pipe.HDel(ctx, key, "metadata")
		}

		if len(destination.Filter) > 0 {
			pipe.HSet(ctx, key, "filter", &destination.Filter)
		} else {
			pipe.HDel(ctx, key, "filter")
		}

		pipe.HSet(ctx, summaryKey, destination.ID, newDestinationSummary(destination))
		return nil
	})

	return err
}

func (s *store) DeleteDestination(ctx context.Context, tenantID, destinationID string) error {
	key := s.redisDestinationID(destinationID, tenantID)
	summaryKey := s.redisTenantDestinationSummaryKey(tenantID)

	if exists, err := s.redisClient.Exists(ctx, key).Result(); err != nil {
		return err
	} else if exists == 0 {
		return driver.ErrDestinationNotFound
	}

	_, err := s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		nowUnixMilli := time.Now().UnixMilli()

		pipe.HDel(ctx, summaryKey, destinationID)
		pipe.HSet(ctx, key, "deleted_at", nowUnixMilli)
		pipe.Expire(ctx, key, 7*24*time.Hour)

		return nil
	})

	return err
}

func (s *store) MatchEvent(ctx context.Context, event models.Event) ([]string, error) {
	destinationSummaryList, err := s.listDestinationSummaryByTenant(ctx, event.TenantID, driver.ListDestinationByTenantOpts{})
	if err != nil {
		return nil, err
	}

	var matched []string

	for _, ds := range destinationSummaryList {
		if ds.Disabled {
			continue
		}
		if event.Topic != "" && !ds.Topics.MatchTopic(event.Topic) {
			continue
		}
		if !models.MatchFilter(ds.Filter, event) {
			continue
		}
		matched = append(matched, ds.ID)
	}

	return matched, nil
}
