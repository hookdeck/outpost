package models

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

const defaultMaxDestinationsPerTenant = 20

type EntityStore interface {
	Init(ctx context.Context) error
	RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error)
	UpsertTenant(ctx context.Context, tenant Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenant(ctx context.Context, req ListTenantRequest) (*ListTenantResponse, error)
	ListDestinationByTenant(ctx context.Context, tenantID string, options ...ListDestinationByTenantOpts) ([]Destination, error)
	RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*Destination, error)
	CreateDestination(ctx context.Context, destination Destination) error
	UpsertDestination(ctx context.Context, destination Destination) error
	DeleteDestination(ctx context.Context, tenantID, destinationID string) error
	MatchEvent(ctx context.Context, event Event) ([]DestinationSummary, error)
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
	Order string // Sort order: "asc" or "desc" (default: "desc")
}

// ListTenantResponse contains the paginated list of tenants.
type ListTenantResponse struct {
	Data  []TenantListItem `json:"data"`
	Next  string           `json:"next"`
	Prev  string           `json:"prev"`
	Count int              `json:"count"`
}

type entityStoreImpl struct {
	redisClient              redis.Cmdable
	cipher                   Cipher
	availableTopics          []string
	maxDestinationsPerTenant int
	deploymentID             string
	listTenantSupported      bool
}

// doCmd executes an arbitrary Redis command using the Do method.
// Returns an error if the client doesn't support Do (e.g., mock clients).
func (s *entityStoreImpl) doCmd(ctx context.Context, args ...interface{}) *redis.Cmd {
	if dc, ok := s.redisClient.(redis.DoContext); ok {
		return dc.Do(ctx, args...)
	}
	// Return an error cmd if Do is not supported
	cmd := &redis.Cmd{}
	cmd.SetErr(errors.New("redis client does not support Do command"))
	return cmd
}

// deploymentPrefix returns the deployment prefix for Redis keys
func (s *entityStoreImpl) deploymentPrefix() string {
	if s.deploymentID == "" {
		return ""
	}
	return fmt.Sprintf("%s:", s.deploymentID)
}

// New cluster-compatible key formats with hash tags
func (s *entityStoreImpl) redisTenantID(tenantID string) string {
	return fmt.Sprintf("%stenant:{%s}:tenant", s.deploymentPrefix(), tenantID)
}

func (s *entityStoreImpl) redisTenantDestinationSummaryKey(tenantID string) string {
	return fmt.Sprintf("%stenant:{%s}:destinations", s.deploymentPrefix(), tenantID)
}

func (s *entityStoreImpl) redisDestinationID(destinationID, tenantID string) string {
	return fmt.Sprintf("%stenant:{%s}:destination:%s", s.deploymentPrefix(), tenantID, destinationID)
}

var _ EntityStore = (*entityStoreImpl)(nil)

type EntityStoreOption func(*entityStoreImpl)

func WithCipher(cipher Cipher) EntityStoreOption {
	return func(s *entityStoreImpl) {
		s.cipher = cipher
	}
}

func WithAvailableTopics(topics []string) EntityStoreOption {
	return func(s *entityStoreImpl) {
		s.availableTopics = topics
	}
}

func WithMaxDestinationsPerTenant(maxDestinationsPerTenant int) EntityStoreOption {
	return func(s *entityStoreImpl) {
		s.maxDestinationsPerTenant = maxDestinationsPerTenant
	}
}

func WithDeploymentID(deploymentID string) EntityStoreOption {
	return func(s *entityStoreImpl) {
		s.deploymentID = deploymentID
	}
}

func NewEntityStore(redisClient redis.Cmdable, opts ...EntityStoreOption) EntityStore {
	store := &entityStoreImpl{
		redisClient:              redisClient,
		cipher:                   NewAESCipher(""),
		availableTopics:          []string{},
		maxDestinationsPerTenant: defaultMaxDestinationsPerTenant,
	}

	for _, opt := range opts {
		opt(store)
	}

	return store
}

// tenantIndexName returns the RediSearch index name for tenants.
func (s *entityStoreImpl) tenantIndexName() string {
	return s.deploymentPrefix() + "tenant_idx"
}

