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

func redisTenantID(tenantID string) string {
	return fmt.Sprintf("tenant:%s", tenantID)
}

func redisTenantDestinationSummaryKey(tenantID string) string {
	return fmt.Sprintf("tenant:%s:destinations", tenantID)
}

func redisDestinationID(destinationID, tenantID string) string {
	return fmt.Sprintf("tenant:%s:destination:%s", tenantID, destinationID)
}

type entityStoreImpl struct {
	redisClient              *redis.Client
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

func NewEntityStore(redisClient *redis.Client, opts ...EntityStoreOption) EntityStore {
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
	topicsCmd := pipe.HGetAll(ctx, redisTenantDestinationSummaryKey(tenantID))

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

	destinationSummaryList, err := s.parselistDestinationSummaryByTenantCmd(topicsCmd, ListDestinationByTenantOpts{})
	if err != nil {
		return nil, err
	}
	tenant.DestinationsCount = len(destinationSummaryList)
	tenant.Topics = s.parseTenantTopics(destinationSummaryList)

	return tenant, err
}

func (s *entityStoreImpl) UpsertTenant(ctx context.Context, tenant Tenant) error {
	key := redisTenantID(tenant.ID)

	_, err := s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// Support overriding deleted resources
		pipe.Persist(ctx, key)
		pipe.HDel(ctx, key, "deleted_at")

		// Set tenant data
		pipe.HSet(ctx, key, tenant)
		return nil
	})

	return err
}

func (s *entityStoreImpl) DeleteTenant(ctx context.Context, tenantID string) error {
	maxRetries := 100

	if exists, err := s.redisClient.Exists(ctx, redisTenantID(tenantID)).Result(); err != nil {
		return err
	} else if exists == 0 {
		return ErrTenantNotFound
	}

	txf := func(tx *redis.Tx) error {
		destinationIDs, err := s.redisClient.HKeys(ctx, redisTenantDestinationSummaryKey(tenantID)).Result()
		if err != nil {
			return err
		}
		if _, err := s.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			now := time.Now()
			for _, destinationID := range destinationIDs {
				s.deleteDestinationOperation(ctx, pipe, redisDestinationID(destinationID, tenantID), now)
			}
			pipe.Del(ctx, redisTenantDestinationSummaryKey(tenantID))
			tenantKey := redisTenantID(tenantID)
			pipe.Del(ctx, tenantKey)
			pipe.HSet(ctx, tenantKey, "deleted_at", now)
			pipe.Expire(ctx, tenantKey, 7*24*time.Hour)
			return nil
		}); err != nil {
			return err
		}
		return nil
	}

	for i := 0; i < maxRetries; i++ {
		err := s.redisClient.Watch(ctx, txf, redisTenantDestinationSummaryKey(tenantID))
		if err == nil {
			// Success.
			return nil
		}
		if err == redis.TxFailedErr {
			// Optimistic lock lost. Retry.
			continue
		}
		// Return any other error.
		return err
	}

	return errors.New("increment reached maximum number of retries")
}

func (s *entityStoreImpl) listDestinationSummaryByTenant(ctx context.Context, tenantID string, opts ListDestinationByTenantOpts) ([]DestinationSummary, error) {
	return s.parselistDestinationSummaryByTenantCmd(s.redisClient.HGetAll(ctx, redisTenantDestinationSummaryKey(tenantID)), opts)
}

func (s *entityStoreImpl) parselistDestinationSummaryByTenantCmd(cmd *redis.MapStringStringCmd, opts ListDestinationByTenantOpts) ([]DestinationSummary, error) {
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
	_, err := m.redisClient.TxPipelined(ctx, func(r redis.Pipeliner) error {
		credentialsBytes, err := destination.Credentials.MarshalBinary()
		if err != nil {
			return err
		}
		encryptedCredentials, err := m.cipher.Encrypt(credentialsBytes)
		if err != nil {
			return err
		}
		// Support overriding deleted resources
		r.Persist(ctx, key)
		r.HDel(ctx, key, "deleted_at")
		// Set the new destination values
		r.HSet(ctx, key, "id", destination.ID)
		r.HSet(ctx, key, "type", destination.Type)
		r.HSet(ctx, key, "topics", &destination.Topics)
		r.HSet(ctx, key, "config", &destination.Config)
		r.HSet(ctx, key, "credentials", encryptedCredentials)
		r.HSet(ctx, key, "created_at", destination.CreatedAt)
		if destination.DisabledAt != nil {
			r.HSet(ctx, key, "disabled_at", *destination.DisabledAt)
		} else {
			r.HDel(ctx, key, "disabled_at")
		}
		r.HSet(ctx, redisTenantDestinationSummaryKey(destination.TenantID), destination.ID, destination.ToSummary()).Val()
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

	pipe := s.redisClient.Pipeline()
	pipe.HDel(ctx, summaryKey, destinationID)
	s.deleteDestinationOperation(ctx, pipe, key, time.Now())
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	return nil
}

func (s *entityStoreImpl) deleteDestinationOperation(ctx context.Context, pipe redis.Pipeliner, key string, ts time.Time) {
	pipe.Del(ctx, key)
	pipe.HSet(ctx, key, "deleted_at", ts)
	pipe.Expire(ctx, key, 7*24*time.Hour)
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
