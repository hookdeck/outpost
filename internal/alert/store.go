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

	// Use a transaction to ensure atomicity between INCR and EXPIRE operations.
	// Since all operations use the same key, they will be routed to the same hash slot
	// in Redis cluster mode, making transactions safe to use.
	pipe := s.client.TxPipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 24*time.Hour)

	// Execute the transaction
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to execute consecutive failure count transaction: %w", err)
	}

	// Get the incremented count from the INCR command result
	count, err := incrCmd.Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get incremented consecutive failure count: %w", err)
	}

	return int(count), nil
}

func (s *redisAlertStore) ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(destinationID)).Err()
}

func (s *redisAlertStore) getFailuresKey(destinationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefixAlert, destinationID, keyFailures)
}
