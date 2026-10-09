package mcpworker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSweepDeletesExpiredSubscriptions(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	now := h.clock.Now()
	a := h.sub(t, "t1", "sub_a", topicA, ptr(now.Add(-2*time.Minute)))
	h.sub(t, "t1", "sub_grace", topicA, ptr(now.Add(-30*time.Second)))
	h.sub(t, "t1", "sub_live", topicA, ptr(now.Add(time.Hour)))
	h.sub(t, "t1", "sub_forever", topicA, nil)
	h.webhook(t, "t1", "wh_1", topicB)
	d := h.sub(t, "t2", "sub_d", topicB, ptr(now.Add(-5*time.Minute)))

	stats := h.pass(t)
	assert.Equal(t, PassStats{Expired: 2}, stats)
	assert.False(t, h.live(t, "t1", "sub_a"))
	assert.False(t, h.live(t, "t2", "sub_d"))
	for _, id := range []string{"sub_grace", "sub_live", "sub_forever"} {
		assert.True(t, h.live(t, "t1", id), id)
	}
	assert.Equal(t, map[string]int64{
		"sub_grace":   now.Add(-30 * time.Second).UnixMilli(),
		"sub_live":    now.Add(time.Hour).UnixMilli(),
		"sub_forever": tenantstore.NoExpiryScore,
	}, h.indexed(t, ""))
	assert.NotContains(t, h.indexed(t, topicB), "sub_d")

	// One mcp.subscription.expired per subscription, with the documented
	// payload.
	var expired []opevents.MCPSubscriptionExpiredData
	for _, ev := range h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired) {
		expired = append(expired, ev.Data.(opevents.MCPSubscriptionExpiredData))
	}
	assert.ElementsMatch(t, []opevents.MCPSubscriptionExpiredData{
		{TenantID: "t1", SubscriptionID: "sub_a", Principal: "user_sub_a", Topic: topicA, URL: "https://receiver.example.com/sub_a", ExpiresAt: a.ExpiresAt.UTC()},
		{TenantID: "t2", SubscriptionID: "sub_d", Principal: "user_sub_d", Topic: topicB, URL: "https://receiver.example.com/sub_d", ExpiresAt: d.ExpiresAt.UTC()},
	}, expired)

	// One tenant.subscription.updated per tenant.
	updates := map[string]opevents.TenantSubscriptionUpdatedData{}
	for _, ev := range h.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated) {
		data := ev.Data.(opevents.TenantSubscriptionUpdatedData)
		require.NotContains(t, updates, data.TenantID, "one event per tenant")
		updates[data.TenantID] = data
	}
	require.Len(t, updates, 2)
	assert.Equal(t, 5, updates["t1"].PreviousDestinationsCount)
	assert.Equal(t, 4, updates["t1"].DestinationsCount)
	assert.Equal(t, 1, updates["t2"].PreviousDestinationsCount)
	assert.Equal(t, 0, updates["t2"].DestinationsCount)
	assert.Equal(t, []string{topicB}, updates["t2"].PreviousTopics)
	assert.Empty(t, updates["t2"].Topics)

	// Nothing more to do.
	assert.Equal(t, PassStats{}, h.pass(t))
	assert.Len(t, h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired), 2)

	// Past its grace period, the next one goes.
	h.clock.Add(time.Minute)
	assert.Equal(t, PassStats{Expired: 1}, h.pass(t))
	assert.False(t, h.live(t, "t1", "sub_grace"))
	_ = ctx
}

func TestSweepRepairsStaleIndexEntries(t *testing.T) {
	for _, withEmitter := range []bool{true, false} {
		t.Run(fmt.Sprintf("emitter=%v", withEmitter), func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t, func(c *Config) {
				if !withEmitter {
					c.Emitter = nil
				}
			})
			now := h.clock.Now()
			old := now.Add(-10 * time.Minute).UnixMilli()
			later := now.Add(time.Hour)
			stale := func(d models.Destination) {
				ref := tenantstore.IndexedDestination{TenantID: d.TenantID, DestinationID: d.ID, Score: tenantstore.IndexScore(d.ExpiresAt)}
				require.NoError(t, h.store.RescoreIndexedDestination(ctx, models.DestinationTypeMCP, d.Topics, ref, old))
			}

			// Refreshed, but its entry kept the old expiry.
			refreshed := h.sub(t, "t1", "sub_refreshed", topicA, &later)
			stale(refreshed)
			// Deleted, and its entry was left behind.
			orphan := h.sub(t, "t1", "sub_orphan", topicA, &later)
			stale(orphan)
			require.NoError(t, h.store.DeleteDestination(ctx, "t1", "sub_orphan"))
			// Its ID now names a webhook destination.
			reused := h.sub(t, "t1", "sub_reused", topicA, &later)
			stale(reused)
			require.NoError(t, h.store.DeleteDestination(ctx, "t1", "sub_reused"))
			h.webhook(t, "t1", "sub_reused", topicA)
			require.Len(t, h.indexed(t, ""), 3)

			stats := h.pass(t)
			assert.Equal(t, PassStats{Rescored: 1, Removed: 2}, stats)
			assert.Equal(t, map[string]int64{"sub_refreshed": later.UnixMilli()}, h.indexed(t, ""))
			assert.Equal(t, map[string]int64{"sub_refreshed": later.UnixMilli()}, h.indexed(t, topicA))
			assert.True(t, h.live(t, "t1", "sub_refreshed"))
			assert.Empty(t, h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired))
		})
	}
}