// tenantKeyPrefix returns the key prefix for tenant hashes (for RediSearch).
func (s *entityStoreImpl) tenantKeyPrefix() string {
	return s.deploymentPrefix() + "tenant:"
}

// Init initializes the entity store, probing for RediSearch support.
// If RediSearch is available, it creates the tenant index.
// If RediSearch is not available, ListTenant will return ErrListTenantNotSupported.
func (s *entityStoreImpl) Init(ctx context.Context) error {
	// Probe for RediSearch support using FT._LIST
	_, err := s.doCmd(ctx, "FT._LIST").Result()
	if err != nil {
		// RediSearch not available - this is not an error, just disable the feature
		s.listTenantSupported = false
		return nil
	}

	// Try to create the tenant index, gracefully disable if it fails
	// TODO: consider logging this error, but we don't have a logger in this context
	if err := s.ensureTenantIndex(ctx); err != nil {
		s.listTenantSupported = false
		return nil
	}

	s.listTenantSupported = true
	return nil
}

// ensureTenantIndex creates the RediSearch index for tenants if it doesn't exist.
func (s *entityStoreImpl) ensureTenantIndex(ctx context.Context) error {
	indexName := s.tenantIndexName()

	// Check if index already exists using FT.INFO
	_, err := s.doCmd(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		return nil
	}

	// Create the index
	// FT.CREATE index ON HASH PREFIX 1 prefix SCHEMA id TAG created_at NUMERIC SORTABLE deleted_at NUMERIC
	// Note: created_at and deleted_at are stored as Unix timestamps
	// deleted_at is indexed so we can filter out deleted tenants in FT.SEARCH queries
	prefix := s.tenantKeyPrefix()
	_, err = s.doCmd(ctx, "FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"SCHEMA",
		"id", "TAG",
		"created_at", "NUMERIC", "SORTABLE",
		"deleted_at", "NUMERIC",
	).Result()

	if err != nil {
		return fmt.Errorf("failed to create tenant index: %w", err)
	}

	return nil
}

func (s *entityStoreImpl) RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error) {
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
	tenant := &Tenant{}
	if err := tenant.parseRedisHash(tenantHash); err != nil {
		return nil, err
	}

	destinationSummaryList, err := s.parseListDestinationSummaryByTenantCmd(destinationListCmd, ListDestinationByTenantOpts{})
	if err != nil {
		return nil, err
	}
	tenant.DestinationsCount = len(destinationSummaryList)
	tenant.Topics = s.parseTenantTopics(destinationSummaryList)

	return tenant, err
}

func (s *entityStoreImpl) UpsertTenant(ctx context.Context, tenant Tenant) error {
	key := s.redisTenantID(tenant.ID)

	// For cluster compatibility, execute commands individually instead of in a transaction
	// Support overriding deleted resources
	if err := s.redisClient.Persist(ctx, key).Err(); err != nil && err != redis.Nil {
		return err
	}

	if err := s.redisClient.HDel(ctx, key, "deleted_at").Err(); err != nil && err != redis.Nil {
		return err
	}

	// Auto-generate timestamps if not provided
	now := time.Now()
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	if tenant.UpdatedAt.IsZero() {
		tenant.UpdatedAt = now
	}

	// Set tenant data - store timestamps as Unix milliseconds for timezone-agnostic sorting
	if err := s.redisClient.HSet(ctx, key,
		"id", tenant.ID,
		"created_at", tenant.CreatedAt.UnixMilli(),
		"updated_at", tenant.UpdatedAt.UnixMilli(),
	).Err(); err != nil {
		return err
	}

	// Store metadata if present, otherwise delete field
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

func (s *entityStoreImpl) DeleteTenant(ctx context.Context, tenantID string) error {
	if exists, err := s.redisClient.Exists(ctx, s.redisTenantID(tenantID)).Result(); err != nil {
		return err
	} else if exists == 0 {
		return ErrTenantNotFound
	}

	// Get destination IDs before transaction
	destinationIDs, err := s.redisClient.HKeys(ctx, s.redisTenantDestinationSummaryKey(tenantID)).Result()
	if err != nil {
		return err
	}

	// All operations on same tenant - cluster compatible transaction
	_, err = s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		nowUnixMilli := time.Now().UnixMilli()

		// Delete all destinations atomically
		for _, destinationID := range destinationIDs {
			destKey := s.redisDestinationID(destinationID, tenantID)
			pipe.HSet(ctx, destKey, "deleted_at", nowUnixMilli)
			pipe.Expire(ctx, destKey, 7*24*time.Hour)
		}

		// Delete summary and mark tenant as deleted
		// Store deleted_at as Unix milliseconds so it can be filtered in FT.SEARCH
		pipe.Del(ctx, s.redisTenantDestinationSummaryKey(tenantID))
		pipe.HSet(ctx, s.redisTenantID(tenantID), "deleted_at", nowUnixMilli)
		pipe.Expire(ctx, s.redisTenantID(tenantID), 7*24*time.Hour)

		return nil
	})

	return err
}

