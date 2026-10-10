package mcpevents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

// VerificationStore holds verification state shared by every API replica:
// the per-(tenant, principal, url) cache, the claim that lets one replica at
// a time challenge a (tenant, principal, url), and the per-minute counters
// behind the verification limits. Windows are Unix minutes.
type VerificationStore interface {
	// Verified reports whether (tenant, principal, url) passed a challenge
	// within the cache TTL.
	Verified(ctx context.Context, tenantID, principal, url string) (bool, error)
	// MarkVerified caches a passed challenge for ttl.
	MarkVerified(ctx context.Context, tenantID, principal, url string, ttl time.Duration) error
	// ClaimVerification takes the claim on challenging (tenant, principal,
	// url) for up to ttl; ok is false when another caller holds it. release
	// gives up a claim this call took, and is a no-op once the claim has
	// expired, even if someone else took it since.
	ClaimVerification(ctx context.Context, tenantID, principal, url string, ttl time.Duration) (release func(), ok bool, err error)
	// CountAttempt counts one uncached verification attempt by (tenant,
	// principal) in window and returns the window's total.
	CountAttempt(ctx context.Context, tenantID, principal string, window int64) (int64, error)
	// FailureCounts returns the failed challenges of tenantID to hostport and
	// the unanswered challenges to hostport from every tenant, in window.
	FailureCounts(ctx context.Context, tenantID, hostport string, window int64) (tenant, unanswered int64, err error)
	// CountFailure counts one failed challenge of tenantID to hostport in
	// window, and, when unanswered (timeout, connection refused, TLS
	// failure), one against hostport for every tenant.
	CountFailure(ctx context.Context, tenantID, hostport string, window int64, unanswered bool) error
}

// counterTTL keeps a window's counter for its own minute plus slack for
// clock skew between replicas; counters never outlive it.
const counterTTL = 2 * time.Minute

// claimReleaseTimeout bounds releasing a claim, which runs whether or not the
// caller's context is done.
const claimReleaseTimeout = 5 * time.Second

// RedisVerificationStore is the Redis VerificationStore. Every key is
// single-key (cluster safe) and carries the deployment prefix:
//
//	[<dep>:]mcp:verified:<sha256(tenant, principal, url)>                (TTL: cache TTL)
//	[<dep>:]mcp:verifying:<sha256(tenant, principal, url)>               (TTL: one challenge)
//	[<dep>:]mcp:challenge_rate:p:<sha256(tenant, principal)>:<minute>    (TTL: 2m)
//	[<dep>:]mcp:challenge_fail:t:<sha256(tenant, host:port)>:<minute>    (TTL: 2m)
//	[<dep>:]mcp:challenge_fail:h:<sha256(host:port)>:<minute>            (TTL: 2m)
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

func (s *RedisVerificationStore) claimKey(tenantID, principal, url string) string {
	return s.prefix + "mcp:verifying:" + hashParts(tenantID, principal, url)
}

func (s *RedisVerificationStore) attemptKey(tenantID, principal string, window int64) string {
	return s.prefix + "mcp:challenge_rate:p:" + hashParts(tenantID, principal) + ":" + strconv.FormatInt(window, 10)
}

func (s *RedisVerificationStore) tenantFailureKey(tenantID, hostport string, window int64) string {
	return s.prefix + "mcp:challenge_fail:t:" + hashParts(tenantID, hostport) + ":" + strconv.FormatInt(window, 10)
}

func (s *RedisVerificationStore) hostFailureKey(hostport string, window int64) string {
	return s.prefix + "mcp:challenge_fail:h:" + hashParts(hostport) + ":" + strconv.FormatInt(window, 10)
}

func (s *RedisVerificationStore) Verified(ctx context.Context, tenantID, principal, url string) (bool, error) {
	n, err := s.client.Exists(ctx, s.verifiedKey(tenantID, principal, url)).Result()
	if err != nil {
		return false, fmt.Errorf("mcpevents: read verification cache: %w", err)
	}
	return n > 0, nil
}

func (s *RedisVerificationStore) MarkVerified(ctx context.Context, tenantID, principal, url string, ttl time.Duration) error {
	// The value records when the challenge passed, for debugging.
	at := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := s.client.Set(ctx, s.verifiedKey(tenantID, principal, url), at, ttl).Err(); err != nil {
		return fmt.Errorf("mcpevents: write verification cache: %w", err)
	}
	return nil
}

// releaseClaimScript deletes a claim only while it holds the releasing
// caller's token. KEYS[1]: the claim. ARGV[1]: the token.
const releaseClaimScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`

func (s *RedisVerificationStore) ClaimVerification(ctx context.Context, tenantID, principal, url string, ttl time.Duration) (func(), bool, error) {
	key := s.claimKey(tenantID, principal, url)
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails
	token := hex.EncodeToString(b[:])
	ok, err := s.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, fmt.Errorf("mcpevents: claim verification: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	return func() {
		// Best effort: a claim that isn't released expires with its TTL.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimReleaseTimeout)
		defer cancel()
		_ = s.client.Eval(ctx, releaseClaimScript, []string{key}, token).Err()
	}, true, nil
}

func (s *RedisVerificationStore) CountAttempt(ctx context.Context, tenantID, principal string, window int64) (int64, error) {
	return s.incr(ctx, s.attemptKey(tenantID, principal, window))
}

func (s *RedisVerificationStore) FailureCounts(ctx context.Context, tenantID, hostport string, window int64) (int64, int64, error) {
	// Two keys in different slots: pipelined GETs, not MGET.
	var tenant, host *redis.StringCmd
	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		tenant = pipe.Get(ctx, s.tenantFailureKey(tenantID, hostport, window))
		host = pipe.Get(ctx, s.hostFailureKey(hostport, window))
		return nil
	})
	if err != nil && err != redis.Nil {
		return 0, 0, fmt.Errorf("mcpevents: read verification failures: %w", err)
	}
	t, err := counterValue(tenant)
	if err != nil {
		return 0, 0, err
	}
	h, err := counterValue(host)
	if err != nil {
		return 0, 0, err
	}
	return t, h, nil
}

func counterValue(cmd *redis.StringCmd) (int64, error) {
	n, err := cmd.Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("mcpevents: read verification failures: %w", err)
	}
	return n, nil
}

func (s *RedisVerificationStore) CountFailure(ctx context.Context, tenantID, hostport string, window int64, unanswered bool) error {
	if _, err := s.incr(ctx, s.tenantFailureKey(tenantID, hostport, window)); err != nil {
		return err
	}
	if unanswered {
		if _, err := s.incr(ctx, s.hostFailureKey(hostport, window)); err != nil {
			return err
		}
	}
	return nil
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
