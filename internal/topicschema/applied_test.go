package topicschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/logging"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/redislock"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	appliedTopic = "order.created"
	otherTopic   = "order.shipped"

	schemaV1 = `{"type":"object","properties":{"id":{"type":"string"},"total":{"type":"number"}}}`
	// schemaV2 adds currency to schemaV1.
	schemaV2 = `{"type":"object","properties":{"id":{"type":"string"},"total":{"type":"number"},"currency":{"type":"string"}}}`
	// schemaV3 removes total from schemaV2.
	schemaV3 = `{"type":"object","properties":{"id":{"type":"string"},"currency":{"type":"string"}}}`
)

// mcpTopics builds a snapshot of MCP-enabled topics.
func mcpTopics(schemas map[string]string) Snapshot {
	s := Snapshot{Topics: map[string]SnapshotTopic{}}
	for name, schema := range schemas {
		s.Topics[name] = SnapshotTopic{MCPEnabled: true, PayloadSchema: json.RawMessage(schema)}
	}
	return s
}

func oneTopic(schema string) Snapshot {
	return mcpTopics(map[string]string{appliedTopic: schema})
}

var appliedNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// applyEnv is one isolated deployment on a Redis server.
type applyEnv struct {
	rdb         internalredis.Cmdable
	dep         string
	fastForward func(time.Duration)
	liveCalls   atomic.Int64
	live        map[string]bool // topic -> has live subscriptions; nil = all live
}

func (e *applyEnv) opts(allowBreaking bool) ApplyOptions {
	return ApplyOptions{
		AllowBreaking: allowBreaking,
		Now:           func() time.Time { return appliedNow },
		LockWait:      2 * time.Second,
		LiveTopics: func(_ context.Context, topic string) (bool, error) {
			e.liveCalls.Add(1)
			if e.live == nil {
				return true, nil
			}
			return e.live[topic], nil
		},
	}
}

func (e *applyEnv) apply(t *testing.T, s Snapshot, allowBreaking bool) (ApplyResult, error) {
	t.Helper()
	return Apply(context.Background(), e.rdb, e.dep, s, e.opts(allowBreaking))
}

func (e *applyEnv) mustApply(t *testing.T, s Snapshot, allowBreaking bool) ApplyResult {
	t.Helper()
	res, err := e.apply(t, s, allowBreaking)
	require.NoError(t, err)
	return res
}

func (e *applyEnv) applied(t *testing.T) *Applied {
	t.Helper()
	a, err := ReadApplied(context.Background(), e.rdb, e.dep)
	require.NoError(t, err)
	return a
}

func (e *applyEnv) history(t *testing.T) []HistoryEntry {
	t.Helper()
	h, err := ReadHistory(context.Background(), e.rdb, e.dep)
	require.NoError(t, err)
	return h
}

func (e *applyEnv) rawApplied(t *testing.T) string {
	t.Helper()
	raw, err := e.rdb.Get(context.Background(), AppliedKey(e.dep)).Result()
	if errors.Is(err, goredis.Nil) {
		return ""
	}
	require.NoError(t, err)
	return raw
}

