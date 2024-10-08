package models

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/hookdeck/EventKit/internal/redis"
)

// TODO: get this from config
const MAX_DESTINATIONS_PER_TENANT = 100

type EntityStore interface {
	RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error)
	UpsertTenant(ctx context.Context, tenant Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
	ListDestinationSummaryByTenant(ctx context.Context, tenantID string) ([]DestinationSummary, error)
	ListDestinationByTenant(ctx context.Context, tenantID string) ([]Destination, error)
	RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*Destination, error)
	UpsertDestination(ctx context.Context, destination Destination) error
	DeleteDestination(ctx context.Context, tenantID, destinationID string) error
	MatchEvent(ctx context.Context, event Event) ([]DestinationSummary, error)
}

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
	redisClient *redis.Client
	cipher      Cipher
}

var _ EntityStore = (*entityStoreImpl)(nil)

func NewEntityStore(redisClient *redis.Client, cipher Cipher) EntityStore {
	return &entityStoreImpl{
		redisClient: redisClient,
		cipher:      cipher,
	}
}

func (s *entityStoreImpl) RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	hash, err := s.redisClient.HGetAll(ctx, redisTenantID(tenantID)).Result()
	if err != nil {
		return nil, err
	}
	if len(hash) == 0 {
		return nil, nil
	}
	tenant := &Tenant{}
	if err = tenant.parseRedisHash(hash); err != nil {
		return nil, err
	}
	return tenant, nil
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

func (s *entityStoreImpl) ListDestinationSummaryByTenant(ctx context.Context, tenantID string) ([]DestinationSummary, error) {
	destinationSummaryListHash, err := s.redisClient.HGetAll(ctx, redisTenantDestinationSummaryKey(tenantID)).Result()
	if err != nil {
		if err == redis.Nil {
			return []DestinationSummary{}, nil
		}
		return nil, err
	}
	destinationSummaryList := make([]DestinationSummary, len(destinationSummaryListHash))
	index := 0
	for _, destinationSummaryStr := range destinationSummaryListHash {
		destinationSummary := DestinationSummary{}
		if err := destinationSummary.UnmarshalBinary([]byte(destinationSummaryStr)); err != nil {
			return nil, err
		}
		destinationSummaryList[index] = destinationSummary
		index++
	}
	return destinationSummaryList, nil
}

func (s *entityStoreImpl) ListDestinationByTenant(ctx context.Context, tenantID string) ([]Destination, error) {
	destinationSummaryList, err := s.ListDestinationSummaryByTenant(ctx, tenantID)

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

func (m *entityStoreImpl) UpsertDestination(ctx context.Context, destination Destination) error {
	err := destination.Validate(ctx)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	key := redisDestinationID(destination.ID, destination.TenantID)
	_, err = m.redisClient.TxPipelined(ctx, func(r redis.Pipeliner) error {
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
			r.HSet(ctx, key, "disabled_at", destination.DisabledAt)
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
	destinationSummaryList, err := s.ListDestinationSummaryByTenant(ctx, event.TenantID)
	if err != nil {
		return nil, err
	}

	if event.Topic == "" {
		return destinationSummaryList, nil
	}

	matchedDestinationSummaryList := []DestinationSummary{}

	for _, destinationSummary := range destinationSummaryList {
		if destinationSummary.Topics[0] == "*" || slices.Contains(destinationSummary.Topics, event.Topic) {
			matchedDestinationSummaryList = append(matchedDestinationSummaryList, destinationSummary)
		}
	}

	return matchedDestinationSummaryList, nil
}
