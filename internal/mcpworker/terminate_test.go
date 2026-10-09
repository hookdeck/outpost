package mcpworker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const endedTopic = "order.legacy"

func TestEndedTopicSubscriptionsAreTerminated(t *testing.T) {
	h := newHarness(t)
	h.apply(t, h.snapshot, false)
	now := h.clock.Now()
	h.sub(t, "t1", "sub_keep", topicA, ptr(now.Add(time.Hour)))
	invalidAt := now.Add(time.Hour).UTC()
	old1 := h.sub(t, "t1", "sub_old1", endedTopic, ptr(now.Add(time.Hour)), func(d *models.Destination) {
		d.Credentials["previous_secret"] = testPrevSecr
		d.Credentials["previous_secret_invalid_at"] = invalidAt.Format(time.RFC3339)
	})
	old2 := h.sub(t, "t2", "sub_old2", endedTopic, nil)

	stats := h.pass(t)
	assert.Equal(t, PassStats{Ended: 2}, stats)
	assert.True(t, h.live(t, "t1", "sub_keep"))
	assert.False(t, h.live(t, "t1", "sub_old1"))
	assert.False(t, h.live(t, "t2", "sub_old2"))
	assert.Empty(t, h.indexed(t, endedTopic))
	assert.NotContains(t, h.indexed(t, ""), "sub_old1")

	terms := map[string]mcpevents.Termination{}
	for _, term := range h.notifier.list() {
		terms[term.SubscriptionID] = term
	}
	require.Len(t, terms, 2)
	got := terms["sub_old1"]
	assert.Equal(t, "t1", got.TenantID)
	assert.Equal(t, old1.Config["url"], got.URL)
	assert.True(t, got.CreatedAt.Equal(old1.CreatedAt))
	assert.Equal(t, []mcpevents.Secret{{Key: testKey}, {Key: testPrevKey, InvalidAt: &invalidAt}}, got.Secrets)
	assert.Equal(t, mcpevents.KindNotFound, got.Error.Kind)
	assert.Equal(t, map[string]any{"kind": "event"}, got.Error.Data)
	assert.Equal(t, old2.Config["url"], terms["sub_old2"].URL)
	assert.Equal(t, []mcpevents.Secret{{Key: testKey}}, terms["sub_old2"].Secrets)

	assert.Empty(t, h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired), "ending is not expiring")
	assert.Len(t, h.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated), 2)

	// Done once.
	assert.Equal(t, PassStats{}, h.pass(t))
	assert.Len(t, h.notifier.list(), 2)
}

func TestExpiredSubscriptionsOfEndedTopicsExpire(t *testing.T) {
	h := newHarness(t)
	h.apply(t, h.snapshot, false)
	now := h.clock.Now()
	h.sub(t, "t1", "sub_lapsed", endedTopic, ptr(now.Add(-10*time.Second)))
	h.sub(t, "t1", "sub_gone", endedTopic, ptr(now.Add(-5*time.Minute)))

	// sub_gone is past the grace period: swept as expired. sub_lapsed has
	// expired too, so no terminated envelope: it is swept once its grace
	// period is over.
	assert.Equal(t, PassStats{Expired: 1}, h.pass(t))
	assert.True(t, h.live(t, "t1", "sub_lapsed"))
	h.clock.Add(time.Minute)
	assert.Equal(t, PassStats{Expired: 1}, h.pass(t))
	assert.False(t, h.live(t, "t1", "sub_lapsed"))
	assert.Empty(t, h.notifier.list())
	assert.Len(t, h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired), 2)
}

func TestTerminationPausesOffTheAppliedConfiguration(t *testing.T) {
	t.Run("nothing applied", func(t *testing.T) {
		h := newHarness(t)
		h.sub(t, "t1", "sub_old", endedTopic, nil)
		assert.Equal(t, PassStats{}, h.pass(t))
		assert.True(t, h.live(t, "t1", "sub_old"))
	})

	t.Run("another configuration applied", func(t *testing.T) {
		h := newHarness(t)
		// The applied configuration still serves endedTopic: this instance
		// runs an older or newer one.
		h.apply(t, snapshotOf(map[string]string{topicA: schemaV1, topicB: schemaV1, endedTopic: schemaV1}), false)
		h.sub(t, "t1", "sub_old", endedTopic, nil)
		assert.Equal(t, PassStats{TerminationPaused: true}, h.pass(t))
		assert.True(t, h.live(t, "t1", "sub_old"))
		assert.Empty(t, h.notifier.list())
	})

	t.Run("the topic is still MCP-enabled here", func(t *testing.T) {
		h := newHarness(t)
		h.apply(t, h.snapshot, false)
		h.sub(t, "t1", "sub_a", topicA, nil)
		assert.Equal(t, PassStats{}, h.pass(t))
		assert.True(t, h.live(t, "t1", "sub_a"))
	})
}

