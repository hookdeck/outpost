package mcpevents

import (
	"context"
	"strings"
	"testing"
	"time"

	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runVerificationStoreSuite exercises a RedisVerificationStore on client.
// fastForward moves the server clock (miniredis) or sleeps (real servers).
func runVerificationStoreSuite(t *testing.T, client internalredis.Cmdable, deploymentID string, fastForward func(time.Duration)) {
	ctx := context.Background()
	s := NewRedisVerificationStore(client, deploymentID)
	const url = "https://receiver.example.com/hook"

	t.Run("verified cache", func(t *testing.T) {
		ok, err := s.Verified(ctx, "t1", "p1", url)
		require.NoError(t, err)
		assert.False(t, ok)

		require.NoError(t, s.MarkVerified(ctx, "t1", "p1", url, 2*time.Second))
		ok, err = s.Verified(ctx, "t1", "p1", url)
		require.NoError(t, err)
		assert.True(t, ok)

		// Keyed on the whole (tenant, principal, url).
		for _, k := range [][3]string{{"t2", "p1", url}, {"t1", "p2", url}, {"t1", "p1", url + "x"}, {"t1p", "1", url}, {"t1", "p1\x00" + url, ""}} {
			ok, err := s.Verified(ctx, k[0], k[1], k[2])
			require.NoError(t, err)
			assert.False(t, ok, "%q", k)
		}

		// NUL bytes can't shift a boundary between parts.
		require.NoError(t, s.MarkVerified(ctx, "a\x00b", "c", url, time.Minute))
		ok, err = s.Verified(ctx, "a", "b\x00c", url)
		require.NoError(t, err)
		assert.False(t, ok)

		ttl, err := client.TTL(ctx, s.verifiedKey("t1", "p1", url)).Result()
		require.NoError(t, err)
		assert.Greater(t, ttl, time.Duration(0))
		assert.LessOrEqual(t, ttl, 2*time.Second)

		fastForward(3 * time.Second)
		ok, err = s.Verified(ctx, "t1", "p1", url)
		require.NoError(t, err)
		assert.False(t, ok, "expired")
	})

	t.Run("attempt counters", func(t *testing.T) {
		for want := int64(1); want <= 3; want++ {
			n, err := s.CountAttempt(ctx, "t1", "p1", 1000)
			require.NoError(t, err)
			assert.Equal(t, want, n)
		}
		n, err := s.CountAttempt(ctx, "t1", "p1", 1001)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n, "a new window")
		n, err = s.CountAttempt(ctx, "t1", "p2", 1000)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n, "another principal")
		n, err = s.CountAttempt(ctx, "t2", "p1", 1000)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n, "another tenant")

		ttl, err := client.TTL(ctx, s.attemptKey("t1", "p1", 1000)).Result()
		require.NoError(t, err)
		assert.Greater(t, ttl, time.Duration(0), "INCR and EXPIRE are atomic: a counter always expires")
		assert.LessOrEqual(t, ttl, counterTTL)
	})

	t.Run("failure counters", func(t *testing.T) {
		const host = "receiver.example.com:443"
		counts := func(tenantID, hostport string, window int64) (int64, int64) {
			t.Helper()
			tenant, unanswered, err := s.FailureCounts(ctx, tenantID, hostport, window)
			require.NoError(t, err)
			return tenant, unanswered
		}
		tenant, unanswered := counts("t1", host, 2000)
		assert.Zero(t, tenant)
		assert.Zero(t, unanswered)

		// An answered failure counts for the tenant only; an unanswered
		// one for the tenant and the host.
		require.NoError(t, s.CountFailure(ctx, "t1", host, 2000, false))
		require.NoError(t, s.CountFailure(ctx, "t1", host, 2000, true))
		require.NoError(t, s.CountFailure(ctx, "t2", host, 2000, true))
		tenant, unanswered = counts("t1", host, 2000)
		assert.Equal(t, int64(2), tenant)
		assert.Equal(t, int64(2), unanswered, "unanswered challenges from every tenant")
		tenant, _ = counts("t2", host, 2000)
		assert.Equal(t, int64(1), tenant)
		tenant, unanswered = counts("t3", host, 2000)
		assert.Zero(t, tenant, "another tenant")
		assert.Equal(t, int64(2), unanswered)
		tenant, unanswered = counts("t1", "receiver.example.com:8443", 2000)
		assert.Zero(t, tenant, "another port")
		assert.Zero(t, unanswered)
		tenant, unanswered = counts("t1", host, 2001)
		assert.Zero(t, tenant, "a new window")
		assert.Zero(t, unanswered)

		for _, key := range []string{s.tenantFailureKey("t1", host, 2000), s.hostFailureKey(host, 2000)} {
			ttl, err := client.TTL(ctx, key).Result()
			require.NoError(t, err)
			assert.Greater(t, ttl, time.Duration(0))
			assert.LessOrEqual(t, ttl, counterTTL)
		}
	})

	t.Run("verifying claim", func(t *testing.T) {
		release, ok, err := s.ClaimVerification(ctx, "t1", "p1", url, 2*time.Second)
		require.NoError(t, err)
		require.True(t, ok)
		_, ok, err = s.ClaimVerification(ctx, "t1", "p1", url, 2*time.Second)
		require.NoError(t, err)
		assert.False(t, ok, "held")
		otherRelease, ok, err := s.ClaimVerification(ctx, "t1", "p2", url, 2*time.Second)
		require.NoError(t, err)
		assert.True(t, ok, "keyed on the whole (tenant, principal, url)")
		otherRelease()

		release()
		release2, ok, err := s.ClaimVerification(ctx, "t1", "p1", url, time.Second)
		require.NoError(t, err)
		require.True(t, ok, "released")

		// A claim that outlived its TTL and was taken over is not
		// released by its first holder.
		fastForward(2 * time.Second)
		release3, ok, err := s.ClaimVerification(ctx, "t1", "p1", url, 2*time.Second)
		require.NoError(t, err)
		require.True(t, ok, "expired")
		release2()
		_, ok, err = s.ClaimVerification(ctx, "t1", "p1", url, 2*time.Second)
		require.NoError(t, err)
		assert.False(t, ok, "still held by the new holder")
		release3()
	})

	t.Run("key names", func(t *testing.T) {
		require.NoError(t, s.MarkVerified(ctx, "tenant_secret", "principal_secret", url, time.Minute))
		_, err := s.CountAttempt(ctx, "tenant_secret", "principal_secret", 3000)
		require.NoError(t, err)
		require.NoError(t, s.CountFailure(ctx, "tenant_secret", "receiver.example.com:443", 3000, true))
		_, ok, err := s.ClaimVerification(ctx, "tenant_secret", "principal_secret", url, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)

		keys, err := client.Keys(ctx, "*").Result()
		require.NoError(t, err)
		require.NotEmpty(t, keys)
		for _, k := range keys {
			if deploymentID != "" {
				assert.True(t, strings.HasPrefix(k, deploymentID+":mcp:"), k)
			} else {
				assert.True(t, strings.HasPrefix(k, "mcp:"), k)
			}
			// Attacker-chosen strings never appear in key names.
			assert.NotContains(t, k, "secret")
			assert.NotContains(t, k, "receiver")
			assert.NotContains(t, k, ":destination:")
		}
	})
}

func TestRedisVerificationStore_Miniredis(t *testing.T) {
	t.Parallel()
	for _, dep := range []string{"", "dp_test_001"} {
		t.Run("deployment="+dep, func(t *testing.T) {
			t.Parallel()
			mr, client := newMiniredis(t)
			runVerificationStoreSuite(t, client, dep, mr.FastForward)
		})
	}
}

// The Redis Stack and Dragonfly runs of this suite are in
// verifystore_integration_test.go, an external test package: testinfra
// imports the destination providers, which import this package, so tests
// inside the package can't use it.
