package drivertest

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testIndex(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	const topic = "order.created"
	newStore := func(t *testing.T) (context.Context, driver.TenantStore) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)
		store, err := h.MakeDriverWithOptions(ctx, DriverOptions{
			TypeLimits:   map[string]int{"mcp": 1000},
			IndexedTypes: []string{"mcp"},
		})
		require.NoError(t, err)
		return ctx, store
	}
	ref := func(d models.Destination) driver.IndexedDestination {
		return driver.IndexedDestination{TenantID: d.TenantID, DestinationID: d.ID, Score: driver.IndexScore(d.ExpiresAt)}
	}
	// list returns the whole global index (or a topic index).
	list := func(t *testing.T, ctx context.Context, store driver.TenantStore, topic string) []driver.IndexedDestination {
		t.Helper()
		entries, err := store.ListIndexedDestinations(ctx, "mcp", topic, driver.NoExpiryScore, 1000)
		require.NoError(t, err)
		return entries
	}
	expiring := func(tenantID string, in time.Duration) models.Destination {
		return newMCPDestination(tenantID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(in).Truncate(time.Millisecond)))
	}

	t.Run("WritesIndex", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		d := expiring(tenantID, time.Hour)
		forever := newMCPDestination(tenantID)
		require.NoError(t, store.CreateDestination(ctx, d))
		require.NoError(t, store.CreateDestination(ctx, forever))
		require.NoError(t, store.CreateDestination(ctx, testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenantID))))

		want := []driver.IndexedDestination{ref(d), {TenantID: tenantID, DestinationID: forever.ID, Score: driver.NoExpiryScore}}
		assert.Equal(t, want, list(t, ctx, store, ""), "only indexed types, in score order")
		assert.Equal(t, want, list(t, ctx, store, topic))
		empty, err := store.ListIndexedDestinations(ctx, "webhook", "", driver.NoExpiryScore, 10)
		require.NoError(t, err)
		assert.Empty(t, empty)

		topics, err := store.ListIndexedTopics(ctx, "mcp")
		require.NoError(t, err)
		assert.Equal(t, []string{topic}, topics)

		t.Run("updates rescore", func(t *testing.T) {
			later := time.Now().Add(2 * time.Hour).Truncate(time.Millisecond)
			d.ExpiresAt = &later
			_, err := store.UpdateDestinationIfLive(ctx, d, d.CreatedAt)
			require.NoError(t, err)
			latest := later.Add(time.Hour)
			forever.ExpiresAt = &latest
			require.NoError(t, store.UpsertDestination(ctx, forever))

			want := []driver.IndexedDestination{ref(d), ref(forever)}
			assert.Equal(t, want, list(t, ctx, store, ""))
			assert.Equal(t, want, list(t, ctx, store, topic))
		})

		t.Run("deletes remove", func(t *testing.T) {
			res, err := store.DeleteDestinationIf(ctx, tenantID, d.ID, driver.DeleteCondition{})
			require.NoError(t, err)
			require.True(t, res.Deleted)
			require.NoError(t, store.DeleteDestination(ctx, tenantID, forever.ID))
			assert.Empty(t, list(t, ctx, store, ""))
			assert.Empty(t, list(t, ctx, store, topic))
		})
	})

	t.Run("RefusedWritesLeaveTheIndexRight", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		d := expiring(tenantID, time.Hour)
		require.NoError(t, store.CreateDestination(ctx, d))

		dup := d
		other := time.Now().Add(5 * time.Hour)
		dup.ExpiresAt = &other
		require.ErrorIs(t, store.CreateDestination(ctx, dup), driver.ErrDuplicateDestination)
		assert.Equal(t, []driver.IndexedDestination{ref(d)}, list(t, ctx, store, ""), "a duplicate keeps the live score")

		_, err := store.UpdateDestinationIfLive(ctx, dup, d.CreatedAt.Add(-time.Second))
		require.ErrorIs(t, err, driver.ErrDestinationConflict)
		assert.Equal(t, []driver.IndexedDestination{ref(d)}, list(t, ctx, store, topic), "a conflict keeps the live score")

		refused := expiring(tenantID, 3*time.Hour)
		require.NoError(t, store.CreateDestination(ctx, refused, driver.WithBuckets(driver.Bucket{Name: "full", Max: 1})))
		refused2 := expiring(tenantID, 4*time.Hour)
		require.Error(t, store.CreateDestination(ctx, refused2, driver.WithBuckets(driver.Bucket{Name: "full", Max: 1})))
		for _, e := range list(t, ctx, store, "") {
			assert.NotEqual(t, refused2.ID, e.DestinationID, "a refused create is not indexed")
		}

		gone := expiring(tenantID, 6*time.Hour)
		_, err = store.UpdateDestinationIfLive(ctx, gone, gone.CreatedAt)
		require.ErrorIs(t, err, driver.ErrDestinationNotFound)
		for _, e := range list(t, ctx, store, topic) {
			assert.NotEqual(t, gone.ID, e.DestinationID, "a refused update is not indexed")
		}

		deleted := testutil.TenantFactory.Any()
		require.NoError(t, store.UpsertTenant(ctx, deleted))
		require.NoError(t, store.DeleteTenant(ctx, deleted.ID))
		orphan := expiring(deleted.ID, 7*time.Hour)
		require.ErrorIs(t, store.CreateDestination(ctx, orphan), driver.ErrTenantDeleted)
		for _, e := range list(t, ctx, store, "") {
			assert.NotEqual(t, orphan.ID, e.DestinationID, "a create into a deleted tenant is not indexed")
		}
	})

	t.Run("ListOrderAndLimit", func(t *testing.T) {
		ctx, store := newStore(t)
		var all []driver.IndexedDestination
		for _, in := range []time.Duration{3 * time.Hour, -time.Hour, time.Hour, -2 * time.Hour} {
			d := expiring(idgen.String(), in)
			require.NoError(t, store.CreateDestination(ctx, d))
			all = append(all, ref(d))
		}
		ordered := []driver.IndexedDestination{all[3], all[1], all[2], all[0]}

		entries, err := store.ListIndexedDestinations(ctx, "mcp", "", time.Now().UnixMilli(), 10)
		require.NoError(t, err)
		assert.Equal(t, ordered[:2], entries, "expired only")

		entries, err = store.ListIndexedDestinations(ctx, "mcp", topic, driver.NoExpiryScore, 3)
		require.NoError(t, err)
		assert.Equal(t, ordered[:3], entries)

		_, err = store.ListIndexedDestinations(ctx, "mcp", "", driver.NoExpiryScore, 0)
		assert.Error(t, err)

		n, err := store.CountIndexed(ctx, "mcp", "", time.Now().UnixMilli())
		require.NoError(t, err)
		assert.EqualValues(t, 2, n)
		n, err = store.CountIndexed(ctx, "mcp", topic, ordered[2].Score)
		require.NoError(t, err)
		assert.EqualValues(t, 2, n, "minScore is inclusive")
		n, err = store.CountIndexed(ctx, "mcp", "other.topic", 0)
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("SweepPaging", func(t *testing.T) {
		ctx, store := newStore(t)
		var expired, live []models.Destination
		for i := range 7 {
			d := expiring(idgen.String(), -time.Duration(i+1)*time.Minute)
			require.NoError(t, store.CreateDestination(ctx, d))
			expired = append(expired, d)
		}
		for range 3 {
			d := expiring(idgen.String(), time.Hour)
			require.NoError(t, store.CreateDestination(ctx, d))
			live = append(live, d)
		}

		// A sweeper reads from offset 0 every time, as each processed entry
		// leaves the range.
		now := time.Now()
		deleted := 0
		for pass := 0; pass < 10; pass++ {
			entries, err := store.ListIndexedDestinations(ctx, "mcp", "", now.UnixMilli(), 2)
			require.NoError(t, err)
			if len(entries) == 0 {
				break
			}
			for _, e := range entries {
				res, err := store.DeleteDestinationIf(ctx, e.TenantID, e.DestinationID, driver.DeleteCondition{
					ExpiredBefore: &now, Reason: driver.DeleteReasonExpired,
				})
				require.NoError(t, err)
				require.True(t, res.Deleted)
				deleted++
			}
		}
		assert.Equal(t, len(expired), deleted)
		for _, d := range expired {
			assertNotLive(t, ctx, store, d)
		}
		remaining := list(t, ctx, store, "")
		require.Len(t, remaining, len(live))
	})

	t.Run("CompareScoreRemove", func(t *testing.T) {
		ctx, store := newStore(t)
		d := expiring(idgen.String(), time.Hour)
		forever := newMCPDestination(idgen.String())
		require.NoError(t, store.CreateDestination(ctx, d))
		require.NoError(t, store.CreateDestination(ctx, forever))
		topics := []string{topic}
		// Their entries outlive them as "mcp" destinations: a live one keeps
		// its entries (see RemoveKeepsLiveDestinations).
		for _, retyped := range []models.Destination{d, forever} {
			retyped.Type = "webhook"
			require.NoError(t, store.UpsertDestination(ctx, retyped))
		}

		stale := ref(d)
		stale.Score--
		require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", topics, stale))
		require.Len(t, list(t, ctx, store, ""), 2, "another score is left alone")

		require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", nil, ref(d)))
		assert.Equal(t, []driver.IndexedDestination{ref(forever)}, list(t, ctx, store, ""))
		assert.Len(t, list(t, ctx, store, topic), 2, "topic indexes are only touched when named")
		require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", topics, ref(d)))
		assert.Equal(t, []driver.IndexedDestination{ref(forever)}, list(t, ctx, store, topic))

		require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", topics, driver.IndexedDestination{
			TenantID: forever.TenantID, DestinationID: forever.ID, Score: driver.NoExpiryScore - 1,
		}))
		require.Len(t, list(t, ctx, store, ""), 1)
		require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", topics, ref(forever)))
		assert.Empty(t, list(t, ctx, store, ""))
		assert.Empty(t, list(t, ctx, store, topic))

		require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", topics, ref(forever)), "removing a missing entry is a no-op")
	})

	// Every generation of a destination without expiry has the same member
	// and score, so a removal for one generation can't tell it from the
	// next: the entries of a live destination stay.
	t.Run("RemoveKeepsLiveDestinations", func(t *testing.T) {
		ctx, store := newStore(t)
		forever := newMCPDestination(idgen.String())
		require.NoError(t, store.CreateDestination(ctx, forever))
		res, err := store.DeleteDestinationIf(ctx, forever.TenantID, forever.ID, driver.DeleteCondition{Reason: driver.DeleteReasonUnsubscribed})
		require.NoError(t, err)
		require.True(t, res.Deleted)

		// Subscribed again before the first generation's removal ran.
		require.NoError(t, store.CreateDestination(ctx, forever))
		require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", forever.Topics, ref(forever)))
		assert.Equal(t, []driver.IndexedDestination{ref(forever)}, list(t, ctx, store, ""))
		assert.Equal(t, []driver.IndexedDestination{ref(forever)}, list(t, ctx, store, topic))

		t.Run("with its own score, in every index", func(t *testing.T) {
			d := expiring(idgen.String(), time.Hour)
			require.NoError(t, store.CreateDestination(ctx, d))
			require.NoError(t, store.RemoveIndexedDestination(ctx, "mcp", nil, ref(d)))
			assert.Contains(t, list(t, ctx, store, ""), ref(d))
			assert.Contains(t, list(t, ctx, store, topic), ref(d))
		})

		t.Run("not once deleted", func(t *testing.T) {
			res, err := store.DeleteDestinationIf(ctx, forever.TenantID, forever.ID, driver.DeleteCondition{})
			require.NoError(t, err)
			require.True(t, res.Deleted)
			for _, topic := range []string{"", topic} {
				for _, e := range list(t, ctx, store, topic) {
					assert.NotEqual(t, forever.ID, e.DestinationID)
				}
			}
		})
	})

	t.Run("CompareScoreRescore", func(t *testing.T) {
		ctx, store := newStore(t)
		d := expiring(idgen.String(), -time.Hour)
		require.NoError(t, store.CreateDestination(ctx, d))
		topics := []string{topic}
		real := time.Now().Add(time.Hour).UnixMilli()

		stale := ref(d)
		stale.Score++
		require.NoError(t, store.RescoreIndexedDestination(ctx, "mcp", topics, stale, real))
		assert.Equal(t, ref(d).Score, list(t, ctx, store, "")[0].Score, "another score is left alone")

		require.NoError(t, store.RescoreIndexedDestination(ctx, "mcp", topics, ref(d), real))
		assert.Equal(t, real, list(t, ctx, store, "")[0].Score)
		assert.Equal(t, real, list(t, ctx, store, topic)[0].Score)

		moved := ref(d)
		moved.Score = real
		require.NoError(t, store.RescoreIndexedDestination(ctx, "mcp", topics, moved, driver.NoExpiryScore))
		assert.Equal(t, driver.NoExpiryScore, list(t, ctx, store, "")[0].Score)
		assert.Equal(t, driver.NoExpiryScore, list(t, ctx, store, topic)[0].Score)

		moved.Score = driver.NoExpiryScore
		require.NoError(t, store.RescoreIndexedDestination(ctx, "mcp", topics, moved, real))
		assert.Equal(t, real, list(t, ctx, store, topic)[0].Score)

		missing := driver.IndexedDestination{TenantID: "t", DestinationID: "d", Score: real}
		require.NoError(t, store.RescoreIndexedDestination(ctx, "mcp", topics, missing, real+1))
		assert.Len(t, list(t, ctx, store, ""), 1, "rescoring never adds")
	})

	t.Run("TenantDeleteCleansIndexes", func(t *testing.T) {
		ctx, store := newStore(t)
		tenant := testutil.TenantFactory.Any()
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		require.NoError(t, store.CreateDestination(ctx, expiring(tenant.ID, time.Hour)))
		require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID)))
		other := expiring(idgen.String(), time.Hour)
		require.NoError(t, store.CreateDestination(ctx, other))

		require.NoError(t, store.DeleteTenant(ctx, tenant.ID))
		assert.Equal(t, []driver.IndexedDestination{ref(other)}, list(t, ctx, store, ""))
		assert.Equal(t, []driver.IndexedDestination{ref(other)}, list(t, ctx, store, topic))
	})

	t.Run("Scan", func(t *testing.T) {
		ctx, store := newStore(t)
		want := map[string]driver.IndexedDestination{}
		for i := range 9 {
			d := expiring(idgen.String(), time.Duration(i+1)*time.Minute)
			require.NoError(t, store.CreateDestination(ctx, d))
			want[d.ID] = ref(d)
		}
		for _, topic := range []string{"", topic} {
			got := map[string]driver.IndexedDestination{}
			var cursor uint64
			for i := 0; ; i++ {
				require.Less(t, i, 100, "scan must terminate")
				entries, next, err := store.ScanIndexedDestinations(ctx, "mcp", topic, cursor, 2)
				require.NoError(t, err)
				for _, e := range entries {
					got[e.DestinationID] = e
				}
				if next == 0 {
					break
				}
				cursor = next
			}
			assert.Equal(t, want, got)
		}
		_, _, err := store.ScanIndexedDestinations(ctx, "mcp", "", 0, 0)
		assert.Error(t, err)
	})
}
