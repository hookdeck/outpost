package logretention

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hookdeck/outpost/internal/redis"
)

// RedisPolicyStore implements PolicyStore using Redis.
type RedisPolicyStore struct {
	client       redis.Cmdable
	deploymentID string
}

var _ policyStore = (*RedisPolicyStore)(nil)

// NewRedisPolicyStore creates a new Redis-backed policy store.
func NewRedisPolicyStore(client redis.Cmdable, deploymentID string) *RedisPolicyStore {
	return &RedisPolicyStore{
		client:       client,
		deploymentID: deploymentID,
	}
}

// redisKey returns the Redis key for storing the applied TTL.
// In multi-deployment mode, keys are prefixed with <deploymentID>:.
func (s *RedisPolicyStore) redisKey() string {
	if s.deploymentID == "" {
		return "outpost:log_retention_ttl"
	}
	return fmt.Sprintf("%s:outpost:log_retention_ttl", s.deploymentID)
}

// GetAppliedTTL reads the persisted TTL value from Redis.
// Returns -1 if the key doesn't exist.
func (s *RedisPolicyStore) GetAppliedTTL(ctx context.Context) (int, error) {
	val, err := s.client.Get(ctx, s.redisKey()).Result()
	if err == redis.Nil {
		return -1, nil // Key doesn't exist
	}
	if err != nil {
		return 0, err
	}

	ttl, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid TTL value in Redis: %w", err)
	}

	return ttl, nil
}

// SetAppliedTTL writes the TTL value to Redis.
func (s *RedisPolicyStore) SetAppliedTTL(ctx context.Context, ttlDays int) error {
	return s.client.Set(ctx, s.redisKey(), strconv.Itoa(ttlDays), 0).Err()
}