const (
	defaultListTenantLimit = 20
	maxListTenantLimit     = 100
)

// ListTenant returns a paginated list of tenants using RediSearch.
func (s *entityStoreImpl) ListTenant(ctx context.Context, req ListTenantRequest) (*ListTenantResponse, error) {
	if !s.listTenantSupported {
		return nil, ErrListTenantNotSupported
	}

	// Validate: cannot specify both Next and Prev
	if req.Next != "" && req.Prev != "" {
		return nil, ErrConflictingCursors
	}

	// Apply defaults and validate limit
	limit := req.Limit
	if limit <= 0 {
		limit = defaultListTenantLimit
	}
	if limit > maxListTenantLimit {
		limit = maxListTenantLimit
	}

	// Validate and apply order
	order := req.Order
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return nil, ErrInvalidOrder
	}

	// Determine sort direction
	sortDir := "DESC"
	if order == "asc" {
		sortDir = "ASC"
	}

	// Parse cursor timestamp (keyset pagination)
	// Cursor contains the timestamp boundary for the next/prev page
	var cursorTimestamp int64
	isNextCursor := false
	isPrevCursor := false
	if req.Next != "" {
		var err error
		cursorTimestamp, err = decodeCursor(req.Next)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
		isNextCursor = true
	} else if req.Prev != "" {
		var err error
		cursorTimestamp, err = decodeCursor(req.Prev)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
		isPrevCursor = true
	}

	// Build FT.SEARCH query with timestamp filter (keyset pagination)
	// This avoids offset-based pagination which has issues on Dragonfly
	// We use inclusive ranges with cursor-1/cursor+1 for better compatibility
	// The -@deleted_at:[1 +inf] filter excludes deleted tenants from results
	// (documents without deleted_at field won't match the positive query, so negation includes them)
	notDeleted := "-@deleted_at:[1 +inf]"
	var query string
	querySortDir := sortDir

	if !isNextCursor && !isPrevCursor {
		// First page - only filter out deleted
		query = notDeleted
	} else if isNextCursor {
		// Next cursor: get older (DESC) or newer (ASC) records
		if order == "desc" {
			// DESC: get records with created_at < cursor (older)
			query = fmt.Sprintf("(@created_at:[0 %d]) %s", cursorTimestamp-1, notDeleted)
		} else {
			// ASC: get records with created_at > cursor (newer)
			query = fmt.Sprintf("(@created_at:[%d +inf]) %s", cursorTimestamp+1, notDeleted)
		}
	} else {
		// Prev cursor: go back to previous page
		// We reverse the sort direction to get items closest to the cursor,
		// then reverse the results to maintain the user's expected order.
		if order == "desc" {
			// DESC going back: get records with created_at > cursor (newer), sorted ASC
			query = fmt.Sprintf("(@created_at:[%d +inf]) %s", cursorTimestamp+1, notDeleted)
			querySortDir = "ASC"
		} else {
			// ASC going back: get records with created_at < cursor (older), sorted DESC
			query = fmt.Sprintf("(@created_at:[0 %d]) %s", cursorTimestamp-1, notDeleted)
			querySortDir = "DESC"
		}
	}

	// Execute FT.SEARCH query with LIMIT 0 count (no offset)
	result, err := s.doCmd(ctx, "FT.SEARCH", s.tenantIndexName(),
		query,
		"SORTBY", "created_at", querySortDir,
		"LIMIT", 0, limit,
	).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to search tenants: %w", err)
	}

	// Parse FT.SEARCH result
	tenants, _, err := s.parseSearchResult(ctx, result)
	if err != nil {
		return nil, err
	}

	// For prev cursor, we fetched in reverse order to get items closest to cursor.
	// Now reverse the results to restore the user's expected order.
	if isPrevCursor {
		for i, j := 0, len(tenants)-1; i < j; i, j = i+1, j-1 {
			tenants[i], tenants[j] = tenants[j], tenants[i]
		}
	}

	// Convert to TenantListItem (excludes computed fields like destinations_count, topics)
	items := make([]TenantListItem, len(tenants))
	for i, t := range tenants {
		items[i] = t.ToListItem()
	}

	// Get total count of all tenants (excluding deleted) - cheap query with LIMIT 0 0
	var totalCount int
	countResult, err := s.doCmd(ctx, "FT.SEARCH", s.tenantIndexName(),
		notDeleted,
		"LIMIT", 0, 0,
	).Result()
	if err == nil {
		_, totalCount, _ = s.parseSearchResult(ctx, countResult)
	}

	// Build response with cursors
	resp := &ListTenantResponse{
		Data:  items,
		Count: totalCount,
	}

	// Always set next/prev cursors based on first/last item timestamps.
	// Client discovers "no more pages" when they use the cursor and get empty result.
	// This avoids expensive probe queries.
	if len(tenants) > 0 {
		lastTenant := tenants[len(tenants)-1]
		firstTenant := tenants[0]

		// Next cursor: points to last item (for continuing in same direction)
		resp.Next = encodeCursor(lastTenant.CreatedAt.UnixMilli())

		// Prev cursor: points to first item (for going back)
		// Only set if we navigated here via a cursor (not on first page)
		if isNextCursor || isPrevCursor {
			resp.Prev = encodeCursor(firstTenant.CreatedAt.UnixMilli())
		}
	}

	return resp, nil
}