func TestSweepPagesAndBudget(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.PassBudget = 120 })
	now := h.clock.Now()
	for i := range 250 {
		h.sub(t, fmt.Sprintf("t%d", i%3), fmt.Sprintf("sub_%03d", i), topicA, ptr(now.Add(-time.Duration(i+2)*time.Minute)))
	}

	assert.EqualValues(t, 120, h.pass(t).Expired, "a pass stops at its budget")
	assert.EqualValues(t, 120, h.pass(t).Expired)
	assert.EqualValues(t, 10, h.pass(t).Expired)
	assert.Empty(t, h.indexed(t, ""))
	assert.Len(t, h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired), 250)
	assert.Len(t, h.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated), 9, "one per tenant per pass")
}

func TestSweepListsPastFailingEntries(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	failing := map[string]bool{}
	for i := range 250 {
		id := fmt.Sprintf("sub_%03d", i)
		// The oldest 150 fail to delete: the sweep must not stall on them.
		h.sub(t, "t1", id, topicA, ptr(now.Add(-time.Duration(1000-i)*time.Minute)))
		if i < 150 {
			failing[id] = true
		}
	}
	h.store.deleteErr = func(id string) error {
		if failing[id] {
			return errors.New("boom")
		}
		return nil
	}

	stats := h.pass(t)
	assert.EqualValues(t, 100, stats.Expired)
	assert.EqualValues(t, 150, stats.Errors)
	assert.Len(t, h.indexed(t, ""), 150)

	h.store.deleteErr = nil
	assert.EqualValues(t, 150, h.pass(t).Expired, "retried on the next pass")
}

func TestSweepWithoutEmitterDoesNotReadSubscriptions(t *testing.T) {
	for name, opt := range map[string]func(*Config){
		"no emitter": func(c *Config) { c.Emitter = nil },
		"topics filtered out": func(c *Config) {
			c.Emitter = &fakeEmitter{topics: map[string]bool{opevents.TopicTenantSubscriptionUpdated: true}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, opt)
			now := h.clock.Now()
			h.sub(t, "t1", "sub_a", topicA, ptr(now.Add(-2*time.Minute)))
			h.sub(t, "t1", "sub_b", topicA, ptr(now.Add(-3*time.Minute)))

			assert.EqualValues(t, 2, h.pass(t).Expired)
			assert.Zero(t, h.store.retrieves.Load())
			assert.False(t, h.live(t, "t1", "sub_a"))
		})
	}
}

func TestSweepConcurrentPassesReportOnce(t *testing.T) {
	h := newHarness(t)
	now := h.clock.Now()
	for i := range 60 {
		h.sub(t, fmt.Sprintf("t%d", i%4), fmt.Sprintf("sub_%02d", i), topicA, ptr(now.Add(-time.Duration(i+2)*time.Minute)))
	}

	// Two passes at once, as when a lock expires mid-pass.
	var wg sync.WaitGroup
	var total [2]PassStats
	for i := range total {
		wg.Go(func() { total[i] = h.w.pass(context.Background()) })
	}
	wg.Wait()

	assert.EqualValues(t, 60, total[0].Expired+total[1].Expired)
	seen := map[string]bool{}
	for _, ev := range h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired) {
		id := ev.Data.(opevents.MCPSubscriptionExpiredData).SubscriptionID
		assert.False(t, seen[id], "reported twice: %s", id)
		seen[id] = true
	}
	assert.Len(t, seen, 60)
}

func TestSweepRefreshDuringPassKeepsSubscription(t *testing.T) {
	for _, when := range []string{"before the read", "between the read and the delete"} {
		t.Run(when, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			now := h.clock.Now()
			d := h.sub(t, "t1", "sub_a", topicA, ptr(now.Add(-2*time.Minute)))

			later := now.Add(time.Hour)
			var once sync.Once
			refresh := func() {
				once.Do(func() {
					refreshed := d
					refreshed.ExpiresAt = &later
					_, err := h.store.UpdateDestinationIfLive(ctx, refreshed, d.CreatedAt)
					require.NoError(t, err)
				})
			}
			if when == "before the read" {
				h.store.retrieveHook = func(string) { refresh() }
			} else {
				h.store.deleteErr = func(string) error { refresh(); return nil }
			}

			stats := h.pass(t)
			assert.Zero(t, stats.Expired)
			assert.True(t, h.live(t, "t1", "sub_a"))
			assert.Equal(t, map[string]int64{"sub_a": later.UnixMilli()}, h.indexed(t, ""))
			assert.Empty(t, h.emitter.byTopic(opevents.TopicMCPSubscriptionExpired))
			assert.Empty(t, h.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated))
		})
	}
}

func TestSweepResubscribeDuringPassIsNotDeleted(t *testing.T) {
	// The subscription expired, was deleted at subscribe time and created
	// again (a new generation) between the sweep's read and its delete.
	ctx := context.Background()
	h := newHarness(t)
	now := h.clock.Now()
	d := h.sub(t, "t1", "sub_a", topicA, ptr(now.Add(-2*time.Minute)))
	later := now.Add(time.Hour)
	var once sync.Once
	h.store.deleteErr = func(string) error {
		once.Do(func() {
			_, err := h.store.TenantStore.DeleteDestinationIf(ctx, "t1", "sub_a", tenantstore.DeleteCondition{Reason: tenantstore.DeleteReasonExpired})
			require.NoError(t, err)
			again := d
			again.CreatedAt = now.Truncate(time.Millisecond)
			again.ExpiresAt = &later
			require.NoError(t, h.store.CreateDestination(ctx, again))
		})
		return nil
	}

	stats := h.pass(t)
	assert.Zero(t, stats.Expired)
	assert.True(t, h.live(t, "t1", "sub_a"))
	assert.Equal(t, map[string]int64{"sub_a": later.UnixMilli()}, h.indexed(t, ""))
}