func runApplySuite(t *testing.T, rdb internalredis.Cmdable, fastForward func(time.Duration)) {
	var n atomic.Int64
	newEnv := func() *applyEnv {
		return &applyEnv{rdb: rdb, dep: fmt.Sprintf("dep%d", n.Add(1)), fastForward: fastForward}
	}
	ctx := context.Background()
	v1, v2, v3 := oneTopic(schemaV1), oneTopic(schemaV2), oneTopic(schemaV3)
	removedTotal := Change{Topic: appliedTopic, Path: "/properties/total", Kind: ChangePropertyRemoved, Detail: "property removed"}

	t.Run("initial write", func(t *testing.T) {
		e := newEnv()
		res := e.mustApply(t, v1, false)
		assert.Equal(t, ApplyResult{Hash: v1.Hash(), Initial: true}, res)
		assert.Zero(t, e.liveCalls.Load(), "nothing to compare against")

		a := e.applied(t)
		assert.Equal(t, v1.Hash(), a.Hash)
		assert.Equal(t, v1.Hash(), a.Snapshot.Hash(), "the snapshot round-trips")
		assert.True(t, a.AppliedAt.Equal(appliedNow))
		assert.Nil(t, a.Broken)
		assert.Nil(t, a.Superseded)
		h := e.history(t)
		require.Len(t, h, 1)
		assert.Equal(t, v1.Hash(), h[0].Hash)
		assert.True(t, h[0].AppliedAt.Equal(appliedNow))
	})

	t.Run("unchanged is a no-op, even while the lock is held", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v1, false)
		raw := e.rawApplied(t)

		lock := redislock.New(rdb, redislock.WithKey(ApplyLockKey(e.dep)), redislock.WithTTL(10*time.Second))
		ok, err := lock.AttemptLock(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		defer func() { _, _ = lock.Unlock(ctx) }()

		res := e.mustApply(t, v1, false)
		assert.Equal(t, ApplyResult{Hash: v1.Hash(), Unchanged: true}, res)
		assert.Equal(t, raw, e.rawApplied(t))
		assert.Len(t, e.history(t), 1)
	})

	t.Run("additive change moves the baseline", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v1, false)
		res := e.mustApply(t, v2, false)
		assert.Equal(t, ApplyResult{Hash: v2.Hash()}, res)
		assert.Zero(t, e.liveCalls.Load(), "only topics with breaking changes are checked for live subscriptions")

		a := e.applied(t)
		assert.Equal(t, v2.Hash(), a.Hash)
		assert.Nil(t, a.Broken)
		assert.Equal(t, map[string][]string{appliedTopic: {v1.TopicHash(appliedTopic)}}, a.Superseded)
		h := e.history(t)
		require.Len(t, h, 2)
		assert.Equal(t, v2.Hash(), h[0].Hash, "newest first")
		assert.Equal(t, v1.Hash(), h[1].Hash)
	})

	t.Run("breaking change is rejected with the list of changes", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v2, false)
		raw := e.rawApplied(t)

		res, err := e.apply(t, v3, false)
		var bce *BreakingChangeError
		require.ErrorAs(t, err, &bce)
		assert.Equal(t, []Change{removedTotal}, bce.Changes)
		assert.Equal(t, []Change{removedTotal}, res.Changes)
		assert.False(t, res.Forced)
		assert.Equal(t, msgBreakingChanges+"\n  - order.created /properties/total: property_removed (property removed)", err.Error())

		b, err := json.Marshal(bce)
		require.NoError(t, err)
		assert.JSONEq(t, `{"message":`+mustJSON(t, msgBreakingChanges)+`,"data":[
			{"topic":"order.created","path":"/properties/total","kind":"property_removed","detail":"property removed"}]}`, string(b))
		byValue, err := json.Marshal(*bce)
		require.NoError(t, err)
		assert.Equal(t, string(b), string(byValue))
		empty, err := json.Marshal(&BreakingChangeError{Message: "m"})
		require.NoError(t, err)
		assert.JSONEq(t, `{"message":"m","data":[]}`, string(empty))

		assert.Equal(t, raw, e.rawApplied(t), "the baseline is kept")
		assert.Len(t, e.history(t), 1)
	})

	t.Run("nil LiveTopics treats every topic as live", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v2, false)
		opts := e.opts(false)
		opts.LiveTopics = nil
		_, err := Apply(ctx, rdb, e.dep, v3, opts)
		var bce *BreakingChangeError
		require.ErrorAs(t, err, &bce)
	})

	t.Run("topics without live subscriptions are not checked, but their old schemas break", func(t *testing.T) {
		e := newEnv()
		e.live = map[string]bool{}
		e.mustApply(t, v1, false)
		e.mustApply(t, v2, false)
		res := e.mustApply(t, v3, false)
		assert.Equal(t, ApplyResult{Hash: v3.Hash(), Changes: []Change{removedTotal}}, res)
		assert.EqualValues(t, 1, e.liveCalls.Load())
		// Instances still running v2 can create subscriptions to it until
		// the rollout ends: they end like those of a forced change.
		assert.Equal(t, []string{v2.TopicHash(appliedTopic), v1.TopicHash(appliedTopic)}, e.applied(t).BrokenHashes(appliedTopic))
	})

	t.Run("only topics with live subscriptions refuse a breaking change", func(t *testing.T) {
		e := newEnv()
		e.live = map[string]bool{otherTopic: true}
		both := func(schema string) Snapshot {
			return mcpTopics(map[string]string{appliedTopic: schema, otherTopic: schema})
		}
		before, after := both(schemaV2), both(schemaV3)
		e.mustApply(t, before, false)

		_, err := e.apply(t, after, false)
		var bce *BreakingChangeError
		require.ErrorAs(t, err, &bce)
		assert.Equal(t, []Change{{Topic: otherTopic, Path: "/properties/total", Kind: ChangePropertyRemoved, Detail: "property removed"}}, bce.Changes)

		res := e.mustApply(t, after, true)
		assert.True(t, res.Forced)
		assert.Len(t, res.Changes, 2)
		a := e.applied(t)
		assert.Equal(t, []string{before.TopicHash(appliedTopic)}, a.BrokenHashes(appliedTopic))
		assert.Equal(t, []string{before.TopicHash(otherTopic)}, a.BrokenHashes(otherTopic))
	})

	t.Run("topics not MCP-enabled on both sides are not checked", func(t *testing.T) {
		e := newEnv()
		plain := Snapshot{Topics: map[string]SnapshotTopic{appliedTopic: {PayloadSchema: json.RawMessage(schemaV2)}}}
		e.mustApply(t, plain, false)
		res := e.mustApply(t, v3, false)
		assert.Empty(t, res.Changes)
		assert.Zero(t, e.liveCalls.Load())

		// Unchanged schemas are not checked either.
		e.mustApply(t, mcpTopics(map[string]string{appliedTopic: schemaV3, otherTopic: schemaV1}), false)
		assert.Zero(t, e.liveCalls.Load())
	})

	t.Run("LiveTopics failure fails the apply", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v2, false)
		opts := e.opts(false)
		opts.LiveTopics = func(context.Context, string) (bool, error) { return false, errors.New("boom") }
		_, err := Apply(ctx, rdb, e.dep, v3, opts)
		require.ErrorContains(t, err, "boom")
		assert.Equal(t, v2.Hash(), e.applied(t).Hash)
	})

	t.Run("forced breaking change records every old hash of the topic", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v1, false)
		e.mustApply(t, v2, false)
		res := e.mustApply(t, v3, true)
		assert.True(t, res.Forced)
		assert.Equal(t, []Change{removedTotal}, res.Changes)

		a := e.applied(t)
		assert.Equal(t, v3.Hash(), a.Hash)
		h1, h2, h3 := v1.TopicHash(appliedTopic), v2.TopicHash(appliedTopic), v3.TopicHash(appliedTopic)
		assert.Equal(t, map[string][]BrokenHash{appliedTopic: {
			{Hash: h2, RecordedAt: appliedNow},
			{Hash: h1, RecordedAt: appliedNow},
		}}, normalizeBroken(a.Broken))
		assert.Equal(t, []string{h2, h1}, a.BrokenHashes(appliedTopic))
		assert.True(t, a.IsBroken(appliedTopic, h1))
		assert.True(t, a.IsBroken(appliedTopic, h2))
		assert.False(t, a.IsBroken(appliedTopic, h3))
		assert.False(t, a.IsBroken(otherTopic, h1))
		assert.False(t, a.IsBroken(appliedTopic, ""))
		set := a.BrokenSet()
		assert.Equal(t, BrokenSet{appliedTopic: {h1: {}, h2: {}}}, set)
		for _, hash := range []string{h1, h2, h3, ""} {
			assert.Equal(t, a.IsBroken(appliedTopic, hash), set.IsBroken(appliedTopic, hash), hash)
			assert.False(t, set.IsBroken(otherTopic, hash), hash)
		}
		assert.Equal(t, []string{h2, h1}, a.Superseded[appliedTopic])
		assert.Len(t, e.history(t), 3)
	})

	t.Run("no broken hashes", func(t *testing.T) {
		var none *Applied
		assert.Nil(t, none.BrokenSet())
		assert.False(t, none.BrokenSet().IsBroken(appliedTopic, "h"))
		assert.Nil(t, (&Applied{}).BrokenSet())
	})

	t.Run("broken hashes accumulate, and an applied schema is never broken", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v2, false)
		e.mustApply(t, v3, true)
		h2, h3 := v2.TopicHash(appliedTopic), v3.TopicHash(appliedTopic)
		assert.Equal(t, []string{h2}, e.applied(t).BrokenHashes(appliedTopic))

		// Another forced change: v3 (and v2 again) are now broken too.
		v4 := oneTopic(`{"type":"object","properties":{"id":{"type":"integer"}}}`)
		e.mustApply(t, v4, true)
		assert.ElementsMatch(t, []string{h2, h3}, e.applied(t).BrokenHashes(appliedTopic))

		// Forcing v2 back: its subscriptions are valid again.
		e.mustApply(t, v2, true)
		a := e.applied(t)
		assert.ElementsMatch(t, []string{h3, v4.TopicHash(appliedTopic)}, a.BrokenHashes(appliedTopic))
		assert.NotContains(t, a.Superseded[appliedTopic], h2)
	})

	t.Run("a running configuration is accepted without moving the baseline", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v2, false)
		require.NoError(t, Heartbeat(ctx, rdb, e.dep, v1.Hash(), time.Second))
		raw := e.rawApplied(t)

		// v2 -> v1 removes currency, but an instance runs v1. No instance
		// reported v2 yet, but it was just applied: its instances may still
		// be starting.
		res := e.mustApply(t, v1, false)
		assert.True(t, res.KnownConfig)
		assert.False(t, res.Adopted)
		assert.False(t, res.Forced)
		assert.Equal(t, []Change{{Topic: appliedTopic, Path: "/properties/currency", Kind: ChangePropertyRemoved, Detail: "property removed"}},
			res.Changes, "the diff is reported for logging")
		assert.Equal(t, raw, e.rawApplied(t), "the baseline is kept")
		assert.Len(t, e.history(t), 1)

		// Once no instance reports it, it is checked like any change.
		fastForward(1100 * time.Millisecond)
		_, err := e.apply(t, v1, false)
		var bce *BreakingChangeError
		require.ErrorAs(t, err, &bce)

		// A heartbeat of another deployment doesn't count.
		other := newEnv()
		other.mustApply(t, v2, false)
		require.NoError(t, Heartbeat(ctx, rdb, e.dep, v1.Hash(), time.Minute))
		_, err = other.apply(t, v1, false)
		require.ErrorAs(t, err, &bce)
	})

	t.Run("a rollback that outlives the applied configuration becomes the baseline", func(t *testing.T) {
		e := newEnv()
		now := appliedNow
		apply := func(s Snapshot, allowBreaking bool) ApplyResult {
			t.Helper()
			opts := e.opts(allowBreaking)
			opts.Now = func() time.Time { return now }
			opts.HeartbeatTTL = time.Second
			res, err := Apply(ctx, rdb, e.dep, s, opts)
			require.NoError(t, err)
			return res
		}
		heartbeat := func(s Snapshot) {
			t.Helper()
			require.NoError(t, Heartbeat(ctx, rdb, e.dep, s.Hash(), time.Second))
		}
		a, b := v2, v3 // b removes total
		hashA := a.TopicHash(appliedTopic)

		// a runs; b is forced in, and its first instance starts.
		apply(a, false)
		heartbeat(a)
		apply(b, true)
		heartbeat(b)
		require.Equal(t, []string{hashA}, e.applied(t).BrokenHashes(appliedTopic))

		// Rolled back while b still runs: a is accepted, b stays applied.
		now = now.Add(2 * time.Second)
		heartbeat(a)
		heartbeat(b)
		res := apply(a, false)
		assert.True(t, res.KnownConfig)
		assert.False(t, res.Adopted)
		assert.Equal(t, b.Hash(), e.applied(t).Hash)

		// b's instances are gone. Once no instance has reported b for the
		// heartbeat TTL, a, which runs, replaces it without a check, and
		// its schema is no longer broken.
		fastForward(1100 * time.Millisecond)
		heartbeat(a)
		res = apply(a, false)
		assert.True(t, res.KnownConfig)
		assert.True(t, res.Adopted)
		assert.False(t, res.Forced)
		got := e.applied(t)
		assert.Equal(t, a.Hash(), got.Hash)
		assert.Nil(t, got.Broken)
		assert.Equal(t, []string{b.TopicHash(appliedTopic)}, got.Superseded[appliedTopic])
		assert.Equal(t, a.Hash(), e.history(t)[0].Hash)
		assert.True(t, apply(a, false).Unchanged)

		// c adds a property to a: applied as additive, nothing ends.
		c := oneTopic(`{"type":"object","properties":{"id":{"type":"string"},"total":{"type":"number"},"currency":{"type":"string"},"note":{"type":"string"}}}`)
		res = apply(c, false)
		assert.False(t, res.Forced)
		assert.Empty(t, res.Changes)
		assert.Nil(t, e.applied(t).Broken)
	})

	t.Run("ending every MCP-enabled topic is refused unless forced", func(t *testing.T) {
		both := mcpTopics(map[string]string{appliedTopic: schemaV1, otherTopic: schemaV1})

		e := newEnv()
		e.mustApply(t, both, false)
		raw := e.rawApplied(t)
		for name, s := range map[string]Snapshot{
			"empty": {},
			"disabled": {Topics: map[string]SnapshotTopic{
				appliedTopic: {PayloadSchema: json.RawMessage(schemaV1)},
				otherTopic:   {PayloadSchema: json.RawMessage(schemaV1)},
			}},
		} {
			res, err := e.apply(t, s, false)
			var bce *BreakingChangeError
			require.ErrorAs(t, err, &bce, name)
			assert.Equal(t, msgAllTopicsEnded, bce.Message)
			assert.Equal(t, []Change{
				{Topic: appliedTopic, Kind: ChangeTopicEnded, Detail: "no longer MCP-enabled"},
				{Topic: otherTopic, Kind: ChangeTopicEnded, Detail: "no longer MCP-enabled"},
			}, bce.Changes)
			assert.Equal(t, []string{appliedTopic, otherTopic}, res.Ended)
			assert.Equal(t, raw, e.rawApplied(t))
		}

		// Ending some topics is fine.
		res := e.mustApply(t, oneTopic(schemaV1), false)
		assert.Equal(t, []string{otherTopic}, res.Ended)
		assert.False(t, res.Forced)

		// Forced: applied, without broken hashes (ended topics end anyway).
		res = e.mustApply(t, Snapshot{}, true)
		assert.True(t, res.Forced)
		assert.Equal(t, []string{appliedTopic}, res.Ended)
		a := e.applied(t)
		assert.Equal(t, Snapshot{}.Hash(), a.Hash)
		assert.Nil(t, a.Broken)

		// Nothing MCP-enabled before: nothing to guard.
		e2 := newEnv()
		e2.mustApply(t, Snapshot{}, false)
		e2.mustApply(t, both, false)
	})

	t.Run("lock contention", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v1, false)
		lock := redislock.New(rdb, redislock.WithKey(ApplyLockKey(e.dep)), redislock.WithTTL(10*time.Second))
		ok, err := lock.AttemptLock(ctx)
		require.NoError(t, err)
		require.True(t, ok)

		opts := e.opts(false)
		opts.LockWait = 300 * time.Millisecond
		start := time.Now()
		_, err = Apply(ctx, rdb, e.dep, v2, opts)
		require.ErrorContains(t, err, "still held")
		assert.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond)
		assert.Equal(t, v1.Hash(), e.applied(t).Hash)

		// Released while waiting: applied.
		go func() {
			time.Sleep(200 * time.Millisecond)
			_, _ = lock.Unlock(context.Background())
		}()
		res := e.mustApply(t, v2, false)
		assert.Equal(t, ApplyResult{Hash: v2.Hash()}, res)

		// Apply released its lock.
		ok, err = lock.AttemptLock(ctx)
		require.NoError(t, err)
		assert.True(t, ok)
		_, _ = lock.Unlock(ctx)
	})

	t.Run("concurrent applies write once", func(t *testing.T) {
		e := newEnv()
		e.mustApply(t, v1, false)
		var wg sync.WaitGroup
		results := make([]ApplyResult, 8)
		errs := make([]error, 8)
		for i := range results {
			wg.Go(func() { results[i], errs[i] = e.apply(t, v2, false) })
		}
		wg.Wait()
		moved := 0
		for i := range results {
			require.NoError(t, errs[i])
			if !results[i].Unchanged {
				moved++
			}
		}
		assert.Equal(t, 1, moved)
		assert.Len(t, e.history(t), 2)
	})

	t.Run("Plan writes nothing", func(t *testing.T) {
		e := newEnv()
		res, err := Plan(ctx, rdb, e.dep, v1, e.opts(false))
		require.NoError(t, err)
		assert.True(t, res.Initial)
		assert.Nil(t, e.applied(t))

		e.mustApply(t, v2, false)
		raw := e.rawApplied(t)
		res, err = Plan(ctx, rdb, e.dep, v3, e.opts(false))
		var bce *BreakingChangeError
		require.ErrorAs(t, err, &bce)
		assert.Equal(t, []Change{removedTotal}, res.Changes)

		res, err = Plan(ctx, rdb, e.dep, v3, e.opts(true))
		require.NoError(t, err)
		assert.True(t, res.Forced)
		assert.Equal(t, raw, e.rawApplied(t))
		assert.Len(t, e.history(t), 1)
	})

	t.Run("history keeps the last 50", func(t *testing.T) {
		e := newEnv()
		var last Snapshot
		for i := range HistoryLimit + 5 {
			last = mcpTopics(map[string]string{fmt.Sprintf("topic.%d", i): schemaV1, appliedTopic: schemaV1})
			e.mustApply(t, last, false)
		}
		h := e.history(t)
		require.Len(t, h, HistoryLimit)
		assert.Equal(t, last.Hash(), h[0].Hash)
	})

	t.Run("RetireBroken only writes over the configuration it read", func(t *testing.T) {
		e := newEnv()
		both := mcpTopics(map[string]string{appliedTopic: schemaV2, otherTopic: schemaV2})
		e.mustApply(t, both, false)
		e.mustApply(t, mcpTopics(map[string]string{appliedTopic: schemaV3, otherTopic: schemaV3}), true)
		a := e.applied(t)
		h2 := v2.TopicHash(appliedTopic)
		require.Equal(t, []string{h2}, a.BrokenHashes(appliedTopic))
		require.Equal(t, []string{h2}, a.BrokenHashes(otherTopic))

		ok, err := RetireBroken(ctx, rdb, e.dep, a, map[string][]string{appliedTopic: {h2}})
		require.NoError(t, err)
		assert.True(t, ok)
		got := e.applied(t)
		assert.Empty(t, got.BrokenHashes(appliedTopic))
		assert.Equal(t, []string{h2}, got.BrokenHashes(otherTopic))
		assert.Equal(t, a.Hash, got.Hash)
		assert.Len(t, e.history(t), 2, "retiring is not an apply")

		// a is stale now.
		ok, err = RetireBroken(ctx, rdb, e.dep, a, map[string][]string{otherTopic: {h2}})
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, []string{h2}, e.applied(t).BrokenHashes(otherTopic))

		ok, err = RetireBroken(ctx, rdb, e.dep, got, map[string][]string{otherTopic: {h2}})
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Nil(t, e.applied(t).Broken)

		_, err = RetireBroken(ctx, rdb, e.dep, &Applied{Hash: "x"}, nil)
		require.Error(t, err)
	})

	t.Run("heartbeats", func(t *testing.T) {
		e := newEnv()
		require.NoError(t, Heartbeat(ctx, rdb, e.dep, "h1", time.Second))
		live, err := IsLive(ctx, rdb, e.dep, "h1")
		require.NoError(t, err)
		assert.True(t, live)
		hashes, err := LiveHashes(ctx, rdb, e.dep, []string{"h0", "h1", "h2"})
		require.NoError(t, err)
		assert.Equal(t, []string{"h1"}, hashes)
		ttl, err := rdb.PTTL(ctx, LiveKey(e.dep, "h1")).Result()
		require.NoError(t, err)
		assert.Greater(t, ttl, time.Duration(0))
		assert.LessOrEqual(t, ttl, time.Second)

		fastForward(1100 * time.Millisecond)
		live, err = IsLive(ctx, rdb, e.dep, "h1")
		require.NoError(t, err)
		assert.False(t, live)
		hashes, err = LiveHashes(ctx, rdb, e.dep, []string{"h1"})
		require.NoError(t, err)
		assert.Empty(t, hashes)

		require.Error(t, Heartbeat(ctx, rdb, e.dep, "", time.Second))
		require.NoError(t, Heartbeat(ctx, rdb, e.dep, "h3", 0))
		ttl, err = rdb.PTTL(ctx, LiveKey(e.dep, "h3")).Result()
		require.NoError(t, err)
		assert.Greater(t, ttl, 50*time.Second, "default TTL")
	})

	t.Run("unreadable applied configuration", func(t *testing.T) {
		e := newEnv()
		require.NoError(t, rdb.Set(ctx, AppliedKey(e.dep), "not json", 0).Err())
		_, err := e.apply(t, v1, false)
		require.ErrorContains(t, err, "unreadable")
		require.NoError(t, rdb.Set(ctx, AppliedKey(e.dep), `{"snapshot":{}}`, 0).Err())
		_, err = ReadApplied(ctx, rdb, e.dep)
		require.ErrorContains(t, err, "no hash")
	})

	t.Run("deployments are isolated", func(t *testing.T) {
		a, b := newEnv(), newEnv()
		a.mustApply(t, v2, false)
		res := b.mustApply(t, v3, false)
		assert.True(t, res.Initial)
		assert.Equal(t, v2.Hash(), a.applied(t).Hash)
	})
}

