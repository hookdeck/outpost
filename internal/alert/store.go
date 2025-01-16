package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixFailures  = "alert:failures"
	keyPrefixLastAlert = "alert:last_alert" // Will store both time and level in a hash

	// Hash fields
	fieldLastAlertTime  = "time"
	fieldLastAlertLevel = "level"
)

// AlertStore manages alert-related data persistence
type AlertStore interface {
	IncrementAndGetFailureState(ctx context.Context, tenantID, destinationID string) (FailureState, error)
	ResetFailures(ctx context.Context, tenantID, destinationID string) error
	UpdateLastAlert(ctx context.Context, tenantID, destinationID string, t time.Time, level int) error
}

type FailureState struct {
	FailureCount   int64
	LastAlertTime  time.Time
	LastAlertLevel int
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

	// Queue get last alert hash fields
	timeCmd := pipe.HGet(ctx, s.getLastAlertKey(destinationID), fieldLastAlertTime)
	levelCmd := pipe.HGet(ctx, s.getLastAlertKey(destinationID), fieldLastAlertLevel)

	// Execute all commands
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return FailureState{}, fmt.Errorf("failed to execute pipeline: %w", err)
	}

	// Get results
	count, err := incrCmd.Result()
	if err != nil {
		return FailureState{}, fmt.Errorf("failed to increment failures: %w", err)
	}

	// Parse last alert time
	lastAlertTime, _ := timeCmd.Time() // Zero time if not found

	// Parse last alert level
	lastAlertLevel := 0
	if level, err := levelCmd.Int(); err == nil {
		lastAlertLevel = level
	}

	return FailureState{
		FailureCount:   count,
		LastAlertTime:  lastAlertTime,
		LastAlertLevel: lastAlertLevel,
	}, nil
}

func (s *redisAlertStore) UpdateLastAlert(ctx context.Context, tenantID, destinationID string, t time.Time, level int) error {
	// Use HSet to update both fields atomically
	return s.client.HSet(ctx, s.getLastAlertKey(destinationID), map[string]interface{}{
		fieldLastAlertTime:  t.Format(time.RFC3339Nano),
		fieldLastAlertLevel: level,
	}).Err()
}

func (s *redisAlertStore) ResetFailures(ctx context.Context, tenantID, destinationID string) error {
	return s.client.Del(ctx, s.getFailuresKey(destinationID)).Err()
}

func (s *redisAlertStore) getFailuresKey(destinationID string) string {
	return fmt.Sprintf("%s:%s", keyPrefixFailures, destinationID)
}

func (s *redisAlertStore) getLastAlertKey(destinationID string) string {
	return fmt.Sprintf("%s:%s", keyPrefixLastAlert, destinationID)
}
