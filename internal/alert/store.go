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

type redisAlertStore struct {
	client redis.Cmdable
}

// NewRedisAlertStore creates a new Redis-backed alert store
func NewRedisAlertStore(client *redis.Client) AlertStore {
	return &redisAlertStore{client: client}
}

func (s *redisAlertStore) IncrementFailures(ctx context.Context, tenantID, destinationID string) (int64, error) {
	key := s.getFailuresKey(tenantID, destinationID)
	return s.client.Incr(ctx, key).Result()
}

func (s *redisAlertStore) ResetFailures(ctx context.Context, tenantID, destinationID string) error {
	key := s.getFailuresKey(tenantID, destinationID)
	return s.client.Del(ctx, key).Err()
}

func (s *redisAlertStore) GetLastAlertTime(ctx context.Context, tenantID, destinationID string) (time.Time, error) {
	key := s.getLastAlertKey(tenantID, destinationID)
	val, err := s.client.Get(ctx, key).Time()
	if err == redis.Nil {
		return time.Time{}, ErrNotFound
	}
	return val, err
}

func (s *redisAlertStore) UpdateLastAlertTime(ctx context.Context, tenantID, destinationID string, t time.Time) error {
	key := s.getLastAlertKey(tenantID, destinationID)
	return s.client.Set(ctx, key, t.Format(time.RFC3339Nano), 0).Err()
}

func (s *redisAlertStore) WithTx(ctx context.Context, fn func(tx AlertStore) error) error {
	// If we're already in a transaction, just execute the function
	if _, ok := s.client.(redis.Pipeliner); ok {
		return fn(s)
	}

	// Get the underlying Redis client
	client, ok := s.client.(*redis.Client)
	if !ok {
		return fmt.Errorf("cannot start transaction: not a Redis client")
	}

	// Start a Redis transaction
	pipe := client.TxPipeline()
	defer pipe.Discard()

	// Create a new store that uses the pipeline
	txStore := &redisAlertStore{client: pipe}

	// Execute the transaction function
	if err := fn(txStore); err != nil {
		return err
	}

	// Execute the pipeline
	_, err := pipe.Exec(ctx)
	return err
}

func (s *redisAlertStore) getFailuresKey(tenantID, destinationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefixFailures, tenantID, destinationID)
}

func (s *redisAlertStore) getLastAlertKey(tenantID, destinationID string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefixLastAlert, tenantID, destinationID)
}
