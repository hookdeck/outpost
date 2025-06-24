package infra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/redis"
)

type redisLock struct {
	client *redis.Client
	key    string
	value  string
	ttl    time.Duration
}

// LockOption configures a redisLock
type LockOption func(*redisLock)

// LockWithKey sets a custom key for the lock
func LockWithKey(key string) LockOption {
	return func(l *redisLock) {
		l.key = key
	}
}

// LockWithTTL sets a custom TTL for the lock
func LockWithTTL(ttl time.Duration) LockOption {
	return func(l *redisLock) {
		l.ttl = ttl
	}
}

// NewRedisLock creates a new Redis-based distributed lock
func NewRedisLock(client *redis.Client, opts ...LockOption) Lock {
	lock := &redisLock{
		client: client,
		key:    lockKey, // default
		value:  generateRandomValue(),
		ttl:    lockTTL, // default
	}

	for _, opt := range opts {
		opt(lock)
	}

	return lock
}

// AttemptLock attempts to acquire the lock using SET NX PX
// Returns true if lock was acquired, false if already locked by another process
func (l *redisLock) AttemptLock(ctx context.Context) (bool, error) {
	// SET key value NX PX milliseconds
	// NX: Only set if key doesn't exist
	// PX: Set expiry in milliseconds
	result := l.client.SetNX(ctx, l.key, l.value, l.ttl)
	if result.Err() != nil {
		return false, result.Err()
	}
	return result.Val(), nil
}

// Unlock releases the lock, but only if we still own it
// Returns true if successfully unlocked, false if lock was not held by us
func (l *redisLock) Unlock(ctx context.Context) (bool, error) {
	// Lua script for safe unlock: only delete if value matches
	// This prevents unlocking a lock that was acquired by another process
	// after our lock expired
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`
	result := l.client.Eval(ctx, script, []string{l.key}, l.value)
	if result.Err() != nil {
		return false, result.Err()
	}

	val, err := result.Int()
	if err != nil {
		return false, err
	}

	return val == 1, nil
}

// generateRandomValue creates a random string to use as the lock value
// This ensures each lock instance has a unique identifier
func generateRandomValue() string {
	// Primary: Use crypto/rand (backed by /dev/urandom on Unix)
	// This is cryptographically secure and the recommended approach
	b := make([]byte, 20) // 20 bytes = 160 bits of entropy
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}

	// Fallback 1: Use UUID v4 which has its own entropy sources
	// UUID v4 provides 122 bits of randomness
	if id, err := uuid.NewRandom(); err == nil {
		return id.String()
	}

	// Fallback 2: Combination of timestamp + hostname + PID
	// This is unique enough for most practical purposes
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%d-%s-%d", time.Now().UnixNano(), hostname, os.Getpid())
}
