package models

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

const defaultMaxDestinationsPerTenant = 20

type EntityStore interface {
	RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error)
	UpsertTenant(ctx context.Context, tenant Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
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
)

// New cluster-compatible key formats with hash tags
func redisTenantID(tenantID string) string {
	return fmt.Sprintf("{%s}:tenant", tenantID)
}

func redisTenantDestinationSummaryKey(tenantID string) string {
	return fmt.Sprintf("{%s}:destinations", tenantID)
}

func redisDestinationID(destinationID, tenantID string) string {
	return fmt.Sprintf("{%s}:destination:%s", tenantID, destinationID)
}

type entityStoreImpl struct {
	redisClient              redis.Cmdable
	cipher                   Cipher
	availableTopics          []string
	maxDestinationsPerTenant int
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

func (s *entityStoreImpl) RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	pipe := s.redisClient.Pipeline()
	tenantCmd := pipe.HGetAll(ctx, redisTenantID(tenantID))
	destinationListCmd := pipe.HGetAll(ctx, redisTenantDestinationSummaryKey(tenantID))

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
	key := redisTenantID(tenant.ID)

	// For cluster compatibility, execute commands individually instead of in a transaction
	// Support overriding deleted resources
	if err := s.redisClient.Persist(ctx, key).Err(); err != nil && err != redis.Nil {
		return err
	}
	
	if err := s.redisClient.HDel(ctx, key, "deleted_at").Err(); err != nil && err != redis.Nil {
		return err
	}

	// Set tenant data
	return s.redisClient.HSet(ctx, key, tenant).Err()
}

func (s *entityStoreImpl) DeleteTenant(ctx context.Context, tenantID string) error {
	if exists, err := s.redisClient.Exists(ctx, redisTenantID(tenantID)).Result(); err != nil {
		return err
	} else if exists == 0 {
		return ErrTenantNotFound
	}

	// Get destination IDs before transaction
	destinationIDs, err := s.redisClient.HKeys(ctx, redisTenantDestinationSummaryKey(tenantID)).Result()
	if err != nil {
		return err
	}

	// All operations on same tenant - cluster compatible transaction
	_, err = s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		now := time.Now()
		
		// Delete all destinations atomically
		for _, destinationID := range destinationIDs {
			destKey := redisDestinationID(destinationID, tenantID)
			pipe.HSet(ctx, destKey, "deleted_at", now)
			pipe.Expire(ctx, destKey, 7*24*time.Hour)
		}
		
		// Delete summary and mark tenant as deleted
		pipe.Del(ctx, redisTenantDestinationSummaryKey(tenantID))
		pipe.HSet(ctx, redisTenantID(tenantID), "deleted_at", now)
		pipe.Expire(ctx, redisTenantID(tenantID), 7*24*time.Hour)
		
		return nil
	})
	
	return err
}

func (s *entityStoreImpl) listDestinationSummaryByTenant(ctx context.Context, tenantID string, opts ListDestinationByTenantOpts) ([]DestinationSummary, error) {
	return s.parseListDestinationSummaryByTenantCmd(s.redisClient.HGetAll(ctx, redisTenantDestinationSummaryKey(tenantID)), opts)
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

	pipe := s.redisClient.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(destinationSummaryList))
	for i, destinationSummary := range destinationSummaryList {
		cmds[i] = pipe.HGetAll(ctx, redisDestinationID(destinationSummary.ID, tenantID))
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
	cmd := s.redisClient.HGetAll(ctx, redisDestinationID(destinationID, tenantID))
	destination := &Destination{TenantID: tenantID}
	if err := destination.parseRedisHash(cmd, s.cipher); err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return destination, nil
}

func (m *entityStoreImpl) CreateDestination(ctx context.Context, destination Destination) error {
	key := redisDestinationID(destination.ID, destination.TenantID)
	// Check if destination exists
	if fields, err := m.redisClient.HGetAll(ctx, key).Result(); err != nil {
		return err
	} else if len(fields) > 0 {
		if _, isDeleted := fields["deleted_at"]; !isDeleted {
			return ErrDuplicateDestination
		}
	}

	// Check if tenant has reached max destinations by counting entries in the summary hash
	count, err := m.redisClient.HLen(ctx, redisTenantDestinationSummaryKey(destination.TenantID)).Result()
	if err != nil {
		return err
	}
	if count >= int64(m.maxDestinationsPerTenant) {
		return ErrMaxDestinationsPerTenantReached
	}

	return m.UpsertDestination(ctx, destination)
}