func TestTerminationsAreSentOnce(t *testing.T) {
	h := newHarness(t)
	h.apply(t, h.snapshot, false)
	for i := range 40 {
		h.sub(t, fmt.Sprintf("t%d", i%3), fmt.Sprintf("sub_%02d", i), endedTopic, nil)
	}
	var wg sync.WaitGroup
	var stats [3]PassStats
	for i := range stats {
		wg.Go(func() { stats[i] = h.w.pass(context.Background()) })
	}
	wg.Wait()

	assert.EqualValues(t, 40, stats[0].Ended+stats[1].Ended+stats[2].Ended)
	seen := map[string]bool{}
	for _, term := range h.notifier.list() {
		assert.False(t, seen[term.SubscriptionID], "terminated twice: %s", term.SubscriptionID)
		seen[term.SubscriptionID] = true
	}
	assert.Len(t, seen, 40)
}

func TestTerminationEnvelopeFailureStillDeletes(t *testing.T) {
	h := newHarness(t)
	h.apply(t, h.snapshot, false)
	h.notifier.err = mcpevents.ErrQueueClosed
	h.sub(t, "t1", "sub_old", endedTopic, nil)
	stats := h.pass(t)
	assert.Equal(t, PassStats{Ended: 1, NotifyFailed: 1}, stats)
	assert.False(t, h.live(t, "t1", "sub_old"))
}

// forcedHarness applies v1 then forces v2 (topicA's total removed), the
// local configuration.
func forcedHarness(t *testing.T, opts ...func(*Config)) (*harness, topicschema.Snapshot, topicschema.Snapshot) {
	v1 := snapshotOf(map[string]string{topicA: schemaV1, topicB: schemaV1})
	v2 := snapshotOf(map[string]string{topicA: schemaV2, topicB: schemaV1})
	h := newHarness(t, append([]func(*Config){func(c *Config) { c.Snapshot = v2 }}, opts...)...)
	h.apply(t, v1, false)
	h.apply(t, v2, true)
	a, err := topicschema.ReadApplied(context.Background(), h.rdb, "")
	require.NoError(t, err)
	require.Equal(t, []string{v1.TopicHash(topicA)}, a.BrokenHashes(topicA))
	h.clock.Add(time.Second)
	return h, v1, v2
}

func withSchemaHash(hash string) func(*models.Destination) {
	return func(d *models.Destination) { d.Config["schema_hash"] = hash }
}

func TestBrokenSchemaSubscriptionsAreTerminated(t *testing.T) {
	h, v1, _ := forcedHarness(t)
	h1 := v1.TopicHash(topicA)
	h.sub(t, "t1", "sub_old", topicA, nil, withSchemaHash(h1))
	h.sub(t, "t1", "sub_new", topicA, nil) // the applied schema
	h.sub(t, "t1", "sub_other", topicB, nil, withSchemaHash(h1))
	h.sub(t, "t2", "sub_unknown", topicA, nil, withSchemaHash(""))

	stats := h.pass(t)
	assert.Equal(t, PassStats{SchemaChanged: 1}, stats)
	assert.False(t, h.live(t, "t1", "sub_old"))
	for _, id := range []string{"sub_new", "sub_other"} {
		assert.True(t, h.live(t, "t1", id), id)
	}
	assert.True(t, h.live(t, "t2", "sub_unknown"))

	terms := h.notifier.list()
	require.Len(t, terms, 1)
	assert.Equal(t, "sub_old", terms[0].SubscriptionID)
	assert.Equal(t, mcpevents.KindUnsupported, terms[0].Error.Kind)
	assert.Equal(t, map[string]any{"feature": "payloadSchema", "reason": "schema_changed"}, terms[0].Error.Data)
	assert.Len(t, h.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated), 1)

	// The scan removed something, so it runs once more, finds nothing and
	// is complete: later passes don't scan.
	h.clock.Add(time.Second)
	assert.Equal(t, PassStats{}, h.pass(t))
	scans := h.store.scans.Load()
	h.clock.Add(time.Second)
	assert.Equal(t, PassStats{}, h.pass(t))
	assert.Equal(t, scans, h.store.scans.Load())
	st := h.scanState(t, topicA)
	assert.Zero(t, st.StartedAt)
	assert.NotZero(t, st.CompletedAt)
}

