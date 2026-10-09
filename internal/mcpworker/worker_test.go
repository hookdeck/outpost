package mcpworker

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/redislock"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runWorker runs w until the returned stop is called, which fails the test
// unless Run returns nil promptly.
func runWorker(t *testing.T, w *Worker) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	return func() {
		t.Helper()
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("Run did not return promptly after cancellation")
		}
	}
}

func fastWorker(c *Config) {
	c.Interval = 20 * time.Millisecond
	c.HeartbeatInterval = 10 * time.Millisecond
	c.Now = nil // real time
}

func TestNewValidates(t *testing.T) {
	h := newHarness(t)
	for name, mutate := range map[string]func(*Config){
		"no redis":    func(c *Config) { c.Redis = nil },
		"no store":    func(c *Config) { c.Store = nil },
		"no notifier": func(c *Config) { c.Notifier = nil },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := h.cfg
			mutate(&cfg)
			_, err := New(cfg)
			require.Error(t, err)
		})
	}

	w, err := New(Config{Redis: h.rdb, Store: h.store, Notifier: h.notifier})
	require.NoError(t, err)
	assert.Equal(t, Name, w.Name())
	assert.Equal(t, "mcp-subscriptions", w.Name())
	assert.Equal(t, DefaultInterval, w.cfg.Interval)
	assert.Equal(t, DefaultGrace, w.cfg.Grace)
	assert.Equal(t, DefaultTTLMax, w.cfg.TTLMax)
	assert.Equal(t, topicschema.DefaultHeartbeatInterval, w.cfg.HeartbeatInterval)
	assert.Equal(t, topicschema.DefaultHeartbeatTTL, w.cfg.HeartbeatTTL)
	assert.Equal(t, DefaultConcurrency, w.cfg.Concurrency)
	assert.Equal(t, DefaultBatchSize, w.cfg.BatchSize)
	assert.Equal(t, DefaultPassBudget, w.cfg.PassBudget)
	assert.Equal(t, DefaultNotifyWait, w.notifyWait)
	assert.Nil(t, w.cfg.TenantUpdates, "no emitter, no tenant updates")
}

func TestKeys(t *testing.T) {
	assert.Equal(t, "outpost:lock:mcp_worker", LockKey(""))
	assert.Equal(t, "dp_1:outpost:lock:mcp_worker", LockKey("dp_1"))
	assert.Equal(t, "{outpost:topic_schemas}:termination", StateKey(""))
	assert.Equal(t, "dp_1:{outpost:topic_schemas}:termination", StateKey("dp_1"))
}

func TestLiveTopics(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	now := h.clock.Now()
	h.sub(t, "t1", "sub_live", topicA, ptr(now.Add(time.Minute)))
	h.sub(t, "t1", "sub_forever", topicB, nil)
	h.sub(t, "t1", "sub_expired", endedTopic, ptr(now.Add(-time.Second)))

	live := LiveTopics(h.store, h.clock.Now)
	for topic, want := range map[string]bool{topicA: true, topicB: true, endedTopic: false, "unknown": false} {
		got, err := live(ctx, topic)
		require.NoError(t, err)
		assert.Equal(t, want, got, topic)
	}

	// It is what Apply consults.
	v1 := snapshotOf(map[string]string{topicA: schemaV1, endedTopic: schemaV1})
	v2 := snapshotOf(map[string]string{topicA: schemaV1, endedTopic: schemaV2})
	opts := topicschema.ApplyOptions{LiveTopics: live}
	_, err := topicschema.Apply(ctx, h.rdb, "", v1, opts)
	require.NoError(t, err)
	res, err := topicschema.Apply(ctx, h.rdb, "", v2, opts)
	require.NoError(t, err, "endedTopic has no live subscription")
	assert.False(t, res.Forced)
}

func TestJitter(t *testing.T) {
	for range 1000 {
		d := jitter(30 * time.Second)
		assert.GreaterOrEqual(t, d, 27*time.Second)
		assert.LessOrEqual(t, d, 33*time.Second)
	}
}

func TestRunSweepsAndHeartbeats(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, fastWorker)
	h.sub(t, "t1", "sub_a", topicA, ptr(time.Now().Add(-2*time.Minute)))

	stop := runWorker(t, h.w)
	require.Eventually(t, func() bool { return !h.live(t, "t1", "sub_a") }, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		live, err := topicschema.IsLive(ctx, h.rdb, "", h.snapshot.Hash())
		return err == nil && live
	}, 2*time.Second, 5*time.Millisecond)
	stop()

	ttl := h.mr.TTL(topicschema.LiveKey("", h.snapshot.Hash()))
	assert.Greater(t, ttl, 50*time.Second, "the heartbeat lasts HeartbeatTTL")
	assert.False(t, h.mr.Exists(LockKey("")), "the lock is released")
}