func (m *entityStoreImpl) UpsertDestination(ctx context.Context, destination Destination) error {
	key := redisDestinationID(destination.ID, destination.TenantID)

	// Pre-marshal and encrypt credentials BEFORE starting Redis transaction
	// This isolates marshaling failures from Redis transaction failures
	credentialsBytes, err := destination.Credentials.MarshalBinary()
	if err != nil {
		return fmt.Errorf("invalid destination credentials: %w", err)
	}
	encryptedCredentials, err := m.cipher.Encrypt(credentialsBytes)
	if err != nil {
		return fmt.Errorf("failed to encrypt destination credentials: %w", err)
	}

	// All keys use same tenant prefix - cluster compatible transaction
	summaryKey := redisTenantDestinationSummaryKey(destination.TenantID)
	
	_, err = m.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// Clear deletion markers
		pipe.Persist(ctx, key)
		pipe.HDel(ctx, key, "deleted_at")
		
		// Set all destination fields atomically
		pipe.HSet(ctx, key, "id", destination.ID)
		pipe.HSet(ctx, key, "type", destination.Type)
		pipe.HSet(ctx, key, "topics", &destination.Topics)
		pipe.HSet(ctx, key, "config", &destination.Config)
		pipe.HSet(ctx, key, "credentials", encryptedCredentials)
		pipe.HSet(ctx, key, "created_at", destination.CreatedAt)
		
		if destination.DisabledAt != nil {
			pipe.HSet(ctx, key, "disabled_at", *destination.DisabledAt)
		} else {
			pipe.HDel(ctx, key, "disabled_at")
		}
		
		// Update summary atomically
		pipe.HSet(ctx, summaryKey, destination.ID, destination.ToSummary())
		return nil
	})
	
	return err
}

func (s *entityStoreImpl) DeleteDestination(ctx context.Context, tenantID, destinationID string) error {
	key := redisDestinationID(destinationID, tenantID)
	summaryKey := redisTenantDestinationSummaryKey(tenantID)

	// Check if destination exists
	if exists, err := s.redisClient.Exists(ctx, key).Result(); err != nil {
		return err
	} else if exists == 0 {
		return ErrDestinationNotFound
	}

	// Atomic deletion with same-tenant keys
	_, err := s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		now := time.Now()
		
		// Remove from summary and mark as deleted atomically
		pipe.HDel(ctx, summaryKey, destinationID)
		pipe.HSet(ctx, key, "deleted_at", now)
		pipe.Expire(ctx, key, 7*24*time.Hour)
		
		return nil
	})
	
	return err
}

func (s *entityStoreImpl) MatchEvent(ctx context.Context, event Event) ([]DestinationSummary, error) {
	if event.DestinationID == "" {
		return s.matchEventWithAllDestination(ctx, event)
	} else {
		return s.matchEventWithDestination(ctx, event)
	}
}

func (s *entityStoreImpl) matchEventWithAllDestination(ctx context.Context, event Event) ([]DestinationSummary, error) {
	destinationSummaryList, err := s.listDestinationSummaryByTenant(ctx, event.TenantID, ListDestinationByTenantOpts{})
	if err != nil {
		return nil, err
	}

	if event.Topic == "" {
		return destinationSummaryList, nil
	}

	matchedDestinationSummaryList := []DestinationSummary{}

	for _, destinationSummary := range destinationSummaryList {
		if destinationSummary.Disabled {
			continue
		}
		// If event topic is "*", match all destinations
		// Otherwise, match if destination has "*" topic or matches the event topic
		if event.Topic == "*" || destinationSummary.Topics.MatchesAll() || slices.Contains(destinationSummary.Topics, event.Topic) {
			matchedDestinationSummaryList = append(matchedDestinationSummaryList, destinationSummary)
		}
	}

	return matchedDestinationSummaryList, nil
}

func (s *entityStoreImpl) matchEventWithDestination(ctx context.Context, event Event) ([]DestinationSummary, error) {
	destination, err := s.RetrieveDestination(ctx, event.TenantID, event.DestinationID)
	if err != nil {
		return nil, err
	}
	if destination == nil {
		return []DestinationSummary{}, nil
	}
	if event.Topic == "" || destination.Topics[0] == "*" || slices.Contains(destination.Topics, event.Topic) {
		return []DestinationSummary{*destination.ToSummary()}, nil
	}
	return []DestinationSummary{}, nil
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
