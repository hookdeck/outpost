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
	client *redis.Client
}

// NewRedisAlertStore creates a new Redis-backed alert store
func NewRedisAlertStore(client *redis.Client) AlertStore {
	return &redisAlertStore{client: client}
}

func (s *redisAlertStore) IncrementConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) (int, error) {
	key := s.getFailuresKey(destinationID)
	pipe := s.client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 24*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to increment failures: %w", err)
	}

	return int(incr.Val()), nil
}

func (s *redisAlertStore) ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(destinationID)).Err()
}

func (s *redisAlertStore) getFailuresKey(destinationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefixAlert, destinationID, keyFailures)
}