// parseSearchResult parses the FT.SEARCH result into a list of tenants.
// Supports both RESP2 (array) and RESP3 (map) formats.
func (s *entityStoreImpl) parseSearchResult(ctx context.Context, result interface{}) ([]Tenant, int, error) {
	// RESP3 format (go-redis v9): map with "total_results", "results", etc.
	if resultMap, ok := result.(map[interface{}]interface{}); ok {
		return s.parseResp3SearchResult(resultMap)
	}

	// RESP2 format: [total_count, doc1_key, doc1_fields, doc2_key, doc2_fields, ...]
	arr, ok := result.([]interface{})
	if !ok || len(arr) == 0 {
		return []Tenant{}, 0, nil
	}

	totalCount, ok := arr[0].(int64)
	if !ok {
		return nil, 0, fmt.Errorf("invalid search result: expected total count")
	}

	tenants := make([]Tenant, 0, (len(arr)-1)/2)

	// Iterate through results (skip first element which is count)
	for i := 1; i < len(arr); i += 2 {
		if i+1 >= len(arr) {
			break
		}

		// arr[i] is the document key, arr[i+1] is the fields
		// Fields can be either:
		// - []interface{} array (Redis Stack RESP2): [field1, val1, field2, val2, ...]
		// - map[interface{}]interface{} (Dragonfly): {field1: val1, field2: val2, ...}
		hash := make(map[string]string)

		switch fields := arr[i+1].(type) {
		case []interface{}:
			// Redis Stack RESP2 format: array of alternating key/value
			for j := 0; j < len(fields)-1; j += 2 {
				key, keyOk := fields[j].(string)
				val, valOk := fields[j+1].(string)
				if keyOk && valOk {
					hash[key] = val
				}
			}
		case map[interface{}]interface{}:
			// Dragonfly format: map of key/value pairs
			for k, v := range fields {
				key, keyOk := k.(string)
				if !keyOk {
					continue
				}
				switch val := v.(type) {
				case string:
					hash[key] = val
				case float64:
					hash[key] = fmt.Sprintf("%.0f", val)
				case int64:
					hash[key] = fmt.Sprintf("%d", val)
				}
			}
		default:
			continue
		}

		// Skip deleted tenants
		if _, deleted := hash["deleted_at"]; deleted {
			continue
		}

		tenant := &Tenant{}
		if err := tenant.parseRedisHash(hash); err != nil {
			continue // Skip invalid entries
		}

		tenants = append(tenants, *tenant)
	}

	return tenants, int(totalCount), nil
}