func TestRunSkipsPassesWhileAnotherInstanceHoldsTheLock(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.sub(t, "t1", "sub_a", topicA, ptr(h.clock.Now().Add(-2*time.Minute)))

	other := redislock.New(h.rdb, redislock.WithKey(LockKey("")), redislock.WithTTL(time.Minute))
	ok, err := other.AttemptLock(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	_, ran := h.w.runPass(ctx)
	assert.False(t, ran)
	assert.True(t, h.live(t, "t1", "sub_a"))

	_, err = other.Unlock(ctx)
	require.NoError(t, err)
	stats, ran := h.w.runPass(ctx)
	assert.True(t, ran)
	assert.EqualValues(t, 1, stats.Expired)
	assert.False(t, h.mr.Exists(LockKey("")))

	// The lock outlives a pass.
	h2 := newHarness(t, func(c *Config) { c.Interval = 5 * time.Second })
	h2.store.listHook = func(context.Context) {
		h2.store.listHook = nil
		assert.Equal(t, 10*time.Second, h2.mr.TTL(LockKey("")))
	}
	h2.w.runPass(ctx)
}

// panicStore panics in every store call the sweep makes.
type panicStore struct {
	*countingStore
	calls atomic.Int64
}

func (s *panicStore) ListIndexedDestinations(context.Context, string, string, int64, int) ([]tenantstore.IndexedDestination, error) {
	s.calls.Add(1)
	panic("boom")
}

func TestRunRecoversFromPanics(t *testing.T) {
	h := newHarness(t, fastWorker)
	store := &panicStore{countingStore: h.store}
	cfg := h.cfg
	cfg.Store = store
	w, err := New(cfg)
	require.NoError(t, err)

	stop := runWorker(t, w)
	require.Eventually(t, func() bool { return store.calls.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"the worker keeps running passes")
	stop()
	assert.False(t, h.mr.Exists(LockKey("")), "a panicking pass releases the lock")

	t.Run("in a subscription", func(t *testing.T) {
		h := newHarness(t)
		h.sub(t, "t1", "sub_a", topicA, ptr(h.clock.Now().Add(-2*time.Minute)))
		h.sub(t, "t1", "sub_b", topicA, ptr(h.clock.Now().Add(-3*time.Minute)))
		h.store.retrieveHook = func(id string) {
			if id == "sub_b" {
				panic("boom")
			}
		}
		stats, ran := h.w.runPass(context.Background())
		h.store.retrieveHook = nil
		assert.True(t, ran)
		assert.EqualValues(t, 1, stats.Errors)
		assert.EqualValues(t, 1, stats.Expired)
		assert.True(t, h.live(t, "t1", "sub_b"))
	})
}

func TestRunStopsPromptly(t *testing.T) {
	h := newHarness(t, fastWorker)
	h.sub(t, "t1", "sub_a", topicA, ptr(time.Now().Add(-2*time.Minute)))
	entered := make(chan struct{})
	var once atomic.Bool
	h.store.listHook = func(ctx context.Context) {
		if once.CompareAndSwap(false, true) {
			close(entered)
		}
		<-ctx.Done() // a slow store call
	}
	stop := runWorker(t, h.w)
	<-entered
	stop()
	assert.True(t, h.live(t, "t1", "sub_a"), "nothing started after cancellation")
}

func TestPassStopsAtItsDeadline(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Interval = 200 * time.Millisecond })
	h.apply(t, h.snapshot, false)
	for i := range 3 {
		h.sub(t, "t1", fmt.Sprintf("sub_%d", i), endedTopic, nil)
	}
	// Terminated envelopes never find room: each waits at most the
	// interval, and the pass ends anyway.
	h.notifier.gate = make(chan struct{})
	start := time.Now()
	stats := h.pass(t)
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.EqualValues(t, 3, stats.Ended, "deleted subscriptions still count")
	assert.EqualValues(t, 3, stats.NotifyFailed)
}

