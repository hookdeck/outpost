package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixAlert = "alert" // Base prefix for all alert keys
	keyFailures    = "cf"    // Set for consecutive failure attempt IDs
	alertKeyTTL    = 24 * time.Hour
)

// AlertStore persists the tracker's own state: the consecutive-failure count
// per destination.
type AlertStore interface {
	// IncrementConsecutiveFailureCount records a failed attempt and returns the
	// destination's current consecutive-failure count. Recording is idempotent
	// per attempt ID, so replays never double-count.
	IncrementConsecutiveFailureCount(ctx context.Context, tenantID, destinationID, attemptID string) (int, error)
	ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error
}

type redisAlertStore struct {
	client       redis.Cmdable
	deploymentID string
}

// NewRedisAlertStore creates a new Redis-backed alert store
func NewRedisAlertStore(client redis.Cmdable, deploymentID string) AlertStore {
	return &redisAlertStore{
		client:       client,
		deploymentID: deploymentID,
	}
}

func (s *redisAlertStore) IncrementConsecutiveFailureCount(ctx context.Context, tenantID, destinationID, attemptID string) (int, error) {
	key := s.getFailuresKey(tenantID, destinationID)

	// Use a transaction to ensure atomicity between SADD, SCARD, and EXPIRE operations.
	// SADD is idempotent — adding the same attemptID on replay is a no-op,
	// preventing double-counting when messages are redelivered.
	pipe := s.client.TxPipeline()
	pipe.SAdd(ctx, key, attemptID)
	scardCmd := pipe.SCard(ctx, key)
	pipe.Expire(ctx, key, alertKeyTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to execute consecutive failure count transaction: %w", err)
	}

	count, err := scardCmd.Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get consecutive failure count: %w", err)
	}

	return int(count), nil
}

func (s *redisAlertStore) ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(tenantID, destinationID)).Err()
}

func (s *redisAlertStore) deploymentPrefix() string {
	if s.deploymentID == "" {
		return ""
	}
	return fmt.Sprintf("%s:", s.deploymentID)
}

func (s *redisAlertStore) getFailuresKey(tenantID, destinationID string) string {
	return fmt.Sprintf("%s%s:%s:%s:%s", s.deploymentPrefix(), keyPrefixAlert, tenantID, destinationID, keyFailures)
}