// parseResp3SearchResult parses the RESP3 map format from FT.SEARCH.
func (s *entityStoreImpl) parseResp3SearchResult(resultMap map[interface{}]interface{}) ([]Tenant, int, error) {
	totalCount := 0
	if tc, ok := resultMap["total_results"].(int64); ok {
		totalCount = int(tc)
	}

	results, ok := resultMap["results"].([]interface{})
	if !ok {
		return []Tenant{}, totalCount, nil
	}

	tenants := make([]Tenant, 0, len(results))

	for _, r := range results {
		docMap, ok := r.(map[interface{}]interface{})
		if !ok {
			continue
		}

		// Get extra_attributes which contains the hash fields
		extraAttrs, ok := docMap["extra_attributes"].(map[interface{}]interface{})
		if !ok {
			continue
		}

		// Convert to string map
		hash := make(map[string]string)
		for k, v := range extraAttrs {
			if keyStr, ok := k.(string); ok {
				if valStr, ok := v.(string); ok {
					hash[keyStr] = valStr
				}
			}
		}

		// Skip deleted tenants
		if _, deleted := hash["deleted_at"]; deleted {
			continue
		}

		tenant := &Tenant{}
		if err := tenant.parseRedisHash(hash); err != nil {
			continue // Skip invalid entries
		}

		tenants = append(tenants, *tenant)
	}

	return tenants, totalCount, nil
}

const cursorVersion = "tntv01"

// encodeCursor encodes a timestamp as a versioned base62 cursor.
// Internal format: tntv01:<timestamp>, then base62 encoded.
func encodeCursor(timestamp int64) string {
	raw := fmt.Sprintf("%s:%d", cursorVersion, timestamp)
	return base62Encode(raw)
}

// decodeCursor decodes a base62 cursor into a timestamp.
// Expects base62 encoded string containing: tntv01:<timestamp>
func decodeCursor(cursor string) (int64, error) {
	raw, err := base62Decode(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	// Expected format: tntv02:<timestamp>
	if len(raw) <= len(cursorVersion)+1 {
		return 0, fmt.Errorf("invalid cursor format")
	}

	version := raw[:len(cursorVersion)]
	if version != cursorVersion {
		return 0, fmt.Errorf("unsupported cursor version: %s", version)
	}

	if raw[len(cursorVersion)] != ':' {
		return 0, fmt.Errorf("invalid cursor format")
	}

	var timestamp int64
	_, err = fmt.Sscanf(raw[len(cursorVersion)+1:], "%d", &timestamp)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor timestamp")
	}

	if timestamp < 0 {
		return 0, fmt.Errorf("invalid timestamp")
	}
	return timestamp, nil
}

// base62Encode encodes a string to base62.
func base62Encode(s string) string {
	num := new(big.Int)
	num.SetBytes([]byte(s))
	return num.Text(62)
}

// base62Decode decodes a base62 string.
func base62Decode(s string) (string, error) {
	num := new(big.Int)
	num, ok := num.SetString(s, 62)
	if !ok {
		return "", fmt.Errorf("invalid base62 string")
	}
	return string(num.Bytes()), nil
}

func (s *entityStoreImpl) listDestinationSummaryByTenant(ctx context.Context, tenantID string, opts ListDestinationByTenantOpts) ([]DestinationSummary, error) {
	return s.parseListDestinationSummaryByTenantCmd(s.redisClient.HGetAll(ctx, s.redisTenantDestinationSummaryKey(tenantID)), opts)
}

func (s *entityStoreImpl) parseListDestinationSummaryByTenantCmd(cmd *redis.MapStringStringCmd, opts ListDestinationByTenantOpts) ([]DestinationSummary, error) {
	destinationSummaryListHash, err := cmd.Result()
	if err != nil {
		if err == redis.Nil {
			return []DestinationSummary{}, nil
		}
		return nil, err
	}
	destinationSummaryList := make([]DestinationSummary, 0, len(destinationSummaryListHash))
	for _, destinationSummaryStr := range destinationSummaryListHash {
		destinationSummary := DestinationSummary{}
		if err := destinationSummary.UnmarshalBinary([]byte(destinationSummaryStr)); err != nil {
			return nil, err
		}
		included := true
		if opts.Filter != nil {
			included = opts.Filter.match(destinationSummary)
		}
		if included {
			destinationSummaryList = append(destinationSummaryList, destinationSummary)
		}
	}
	return destinationSummaryList, nil
}