// runRedisStoreScenario runs a pass on the Redis tenant store (its Lua
// scripts and indexes) covering every flow: expiry, a refreshed entry, an
// ended topic and a forced breaking change.
func runRedisStoreScenario(t *testing.T, mr *miniredis.Miniredis, rdb internalredis.Client, dep string) {
	ctx := context.Background()
	v1 := snapshotOf(map[string]string{topicA: schemaV1, topicB: schemaV1, endedTopic: schemaV1})
	v2 := snapshotOf(map[string]string{topicA: schemaV2, topicB: schemaV1})
	h := newHarnessOn(t, mr, rdb, func(rdb internalredis.Client) tenantstore.TenantStore {
		s := tenantstore.New(tenantstore.Config{
			RedisClient:  rdb,
			Secret:       "test-secret-test-secret-test-sec",
			IndexedTypes: []string{models.DestinationTypeMCP},
			DeploymentID: dep,
		})
		require.NoError(t, s.Init(ctx))
		return s
	}, func(c *Config) {
		c.DeploymentID = dep
		c.Snapshot = v2
	})
	h.apply(t, v1, false)
	h.apply(t, v2, true) // ends endedTopic, breaks topicA
	h.clock.Add(time.Second)

	now := h.clock.Now()
	oldHash := withSchemaHash(v1.TopicHash(topicA))
	h.sub(t, "t1", "sub_expired", topicA, ptr(now.Add(-2*time.Minute)))
	h.sub(t, "t1", "sub_live", topicA, ptr(now.Add(time.Hour)))
	h.sub(t, "t1", "sub_broken", topicA, ptr(now.Add(time.Hour)), oldHash)
	h.sub(t, "t2", "sub_ended", endedTopic, nil)
	stale := h.sub(t, "t2", "sub_stale", topicB, ptr(now.Add(time.Hour)))
	require.NoError(t, h.store.RescoreIndexedDestination(ctx, models.DestinationTypeMCP, stale.Topics,
		tenantstore.IndexedDestination{TenantID: "t2", DestinationID: "sub_stale", Score: stale.ExpiresAt.UnixMilli()},
		now.Add(-time.Hour).UnixMilli()))

	stats := h.pass(t)
	assert.Equal(t, PassStats{Expired: 1, Ended: 1, SchemaChanged: 1, Rescored: 1}, stats)
	assert.False(t, h.live(t, "t1", "sub_expired"))
	assert.False(t, h.live(t, "t1", "sub_broken"))
	assert.False(t, h.live(t, "t2", "sub_ended"))
	assert.True(t, h.live(t, "t1", "sub_live"))
	assert.True(t, h.live(t, "t2", "sub_stale"))
	assert.Equal(t, map[string]int64{
		"sub_live":  now.Add(time.Hour).UnixMilli(),
		"sub_stale": now.Add(time.Hour).UnixMilli(),
	}, h.indexed(t, ""))
	assert.Empty(t, h.indexed(t, endedTopic))
	assert.Equal(t, map[string]int64{"sub_live": now.Add(time.Hour).UnixMilli()}, h.indexed(t, topicA))

	terms := map[string]mcpevents.Termination{}
	for _, term := range h.notifier.list() {
		terms[term.SubscriptionID] = term
	}
	require.Len(t, terms, 2)
	assert.Equal(t, []mcpevents.Secret{{Key: testKey}}, terms["sub_ended"].Secrets, "credentials are decrypted")
	assert.Equal(t, mcpevents.KindNotFound, terms["sub_ended"].Error.Kind)
	assert.Equal(t, mcpevents.KindUnsupported, terms["sub_broken"].Error.Kind)
	require.Len(t, h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired), 1)
	assert.Len(t, h.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated), 2)

	n, err := rdb.Exists(ctx, StateKey(dep)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the scan state is kept under the deployment's key")

	h.clock.Add(time.Second)
	assert.Equal(t, PassStats{}, h.pass(t))
	assert.Len(t, h.notifier.list(), 2)
}

func TestWorkerOnRedisTenantStore_Miniredis(t *testing.T) {
	for _, dep := range []string{"", "dp_test_001"} {
		t.Run("deployment="+dep, func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			runRedisStoreScenario(t, mr, rdb, dep)
		})
	}
}

func TestWorkerOnRedisTenantStore_RedisStack(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runRedisStoreIntegration(t, testinfra.NewRedisStackConfig(t))
}

func TestWorkerOnRedisTenantStore_Dragonfly(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runRedisStoreIntegration(t, testinfra.NewDragonflyConfig(t))
}

func runRedisStoreIntegration(t *testing.T, cfg *internalredis.RedisConfig) {
	for _, dep := range []string{"", "dp_test_001"} {
		t.Run("deployment="+dep, func(t *testing.T) {
			client, err := internalredis.New(context.Background(), cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			require.NoError(t, client.FlushDB(context.Background()).Err())
			runRedisStoreScenario(t, nil, client, dep)
		})
	}
}
