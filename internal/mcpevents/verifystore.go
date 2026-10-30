package mcpevents

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

// VerificationStore holds verification state shared by every API replica:
// the per-(tenant, principal, url) cache and the per-minute counters behind
// the verification limits. Windows are Unix minutes.
type VerificationStore interface {
	// Verified reports whether (tenant, principal, url) passed a challenge
	// within the cache TTL.
	Verified(ctx context.Context, tenantID, principal, url string) (bool, error)
	// MarkVerified caches a passed challenge for ttl.
	MarkVerified(ctx context.Context, tenantID, principal, url string, ttl time.Duration) error
	// PurgeTenant drops every cache entry of tenantID.
	PurgeTenant(ctx context.Context, tenantID string) error
	// CountAttempt counts one uncached verification attempt by (tenant,
	// principal) in window and returns the window's total.
	CountAttempt(ctx context.Context, tenantID, principal string, window int64) (int64, error)
	// FailureCount returns the failed challenges of tenantID to hostport in
	// window.
	FailureCount(ctx context.Context, tenantID, hostport string, window int64) (int64, error)
	// CountFailure counts one failed challenge of tenantID to hostport in
	// window.
	CountFailure(ctx context.Context, tenantID, hostport string, window int64) error
}

// counterTTL keeps a window's counter for its own minute plus slack for
// clock skew between replicas; counters never outlive it.
const counterTTL = 2 * time.Minute

// RedisVerificationStore is the Redis VerificationStore. Every key is
// single-key (cluster safe) and carries the deployment prefix:
//
//	[<dep>:]mcp:verified:<sha256(tenant, principal, url)>                (TTL: cache TTL)
//	[<dep>:]mcp:verified_index:<sha256(tenant)>                          (zset of verified keys by expiry)
//	[<dep>:]mcp:challenge_rate:<sha256(tenant, principal)>:<minute>      (TTL: 2m)
//	[<dep>:]mcp:challenge_fail:<sha256(tenant, host:port)>:<minute>      (TTL: 2m)
//
// Hashing (of length-prefixed parts) keeps attacker-chosen strings out of
// key names and bounds their length.
type RedisVerificationStore struct {
	client redis.Cmdable
	prefix string
}

var _ VerificationStore = (*RedisVerificationStore)(nil)

// NewRedisVerificationStore returns a store on client; deploymentID ("" for
// none) prefixes every key.
func NewRedisVerificationStore(client redis.Cmdable, deploymentID string) *RedisVerificationStore {
	prefix := ""
	if deploymentID != "" {
		prefix = deploymentID + ":"
	}
	return &RedisVerificationStore{client: client, prefix: prefix}
}

// hashParts hashes length-prefixed parts, so no choice of tenant, principal
// or URL bytes (NUL included) can make two tuples collide.
func hashParts(parts ...string) string {
	h := sha256.New()
	var n [8]byte
	for _, p := range parts {
		binary.BigEndian.PutUint64(n[:], uint64(len(p)))
		h.Write(n[:])
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *RedisVerificationStore) verifiedKey(tenantID, principal, url string) string {
	return s.prefix + "mcp:verified:" + hashParts(tenantID, principal, url)
}

func (s *RedisVerificationStore) indexKey(tenantID string) string {
	return s.prefix + "mcp:verified_index:" + hashParts(tenantID)
}

func (s *RedisVerificationStore) attemptKey(tenantID, principal string, window int64) string {
	return s.prefix + "mcp:challenge_rate:" + hashParts(tenantID, principal) + ":" + strconv.FormatInt(window, 10)
}

func (s *RedisVerificationStore) tenantFailureKey(tenantID, hostport string, window int64) string {
	return s.prefix + "mcp:challenge_fail:" + hashParts(tenantID, hostport) + ":" + strconv.FormatInt(window, 10)
}

func (s *RedisVerificationStore) Verified(ctx context.Context, tenantID, principal, url string) (bool, error) {
	n, err := s.client.Exists(ctx, s.verifiedKey(tenantID, principal, url)).Result()
	if err != nil {
		return false, fmt.Errorf("mcpevents: read verification cache: %w", err)
	}
	return n > 0, nil
}

func (s *RedisVerificationStore) MarkVerified(ctx context.Context, tenantID, principal, url string, ttl time.Duration) error {
	key := s.verifiedKey(tenantID, principal, url)
	now := time.Now()
	// The value records when the challenge passed, for debugging.
	if err := s.client.Set(ctx, key, strconv.FormatInt(now.UnixMilli(), 10), ttl).Err(); err != nil {
		return fmt.Errorf("mcpevents: write verification cache: %w", err)
	}
	// The index lets PurgeTenant find the tenant's entries. Entries past
	// their expiry (with a minute of slack) are pruned on every write, so it
	// holds only live ones.
	index := s.indexKey(tenantID)
	pipe := s.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, index, "-inf", strconv.FormatInt(now.Add(-time.Minute).UnixMilli(), 10))
	pipe.ZAdd(ctx, index, redis.Z{Score: float64(now.Add(ttl).UnixMilli()), Member: key})
	pipe.PExpire(ctx, index, ttl+time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("mcpevents: index verification cache: %w", err)
	}
	return nil
}

func (s *RedisVerificationStore) PurgeTenant(ctx context.Context, tenantID string) error {
	index := s.indexKey(tenantID)
	keys, err := s.client.ZRange(ctx, index, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("mcpevents: read verification index: %w", err)
	}
	for _, key := range keys {
		if err := s.client.Del(ctx, key).Err(); err != nil {
			return fmt.Errorf("mcpevents: purge verification cache: %w", err)
		}
	}
	if err := s.client.Del(ctx, index).Err(); err != nil {
		return fmt.Errorf("mcpevents: purge verification cache: %w", err)
	}
	return nil
}

func (s *RedisVerificationStore) CountAttempt(ctx context.Context, tenantID, principal string, window int64) (int64, error) {
	return s.incr(ctx, s.attemptKey(tenantID, principal, window))
}

func (s *RedisVerificationStore) FailureCount(ctx context.Context, tenantID, hostport string, window int64) (int64, error) {
	n, err := s.client.Get(ctx, s.tenantFailureKey(tenantID, hostport, window)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("mcpevents: read verification failures: %w", err)
	}
	return n, nil
}

func (s *RedisVerificationStore) CountFailure(ctx context.Context, tenantID, hostport string, window int64) error {
	_, err := s.incr(ctx, s.tenantFailureKey(tenantID, hostport, window))
	return err
}

// incr increments a window counter and sets its TTL in one MULTI, so a
// counter never exists without an expiry.
func (s *RedisVerificationStore) incr(ctx context.Context, key string) (int64, error) {
	pipe := s.client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, counterTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("mcpevents: count verification: %w", err)
	}
	return incr.Val(), nil
}