func (s *entityStoreImpl) ListDestinationByTenant(ctx context.Context, tenantID string, options ...ListDestinationByTenantOpts) ([]Destination, error) {
	var opts ListDestinationByTenantOpts
	if len(options) > 0 {
		opts = options[0]
	} else {
		opts = ListDestinationByTenantOpts{}
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

	destinations := make([]Destination, len(destinationSummaryList))
	for i, cmd := range cmds {
		destination := &Destination{TenantID: tenantID}
		err = destination.parseRedisHash(cmd, s.cipher)
		if err != nil {
			return []Destination{}, err
		}
		destinations[i] = *destination
	}

	sort.Slice(destinations, func(i, j int) bool {
		return destinations[i].CreatedAt.Before(destinations[j].CreatedAt)
	})

	return destinations, nil
}

func (s *entityStoreImpl) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*Destination, error) {
	cmd := s.redisClient.HGetAll(ctx, s.redisDestinationID(destinationID, tenantID))
	destination := &Destination{TenantID: tenantID}
	if err := destination.parseRedisHash(cmd, s.cipher); err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return destination, nil
}

func (s *entityStoreImpl) CreateDestination(ctx context.Context, destination Destination) error {
	key := s.redisDestinationID(destination.ID, destination.TenantID)
	// Check if destination exists
	if fields, err := s.redisClient.HGetAll(ctx, key).Result(); err != nil {
		return err
	} else if len(fields) > 0 {
		if _, isDeleted := fields["deleted_at"]; !isDeleted {
			return ErrDuplicateDestination
		}
	}

	// Check if tenant has reached max destinations by counting entries in the summary hash
	count, err := s.redisClient.HLen(ctx, s.redisTenantDestinationSummaryKey(destination.TenantID)).Result()
	if err != nil {
		return err
	}
	if count >= int64(s.maxDestinationsPerTenant) {
		return ErrMaxDestinationsPerTenantReached
	}

	return s.UpsertDestination(ctx, destination)
}

func (s *entityStoreImpl) UpsertDestination(ctx context.Context, destination Destination) error {
	key := s.redisDestinationID(destination.ID, destination.TenantID)

	// Pre-marshal and encrypt credentials and delivery_metadata BEFORE starting Redis transaction
	// This isolates marshaling failures from Redis transaction failures
	credentialsBytes, err := destination.Credentials.MarshalBinary()
	if err != nil {
		return fmt.Errorf("invalid destination credentials: %w", err)
	}
	encryptedCredentials, err := s.cipher.Encrypt(credentialsBytes)
	if err != nil {
		return fmt.Errorf("failed to encrypt destination credentials: %w", err)
	}

	// Encrypt delivery_metadata if present (contains sensitive data like auth tokens)
	var encryptedDeliveryMetadata []byte
	if destination.DeliveryMetadata != nil {
		deliveryMetadataBytes, err := destination.DeliveryMetadata.MarshalBinary()
		if err != nil {
			return fmt.Errorf("invalid destination delivery_metadata: %w", err)
		}
		encryptedDeliveryMetadata, err = s.cipher.Encrypt(deliveryMetadataBytes)
		if err != nil {
			return fmt.Errorf("failed to encrypt destination delivery_metadata: %w", err)
		}
	}

	// Auto-generate timestamps if not provided
	now := time.Now()
	if destination.CreatedAt.IsZero() {
		destination.CreatedAt = now
	}
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}

	// All keys use same tenant prefix - cluster compatible transaction
	summaryKey := s.redisTenantDestinationSummaryKey(destination.TenantID)

	_, err = s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// Clear deletion markers
		pipe.Persist(ctx, key)
		pipe.HDel(ctx, key, "deleted_at")

		// Set all destination fields atomically
		// Store timestamps as Unix milliseconds for timezone-agnostic handling
		pipe.HSet(ctx, key, "id", destination.ID)
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

		// Store encrypted delivery_metadata if present
		if destination.DeliveryMetadata != nil {
			pipe.HSet(ctx, key, "delivery_metadata", encryptedDeliveryMetadata)
		} else {
			pipe.HDel(ctx, key, "delivery_metadata")
		}

		// Store metadata if present
		if destination.Metadata != nil {
			pipe.HSet(ctx, key, "metadata", &destination.Metadata)
		} else {
			pipe.HDel(ctx, key, "metadata")
		}

		// Store filter if present
		if destination.Filter != nil && len(destination.Filter) > 0 {
			pipe.HSet(ctx, key, "filter", &destination.Filter)
		} else {
			pipe.HDel(ctx, key, "filter")
		}

		// Update summary atomically
		pipe.HSet(ctx, summaryKey, destination.ID, destination.ToSummary())
		return nil
	})

	return err
}

