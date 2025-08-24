package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixAlert = "alert"                // Base prefix for all alert keys
	keyFailures    = "consecutive_failures" // Counter for consecutive failures)
)

// AlertStore manages alert-related data persistence
type AlertStore interface {
	IncrementConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) (int, error)
	ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error
}

type redisAlertStore struct {
	client redis.Cmdable
}

// NewRedisAlertStore creates a new Redis-backed alert store
func NewRedisAlertStore(client redis.Cmdable) AlertStore {
	return &redisAlertStore{client: client}
}

func (s *redisAlertStore) IncrementConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) (int, error) {
	key := s.getFailuresKey(destinationID)
	
	// Increment the counter
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to increment failures: %w", err)
	}
	
	// Set expiration - this is a separate operation since we can't use transactions in cluster mode
	// If this fails, the key will remain but without expiration, which is acceptable
	s.client.Expire(ctx, key, 24*time.Hour)
	
	return int(count), nil
}

func (s *redisAlertStore) ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(destinationID)).Err()
}

func (s *redisAlertStore) getFailuresKey(destinationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefixAlert, destinationID, keyFailures)
}
