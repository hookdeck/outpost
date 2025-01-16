package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixFailures  = "alert:failures"
	keyPrefixLastAlert = "alert:last_alert"
)

// AlertStore manages alert-related data persistence
type AlertStore interface {
	IncrementAndGetFailureState(ctx context.Context, tenantID, destinationID string) (FailureState, error)
	ResetFailures(ctx context.Context, tenantID, destinationID string) error
	UpdateLastAlertTime(ctx context.Context, tenantID, destinationID string, t time.Time) error
}

type FailureState struct {
	FailureCount  int64
	LastAlertTime time.Time
}

type redisAlertStore struct {
	client *redis.Client
}

// NewRedisAlertStore creates a new Redis-backed alert store
func NewRedisAlertStore(client *redis.Client) AlertStore {
	return &redisAlertStore{client: client}
}

func (s *redisAlertStore) IncrementAndGetFailureState(ctx context.Context, tenantID, destinationID string) (FailureState, error) {
	pipe := s.client.Pipeline()
	defer pipe.Discard()

	// Queue increment command
	incrCmd := pipe.Incr(ctx, s.getFailuresKey(destinationID))

	// Queue get last alert time command
	timeCmd := pipe.Get(ctx, s.getLastAlertKey(destinationID))

	// Execute both commands
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return FailureState{}, fmt.Errorf("failed to execute pipeline: %w", err)
	}

	// Get results
	count, err := incrCmd.Result()
	if err != nil {
		return FailureState{}, fmt.Errorf("failed to increment failures: %w", err)
	}

	lastAlertTime, _ := timeCmd.Time() // Zero time if not found

	return FailureState{
		FailureCount:  count,
		LastAlertTime: lastAlertTime,
	}, nil
}

func (s *redisAlertStore) ResetFailures(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(destinationID)).Err()
}

func (s *redisAlertStore) UpdateLastAlertTime(ctx context.Context, tenantID, destinationID string, t time.Time) error {
	return s.client.Set(ctx, s.getLastAlertKey(destinationID), t.Format(time.RFC3339Nano), 0).Err()
}

func (s *redisAlertStore) getFailuresKey(destinationID string) string {
	return fmt.Sprintf("%s:%s", keyPrefixFailures, destinationID)
}

func (s *redisAlertStore) getLastAlertKey(destinationID string) string {
	return fmt.Sprintf("%s:%s", keyPrefixLastAlert, destinationID)
}