func (s *entityStoreImpl) DeleteDestination(ctx context.Context, tenantID, destinationID string) error {
	key := s.redisDestinationID(destinationID, tenantID)
	summaryKey := s.redisTenantDestinationSummaryKey(tenantID)

	// Check if destination exists
	if exists, err := s.redisClient.Exists(ctx, key).Result(); err != nil {
		return err
	} else if exists == 0 {
		return ErrDestinationNotFound
	}

	// Atomic deletion with same-tenant keys
	_, err := s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		nowUnixMilli := time.Now().UnixMilli()

		// Remove from summary and mark as deleted atomically
		pipe.HDel(ctx, summaryKey, destinationID)
		pipe.HSet(ctx, key, "deleted_at", nowUnixMilli)
		pipe.Expire(ctx, key, 7*24*time.Hour)

		return nil
	})

	return err
}

func (s *entityStoreImpl) MatchEvent(ctx context.Context, event Event) ([]DestinationSummary, error) {
	destinationSummaryList, err := s.listDestinationSummaryByTenant(ctx, event.TenantID, ListDestinationByTenantOpts{})
	if err != nil {
		return nil, err
	}

	matchedDestinationSummaryList := []DestinationSummary{}

	for _, destinationSummary := range destinationSummaryList {
		if destinationSummary.Disabled {
			continue
		}
		// Match by topic first (if topic is provided)
		if event.Topic != "" && !destinationSummary.Topics.MatchTopic(event.Topic) {
			continue
		}
		// Then apply filter (if filter is set)
		if !destinationSummary.MatchFilter(event) {
			continue
		}
		matchedDestinationSummaryList = append(matchedDestinationSummaryList, destinationSummary)
	}

	return matchedDestinationSummaryList, nil
}

func (s *entityStoreImpl) parseTenantTopics(destinationSummaryList []DestinationSummary) []string {
	all := false
	topicsSet := make(map[string]struct{})
	for _, destination := range destinationSummaryList {
		for _, topic := range destination.Topics {
			if topic == "*" {
				all = true
				break
			}
			topicsSet[topic] = struct{}{}
		}
	}

	if all {
		return s.availableTopics
	}

	topics := make([]string, 0, len(topicsSet))
	for topic := range topicsSet {
		topics = append(topics, topic)
	}

	sort.Strings(topics)
	return topics
}

type ListDestinationByTenantOpts struct {
	Filter *DestinationFilter
}

type DestinationFilter struct {
	Type   []string
	Topics []string
}

func WithDestinationFilter(filter DestinationFilter) ListDestinationByTenantOpts {
	return ListDestinationByTenantOpts{Filter: &filter}
}

// match returns true if the destinationSummary matches the options
func (filter DestinationFilter) match(destinationSummary DestinationSummary) bool {
	if len(filter.Type) > 0 && !slices.Contains(filter.Type, destinationSummary.Type) {
		return false
	}
	if len(filter.Topics) > 0 {
		filterMatchesAll := len(filter.Topics) == 1 && filter.Topics[0] == "*"
		if !destinationSummary.Topics.MatchesAll() {
			if filterMatchesAll {
				return false
			}
			for _, topic := range filter.Topics {
				if !slices.Contains(destinationSummary.Topics, topic) {
					return false
				}
			}
		}
	}
	return true
}