func (h *harness) scanState(t *testing.T, topic string) scanState {
	t.Helper()
	raw, err := h.rdb.HGet(context.Background(), StateKey(h.cfg.DeploymentID), stateScanPrefix+topic).Result()
	require.NoError(t, err)
	var st scanState
	require.NoError(t, json.Unmarshal([]byte(raw), &st))
	return st
}

func TestBrokenSchemaScanResumesAcrossPasses(t *testing.T) {
	h, v1, _ := forcedHarness(t, func(c *Config) {
		c.BatchSize = 2
		c.PassBudget = 2
	})
	h1 := v1.TopicHash(topicA)
	for i := range 7 {
		hash := ""
		if i%2 == 0 {
			hash = h1
		}
		h.sub(t, "t1", fmt.Sprintf("sub_%d", i), topicA, nil, withSchemaHash(hash))
	}

	cursors := []uint64{}
	for range 20 {
		h.pass(t)
		h.clock.Add(time.Second)
		cursors = append(cursors, h.scanState(t, topicA).Cursor)
	}
	assert.Contains(t, cursors, uint64(2), "the cursor is kept between passes")
	for i := range 7 {
		assert.Equal(t, i%2 != 0, h.live(t, "t1", fmt.Sprintf("sub_%d", i)), "sub_%d", i)
	}
	assert.Len(t, h.notifier.list(), 4)
	st := h.scanState(t, topicA)
	assert.Zero(t, st.StartedAt)
	assert.NotZero(t, st.CompletedAt)
}

func TestBrokenHashesRetire(t *testing.T) {
	ctx := context.Background()
	h, v1, v2 := forcedHarness(t, func(c *Config) { c.TTLMax = time.Hour })
	h.sub(t, "t1", "sub_old", topicA, nil, withSchemaHash(v1.TopicHash(topicA)))
	broken := func() []string {
		a, err := topicschema.ReadApplied(ctx, h.rdb, "")
		require.NoError(t, err)
		return a.BrokenHashes(topicA)
	}

	// An instance still runs v1: subscriptions it accepts keep coming, so
	// the scan keeps running and nothing retires.
	require.NoError(t, topicschema.Heartbeat(ctx, h.rdb, "", v1.Hash(), 10*time.Hour))
	assert.EqualValues(t, 1, h.pass(t).SchemaChanged)
	h.clock.Add(2 * time.Hour)
	h.sub(t, "t1", "sub_late", topicA, nil, withSchemaHash(v1.TopicHash(topicA)))
	assert.EqualValues(t, 1, h.pass(t).SchemaChanged, "accepted by the v1 instance during the rollout")
	assert.NotEmpty(t, broken())

	// It stops: MCP_TTL_MAX after it was last seen, and after a complete
	// scan, the hashes retire.
	h.mr.Del(topicschema.LiveKey("", v1.Hash()))
	h.clock.Add(time.Minute)
	h.pass(t)
	h.clock.Add(time.Minute)
	h.pass(t)
	assert.NotEmpty(t, broken(), "not before MCP_TTL_MAX")
	h.clock.Add(time.Hour)
	h.pass(t)
	assert.Empty(t, broken())
	exists, err := h.rdb.HExists(ctx, StateKey(""), stateScanPrefix+topicA).Result()
	require.NoError(t, err)
	assert.False(t, exists, "the scan state is dropped")

	// The applied configuration is otherwise unchanged.
	a, err := topicschema.ReadApplied(ctx, h.rdb, "")
	require.NoError(t, err)
	assert.Equal(t, v2.Hash(), a.Hash)
}

func TestDestinationSecrets(t *testing.T) {
	invalidAt := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		creds models.Credentials
		want  []mcpevents.Secret
	}{
		"current only": {models.Credentials{"secret": testSecret}, []mcpevents.Secret{{Key: testKey}}},
		"rotation": {
			models.Credentials{"secret": testSecret, "previous_secret": testPrevSecr, "previous_secret_invalid_at": invalidAt.Format(time.RFC3339)},
			[]mcpevents.Secret{{Key: testKey}, {Key: testPrevKey, InvalidAt: &invalidAt}},
		},
		"previous without invalid_at is left out": {
			models.Credentials{"secret": testSecret, "previous_secret": testPrevSecr},
			[]mcpevents.Secret{{Key: testKey}},
		},
		"invalid secrets are left out": {
			models.Credentials{"secret": "whsec_short", "previous_secret": "nope", "previous_secret_invalid_at": invalidAt.Format(time.RFC3339)},
			nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, DestinationSecrets(&models.Destination{Credentials: tc.creds}))
		})
	}
}