func normalizeBroken(m map[string][]BrokenHash) map[string][]BrokenHash {
	for _, entries := range m {
		for i := range entries {
			entries[i].RecordedAt = entries[i].RecordedAt.UTC()
		}
	}
	return m
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := marshalNoEscape(v)
	require.NoError(t, err)
	return string(b)
}

func TestApply_Miniredis(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	runApplySuite(t, client, mr.FastForward)
}

// RunApplySuiteForTest runs the Apply suite against client. It is exported
// for the testcontainers variants in applied_integration_test.go, which live
// in the external test package to avoid an import cycle through testinfra.
func RunApplySuiteForTest(t *testing.T, client goredis.Cmdable) {
	runApplySuite(t, client, time.Sleep)
}

func TestAppliedKeys(t *testing.T) {
	assert.Equal(t, "{outpost:topic_schemas}:applied", AppliedKey(""))
	assert.Equal(t, "{outpost:topic_schemas}:history", HistoryKey(""))
	assert.Equal(t, "outpost:lock:topic_schemas", ApplyLockKey(""))
	assert.Equal(t, "outpost:topic_schemas:live:abc", LiveKey("", "abc"))
	assert.Equal(t, "dp_1:{outpost:topic_schemas}:applied", AppliedKey("dp_1"))
	assert.Equal(t, "dp_1:{outpost:topic_schemas}:history", HistoryKey("dp_1"))
	assert.Equal(t, "dp_1:outpost:lock:topic_schemas", ApplyLockKey("dp_1"))
	assert.Equal(t, "dp_1:outpost:topic_schemas:live:abc", LiveKey("dp_1", "abc"))
}

