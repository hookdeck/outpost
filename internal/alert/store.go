package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixAlert = "alert"      // Base prefix for all alert keys
	keyFailures    = "failures"   // Counter for consecutive failures
	keyLastAlert   = "last_alert" // Hash containing last alert's time and level

	// Last alert hash fields
	fieldLastAlertTime  = "time"
	fieldLastAlertLevel = "level"
)

// AlertStore manages alert-related data persistence
type AlertStore interface {
	IncrementAndGetAlertState(ctx context.Context, tenantID, destinationID string) (AlertState, error)
	ResetAlertState(ctx context.Context, tenantID, destinationID string) error
	UpdateLastAlert(ctx context.Context, tenantID, destinationID string, t time.Time, level int) error
}

type AlertState struct {
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

func (s *redisAlertStore) IncrementAndGetAlertState(ctx context.Context, tenantID, destinationID string) (AlertState, error) {
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
		return AlertState{}, fmt.Errorf("failed to execute pipeline: %w", err)
	}

	// Get results
	count, err := incrCmd.Result()
	if err != nil {
		return AlertState{}, fmt.Errorf("failed to increment failures: %w", err)
	}

	// Parse last alert time
	lastAlertTime, _ := timeCmd.Time() // Zero time if not found

	// Parse last alert level
	lastAlertLevel := 0
	if level, err := levelCmd.Int(); err == nil {
		lastAlertLevel = level
	}

	return AlertState{
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

func (s *redisAlertStore) ResetAlertState(ctx context.Context, tenantID, destinationID string) error {
	// Delete both failure count and last alert state
	pipe := s.client.Pipeline()
	defer pipe.Discard()

	pipe.Del(ctx, s.getFailuresKey(destinationID))
	pipe.Del(ctx, s.getLastAlertKey(destinationID))

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to reset alert state: %w", err)
	}
	return nil
}

func (s *redisAlertStore) getFailuresKey(destinationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefixAlert, destinationID, keyFailures)
}

func (s *redisAlertStore) getLastAlertKey(destinationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefixAlert, destinationID, keyLastAlert)
}
