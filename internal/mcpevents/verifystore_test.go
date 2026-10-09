package mcpevents

import (
	"context"
	"strings"
	"testing"
	"time"

	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
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

	t.Run("host failure counters", func(t *testing.T) {
		n, err := s.HostFailures(ctx, "receiver.example.com:443", 2000)
		require.NoError(t, err)
		assert.Zero(t, n)
		for want := int64(1); want <= 2; want++ {
			n, err := s.CountHostFailure(ctx, "receiver.example.com:443", 2000)
			require.NoError(t, err)
			assert.Equal(t, want, n)
		}
		n, err = s.HostFailures(ctx, "receiver.example.com:443", 2000)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = s.HostFailures(ctx, "receiver.example.com:8443", 2000)
		require.NoError(t, err)
		assert.Zero(t, n)
		n, err = s.HostFailures(ctx, "receiver.example.com:443", 2001)
		require.NoError(t, err)
		assert.Zero(t, n)

		ttl, err := client.TTL(ctx, s.failureKey("receiver.example.com:443", 2000)).Result()
		require.NoError(t, err)
		assert.Greater(t, ttl, time.Duration(0))
	})

	t.Run("key names", func(t *testing.T) {
		require.NoError(t, s.MarkVerified(ctx, "tenant_secret", "principal_secret", url, time.Minute))
		_, err := s.CountAttempt(ctx, "tenant_secret", "principal_secret", 3000)
		require.NoError(t, err)
		_, err = s.CountHostFailure(ctx, "receiver.example.com:443", 3000)
		require.NoError(t, err)

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

func TestRedisVerificationStore_RedisStack(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runVerificationStoreIntegration(t, testinfra.NewRedisStackConfig(t))
}

func TestRedisVerificationStore_Dragonfly(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runVerificationStoreIntegration(t, testinfra.NewDragonflyConfig(t))
}

func runVerificationStoreIntegration(t *testing.T, cfg *internalredis.RedisConfig) {
	for _, dep := range []string{"", "dp_test_001"} {
		t.Run("deployment="+dep, func(t *testing.T) {
			client, err := internalredis.New(context.Background(), cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			require.NoError(t, client.FlushDB(context.Background()).Err())
			runVerificationStoreSuite(t, client, dep, time.Sleep)
		})
	}
}
