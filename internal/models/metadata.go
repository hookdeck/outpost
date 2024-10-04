package models

import (
	"context"
	"fmt"
	"sort"

	"github.com/hookdeck/EventKit/internal/redis"
)

// TODO: get this from config
const MAX_DESTINATIONS_PER_TENANT = 100

type MetadataRepo interface {
	RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error)
	UpsertTenant(ctx context.Context, tenant Tenant) error
	DeleteTenant(ctx context.Context, tenantID string) error
	ListDestinationByTenant(ctx context.Context, tenantID string) ([]Destination, error)
	RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*Destination, error)
	UpsertDestination(ctx context.Context, destination Destination) error
	DeleteDestination(ctx context.Context, tenantID, destinationID string) error
	DeleteManyDestination(ctx context.Context, tenantID string, destinationIDs ...string) (int64, error)
}

func redisTenantID(tenantID string) string {
	return fmt.Sprintf("tenant:%s", tenantID)
}

func redisDestinationID(destinationID, tenantID string) string {
	return fmt.Sprintf("tenant:%s:destination:%s", tenantID, destinationID)
}

type metadataImpl struct {
	redisClient *redis.Client
	cipher      Cipher
}

var _ MetadataRepo = (*metadataImpl)(nil)

func NewMetadataRepo(redisClient *redis.Client, cipher Cipher) MetadataRepo {
	return &metadataImpl{
		redisClient: redisClient,
		cipher:      cipher,
	}
}

func (m *metadataImpl) RetrieveTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	hash, err := m.redisClient.HGetAll(ctx, redisTenantID(tenantID)).Result()
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

func (m *metadataImpl) UpsertTenant(ctx context.Context, tenant Tenant) error {
	return m.redisClient.HSet(ctx, redisTenantID(tenant.ID), tenant).Err()
}

func (m *metadataImpl) DeleteTenant(ctx context.Context, tenantID string) error {
	return m.redisClient.Del(ctx, redisTenantID(tenantID)).Err()
}

func (m *metadataImpl) ListDestinationByTenant(ctx context.Context, tenantID string) ([]Destination, error) {
	keys, _, err := m.redisClient.Scan(ctx, 0, redisDestinationID("*", tenantID), MAX_DESTINATIONS_PER_TENANT).Result()
	if err != nil {
		return nil, err
	}

	pipe := m.redisClient.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.HGetAll(ctx, key)
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	destinations := make([]Destination, len(keys))
	for i, cmd := range cmds {
		destination := &Destination{TenantID: tenantID}
		err = destination.parseRedisHash(cmd, m.cipher)
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

func (m *metadataImpl) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*Destination, error) {
	cmd := m.redisClient.HGetAll(ctx, redisDestinationID(destinationID, tenantID))
	destination := &Destination{TenantID: tenantID}
	if err := destination.parseRedisHash(cmd, m.cipher); err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return destination, nil
}

func (m *metadataImpl) UpsertDestination(ctx context.Context, destination Destination) error {
	err := destination.Validate(ctx)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	key := redisDestinationID(destination.ID, destination.TenantID)
	_, err = m.redisClient.Pipelined(ctx, func(r redis.Pipeliner) error {
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
		return nil
	})
	return err
}

func (m *metadataImpl) DeleteDestination(ctx context.Context, tenantID, destinationID string) error {
	return m.redisClient.Del(ctx, redisDestinationID(destinationID, tenantID)).Err()
}

func (m *metadataImpl) DeleteManyDestination(ctx context.Context, tenantID string, destinationIDs ...string) (int64, error) {
	if len(destinationIDs) == 0 {
		return 0, nil
	}
	keys := make([]string, len(destinationIDs))
	for i, destinationID := range destinationIDs {
		keys[i] = redisDestinationID(destinationID, tenantID)
	}
	return m.redisClient.Del(ctx, keys...).Result()
}