func TestSnapshotMCPServed(t *testing.T) {
	s := Snapshot{Topics: map[string]SnapshotTopic{
		"a": {MCPEnabled: true, PayloadSchema: json.RawMessage(schemaV1)},
		"b": {MCPEnabled: true},
		"c": {PayloadSchema: json.RawMessage(schemaV1)},
		"d": {MCPEnabled: true, PayloadSchema: json.RawMessage("null")},
	}}
	assert.True(t, s.MCPServed("a"))
	assert.False(t, s.MCPServed("b"))
	assert.False(t, s.MCPServed("c"))
	assert.False(t, s.MCPServed("d"))
	assert.False(t, s.MCPServed("missing"))
}

func testLogger(t *testing.T) *logging.Logger {
	return logging.NewTestLogger(zaptest.NewLogger(t))
}

func TestApplyLogs(t *testing.T) {
	// Logging must not panic on any outcome, and never runs for Plan.
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	opts := ApplyOptions{Logger: testLogger(t)}
	for _, step := range []struct {
		s     Snapshot
		force bool
	}{{oneTopic(schemaV2), false}, {oneTopic(schemaV2), false}, {oneTopic(schemaV1), true}, {Snapshot{}, true}} {
		opts.AllowBreaking = step.force
		_, err := Apply(ctx, client, "", step.s, opts)
		require.NoError(t, err)
	}
}
