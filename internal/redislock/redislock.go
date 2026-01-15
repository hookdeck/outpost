// Package redislock provides distributed locking using Redis.
//
// This implementation uses a naive "single instance" Redis distributed locking algorithm
// as described in https://redis.io/docs/latest/develop/use/patterns/distributed-locks/
//
// We use the simple SET NX PX pattern which has edge cases where multiple nodes may
// acquire the lock under extreme circumstances (see the Redis documentation for details).
// We accept these edge cases because our primary use cases can tolerate occasional races:
//
//  1. Infrastructure provisioning - infrequent, cloud providers handle concurrent attempts gracefully
//  2. Migration coordination - prevents multiple nodes from running migrations simultaneously at
//     startup; if a race occurs, the migration runner re-checks "already applied" status after
//     acquiring the lock
//  3. Initialization tasks - typically have "already exists" checks after acquiring lock
//
// In the worst case, if an operation fails due to a lock race, the node will fail its
// health check and another node can take over. This is acceptable for startup-time
// operations that run infrequently.
//
// Do NOT use this for high-frequency locking or cases where duplicate execution would
// cause data corruption. For those cases, consider Redlock or a proper distributed lock.
package redislock

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

// Lock defines the interface for distributed locking
type Lock interface {
	AttemptLock(ctx context.Context) (bool, error)
	Unlock(ctx context.Context) (bool, error)
}

type redisLock struct {
	client redis.Cmdable
	key    string
	value  string
	ttl    time.Duration
}

// Option configures a redisLock
type Option func(*redisLock)

// WithKey sets a custom key for the lock
func WithKey(key string) Option {
	return func(l *redisLock) {
		l.key = key
	}
}

// WithTTL sets a custom TTL for the lock
func WithTTL(ttl time.Duration) Option {
	return func(l *redisLock) {
		l.ttl = ttl
	}
}

// New creates a new Redis-based distributed lock
func New(client redis.Cmdable, opts ...Option) Lock {
	lock := &redisLock{
		client: client,
		key:    "outpost:lock", // default
		value:  generateRandomValue(),
		ttl:    10 * time.Second, // default
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
	b := make([]byte, 20) // 20 bytes = 160 bits of entropy
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}

	// Fallback 1: Use UUID v4
	if id, err := uuid.NewRandom(); err == nil {
		return id.String()
	}

	// Fallback 2: Combination of timestamp + hostname + PID
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%d-%s-%d", time.Now().UnixNano(), hostname, os.Getpid())
}
