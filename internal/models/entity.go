package models

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/hookdeck/outpost/internal/redis"
)

// TODO: get this from config
const MAX_DESTINATIONS_PER_TENANT = 100

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
	ErrDuplicateDestination = errors.New("destination already exists")
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
	redisClient     *redis.Client
	cipher          Cipher
	availableTopics []string
}

var _ EntityStore = (*entityStoreImpl)(nil)

func NewEntityStore(redisClient *redis.Client, cipher Cipher, availableTopics []string) EntityStore {
	return &entityStoreImpl{
		redisClient:     redisClient,
		cipher:          cipher,
		availableTopics: availableTopics,
	}
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
	return s.redisClient.HSet(ctx, redisTenantID(tenant.ID), tenant).Err()
}

func (s *entityStoreImpl) DeleteTenant(ctx context.Context, tenantID string) error {
	maxRetries := 100

	txf := func(tx *redis.Tx) error {
		destinationIDs, err := s.redisClient.HKeys(ctx, redisTenantDestinationSummaryKey(tenantID)).Result()
		if err != nil {
			return err
		}
		_, err = s.redisClient.TxPipelined(ctx, func(r redis.Pipeliner) error {
			for _, destinationID := range destinationIDs {
				r.Del(ctx, redisDestinationID(destinationID, tenantID))
			}
			r.Del(ctx, redisTenantDestinationSummaryKey(tenantID))
			r.Del(ctx, redisTenantID(tenantID))
			return nil
		})
		return err
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
	destinationExists, err := m.redisClient.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if destinationExists > 0 {
		return ErrDuplicateDestination
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
	_, err := s.redisClient.TxPipelined(ctx, func(r redis.Pipeliner) error {
		if err := r.HDel(ctx, redisTenantDestinationSummaryKey(tenantID), destinationID).Err(); err != nil {
			return err
		}
		return r.Del(ctx, redisDestinationID(destinationID, tenantID)).Err()
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
		if !destinationSummary.Disabled && (destinationSummary.Topics[0] == "*" || slices.Contains(destinationSummary.Topics, event.Topic)) {
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
