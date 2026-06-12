package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixAlert = "alert"  // Base prefix for all alert keys
	keyFailures    = "cf"     // Set for consecutive failure attempt IDs
	keyEvaluated   = "cfeval" // Set for fully evaluated attempt IDs
	alertKeyTTL    = 24 * time.Hour
)

// FailureCountResult reports the state of consecutive-failure tracking
// after recording an attempt.
type FailureCountResult struct {
	Count            int  // current consecutive failure count
	NewlyCounted     bool // attempt was not previously counted (first delivery of this attempt)
	AlreadyEvaluated bool // attempt was fully evaluated before (marked via MarkAttemptEvaluated)
}

// AlertStore manages alert-related data persistence
type AlertStore interface {
	IncrementConsecutiveFailureCount(ctx context.Context, tenantID, destinationID, attemptID string) (FailureCountResult, error)
	// MarkAttemptEvaluated records that an attempt's alert evaluation fully
	// completed, so replays (MQ redelivery, producer re-publish) can skip
	// re-evaluating it.
	MarkAttemptEvaluated(ctx context.Context, tenantID, destinationID, attemptID string) error
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

func (s *redisAlertStore) IncrementConsecutiveFailureCount(ctx context.Context, tenantID, destinationID, attemptID string) (FailureCountResult, error) {
	key := s.getFailuresKey(destinationID)

	// Use a transaction to ensure atomicity between SADD, SCARD, and EXPIRE operations.
	// SADD is idempotent — adding the same attemptID on replay is a no-op,
	// preventing double-counting when messages are redelivered.
	pipe := s.client.TxPipeline()
	saddCmd := pipe.SAdd(ctx, key, attemptID)
	scardCmd := pipe.SCard(ctx, key)
	evaluatedCmd := pipe.SIsMember(ctx, s.getEvaluatedKey(destinationID), attemptID)
	pipe.Expire(ctx, key, alertKeyTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return FailureCountResult{}, fmt.Errorf("failed to execute consecutive failure count transaction: %w", err)
	}

	added, err := saddCmd.Result()
	if err != nil {
		return FailureCountResult{}, fmt.Errorf("failed to record attempt: %w", err)
	}

	count, err := scardCmd.Result()
	if err != nil {
		return FailureCountResult{}, fmt.Errorf("failed to get consecutive failure count: %w", err)
	}

	evaluated, err := evaluatedCmd.Result()
	if err != nil {
		return FailureCountResult{}, fmt.Errorf("failed to check evaluated state: %w", err)
	}

	return FailureCountResult{
		Count:            int(count),
		NewlyCounted:     added == 1,
		AlreadyEvaluated: evaluated,
	}, nil
}

func (s *redisAlertStore) MarkAttemptEvaluated(ctx context.Context, tenantID, destinationID, attemptID string) error {
	key := s.getEvaluatedKey(destinationID)

	pipe := s.client.TxPipeline()
	pipe.SAdd(ctx, key, attemptID)
	pipe.Expire(ctx, key, alertKeyTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to mark attempt evaluated: %w", err)
	}
	return nil
}

func (s *redisAlertStore) ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(destinationID), s.getEvaluatedKey(destinationID)).Err()
}

func (s *redisAlertStore) deploymentPrefix() string {
	if s.deploymentID == "" {
		return ""
	}
	return fmt.Sprintf("%s:", s.deploymentID)
}

func (s *redisAlertStore) getFailuresKey(destinationID string) string {
	return fmt.Sprintf("%s%s:%s:%s", s.deploymentPrefix(), keyPrefixAlert, destinationID, keyFailures)
}

func (s *redisAlertStore) getEvaluatedKey(destinationID string) string {
	return fmt.Sprintf("%s%s:%s:%s", s.deploymentPrefix(), keyPrefixAlert, destinationID, keyEvaluated)
}
