package drivertest

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMCPDestination builds an "mcp" destination of tenantID on topic
// order.created.
func newMCPDestination(tenantID string, opts ...func(*models.Destination)) models.Destination {
	return testutil.DestinationFactory.Any(append([]func(*models.Destination){
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithType("mcp"),
		testutil.DestinationFactory.WithTopics([]string{"order.created"}),
	}, opts...)...)
}

// assertNotLive checks that a destination is deleted everywhere: retrieve,
// list and match.
func assertNotLive(t *testing.T, ctx context.Context, store driver.TenantStore, d models.Destination) {
	t.Helper()
	_, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
	assert.ErrorIs(t, err, driver.ErrDestinationDeleted)
	list, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: d.TenantID, IDs: []string{d.ID}})
	require.NoError(t, err)
	assert.Empty(t, list)
	all, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: d.TenantID})
	require.NoError(t, err)
	for _, l := range all {
		assert.NotEqual(t, d.ID, l.ID)
	}
	matched, err := matchedIDs(store.MatchEvent(ctx, testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(d.TenantID),
		testutil.EventFactory.WithTopic(d.Topics[0]),
	), true))
	require.NoError(t, err)
	assert.NotContains(t, matched, d.ID)
}

func testConditional(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	newStore := func(t *testing.T) (context.Context, driver.TenantStore) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)
		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		return ctx, store
	}

	t.Run("CreateOverTombstone", func(t *testing.T) {
		for _, tc := range []struct {
			reason  string
			revoked bool
		}{
			{driver.DeleteReasonRevoked, true},
			{driver.DeleteReasonUnsubscribed, true},
			{driver.DeleteReasonTerminated, true},
			{"", true},
			{driver.DeleteReasonExpired, false},
		} {
			t.Run("reason "+tc.reason, func(t *testing.T) {
				ctx, store := newStore(t)
				d := newMCPDestination(idgen.String())
				require.NoError(t, store.CreateDestination(ctx, d))

				startedAt := time.Now().Add(-time.Second)
				res, err := store.DeleteDestinationIf(ctx, d.TenantID, d.ID, driver.DeleteCondition{Reason: tc.reason})
				require.NoError(t, err)
				require.True(t, res.Deleted)

				err = store.CreateDestination(ctx, d, driver.WithNotDeletedSince(startedAt))
				if tc.revoked {
					require.ErrorIs(t, err, driver.ErrDestinationRevoked)
					assertNotLive(t, ctx, store, d)
				} else {
					require.NoError(t, err)
				}
			})
		}

		t.Run("deleted before the write started", func(t *testing.T) {
			ctx, store := newStore(t)
			d := newMCPDestination(idgen.String())
			require.NoError(t, store.CreateDestination(ctx, d))
			_, err := store.DeleteDestinationIf(ctx, d.TenantID, d.ID, driver.DeleteCondition{Reason: driver.DeleteReasonRevoked})
			require.NoError(t, err)

			startedAt := time.Now().Add(time.Second)
			require.NoError(t, store.CreateDestination(ctx, d, driver.WithNotDeletedSince(startedAt)))
		})

		t.Run("tenant deleted", func(t *testing.T) {
			ctx, store := newStore(t)
			tenant := testutil.TenantFactory.Any()
			require.NoError(t, store.UpsertTenant(ctx, tenant))
			d := newMCPDestination(tenant.ID)
			require.NoError(t, store.CreateDestination(ctx, d))
			startedAt := time.Now().Add(-time.Second)
			require.NoError(t, store.DeleteTenant(ctx, tenant.ID))

			err := store.CreateDestination(ctx, d, driver.WithNotDeletedSince(startedAt))
			require.ErrorIs(t, err, driver.ErrDestinationRevoked)
		})

		t.Run("without guard replaces the tombstone", func(t *testing.T) {
			ctx, store := newStore(t)
			d := newMCPDestination(idgen.String())
			require.NoError(t, store.CreateDestination(ctx, d))
			_, err := store.DeleteDestinationIf(ctx, d.TenantID, d.ID, driver.DeleteCondition{Reason: driver.DeleteReasonRevoked})
			require.NoError(t, err)
			require.NoError(t, store.CreateDestination(ctx, d))
		})
	})

	// A create that started before its tenant was deleted (a subscribe
	// verifying its callback) must not bring a destination into it.
	t.Run("CreateIntoDeletedTenant", func(t *testing.T) {
		ctx, store := newStore(t)
		tenant := testutil.TenantFactory.Any()
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		startedAt := time.Now().Add(-time.Second)
		require.NoError(t, store.DeleteTenant(ctx, tenant.ID))

		for name, opts := range map[string][]driver.WriteOption{
			"unguarded": nil,
			"guarded":   {driver.WithNotDeletedSince(startedAt)},
			"bucketed":  {driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 5})},
		} {
			t.Run(name, func(t *testing.T) {
				for _, d := range []models.Destination{
					newMCPDestination(tenant.ID),
					testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID)),
				} {
					require.ErrorIs(t, store.CreateDestination(ctx, d, opts...), driver.ErrTenantDeleted)
					got, err := store.RetrieveDestination(ctx, tenant.ID, d.ID)
					require.NoError(t, err)
					assert.Nil(t, got, "nothing written")
				}
				list, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID})
				require.NoError(t, err)
				assert.Empty(t, list)
				matched, err := store.MatchEvent(ctx, testutil.EventFactory.Any(
					testutil.EventFactory.WithTenantID(tenant.ID),
					testutil.EventFactory.WithTopic("order.created"),
				), true)
				require.NoError(t, err)
				assert.Empty(t, matched)
			})
		}

		t.Run("tenant never created", func(t *testing.T) {
			require.NoError(t, store.CreateDestination(ctx, newMCPDestination(idgen.String())))
		})

		t.Run("tenant created again", func(t *testing.T) {
			require.NoError(t, store.UpsertTenant(ctx, tenant))
			require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID)))
		})
	})

	// Every create racing DeleteTenant is either refused or deleted with
	// the tenant.
	t.Run("DeleteTenantRacingCreates", func(t *testing.T) {
		// Fewer than the optimistic retries of a driver that has them (each
		// takes a create landing in its window), so DeleteTenant succeeds.
		const creators = 6
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)
		store, err := h.MakeDriverWithOptions(ctx, DriverOptions{TypeLimits: map[string]int{"mcp": 1000}})
		require.NoError(t, err)
		principal := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 1000})

		for round := range 10 {
			tenant := testutil.TenantFactory.Any()
			require.NoError(t, store.UpsertTenant(ctx, tenant))
			require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID), principal))

			var mu sync.Mutex
			var created []models.Destination
			var wg sync.WaitGroup
			start := make(chan struct{})
			for range creators {
				wg.Go(func() {
					<-start
					d := newMCPDestination(tenant.ID)
					err := store.CreateDestination(ctx, d, principal)
					if err == nil {
						mu.Lock()
						created = append(created, d)
						mu.Unlock()
						return
					}
					assert.ErrorIs(t, err, driver.ErrTenantDeleted, "round %d", round)
				})
			}
			wg.Go(func() {
				<-start
				assert.NoError(t, store.DeleteTenant(ctx, tenant.ID), "round %d", round)
			})
			close(start)
			wg.Wait()

			for _, d := range created {
				_, err := store.RetrieveDestination(ctx, tenant.ID, d.ID)
				assert.ErrorIs(t, err, driver.ErrDestinationDeleted, "round %d: a create that succeeded is deleted with its tenant", round)
			}
			list, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID})
			require.NoError(t, err)
			assert.Empty(t, list, "round %d", round)

			// The buckets went with the tenant.
			require.NoError(t, store.UpsertTenant(ctx, tenant))
			one := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 1})
			require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID), one), "round %d", round)
		}
	})

	t.Run("Fence", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		fenceAt := time.Now()
		require.NoError(t, store.WriteFence(ctx, tenantID, "mcp_revoked:abc", fenceAt, time.Minute))

		t.Run("rejects a create started before the fence", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			err := store.CreateDestination(ctx, d,
				driver.WithNotDeletedSince(fenceAt.Add(-time.Second)), driver.WithFence("mcp_revoked:abc"))
			require.ErrorIs(t, err, driver.ErrDestinationRevoked)
			got, err := store.RetrieveDestination(ctx, tenantID, d.ID)
			require.NoError(t, err)
			assert.Nil(t, got)
		})

		t.Run("rejects a create started at the fence", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			err := store.CreateDestination(ctx, d,
				driver.WithNotDeletedSince(fenceAt), driver.WithFence("mcp_revoked:abc"))
			require.ErrorIs(t, err, driver.ErrDestinationRevoked)
		})

		t.Run("accepts a create started after the fence", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			require.NoError(t, store.CreateDestination(ctx, d,
				driver.WithNotDeletedSince(fenceAt.Add(time.Second)), driver.WithFence("mcp_revoked:abc")))
		})

		t.Run("ignores other fences", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			require.NoError(t, store.CreateDestination(ctx, d,
				driver.WithNotDeletedSince(fenceAt.Add(-time.Second)), driver.WithFence("mcp_revoked:other")))
		})

		t.Run("ignores fences of other tenants", func(t *testing.T) {
			d := newMCPDestination(idgen.String())
			require.NoError(t, store.CreateDestination(ctx, d,
				driver.WithNotDeletedSince(fenceAt.Add(-time.Second)), driver.WithFence("mcp_revoked:abc")))
		})

		t.Run("rejects an update started before the fence", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			require.NoError(t, store.CreateDestination(ctx, d))
			d.Metadata = map[string]string{"k": "v"}
			_, err := store.UpdateDestinationIfLive(ctx, d, d.CreatedAt,
				driver.WithNotDeletedSince(fenceAt.Add(-time.Second)), driver.WithFence("mcp_revoked:abc"))
			require.ErrorIs(t, err, driver.ErrDestinationRevoked)
			got, err := store.RetrieveDestination(ctx, tenantID, d.ID)
			require.NoError(t, err)
			assert.Nil(t, got.Metadata, "a revoked update must not write")
		})

		t.Run("only moves forward", func(t *testing.T) {
			require.NoError(t, store.WriteFence(ctx, tenantID, "mcp_revoked:abc", fenceAt.Add(-time.Hour), time.Minute))
			d := newMCPDestination(tenantID)
			err := store.CreateDestination(ctx, d,
				driver.WithNotDeletedSince(fenceAt.Add(-time.Millisecond)), driver.WithFence("mcp_revoked:abc"))
			require.ErrorIs(t, err, driver.ErrDestinationRevoked)
		})

		t.Run("validation", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			assert.Error(t, store.CreateDestination(ctx, d, driver.WithFence("mcp_revoked:abc")), "fence without since")
			assert.Error(t, store.CreateDestination(ctx, d, driver.WithNotDeletedSince(fenceAt), driver.WithFence("")))
			assert.Error(t, store.CreateDestination(ctx, d, driver.WithNotDeletedSince(fenceAt), driver.WithFence("a b")))
			assert.Error(t, store.WriteFence(ctx, tenantID, "bad\nname", fenceAt, time.Minute))
			assert.Error(t, store.WriteFence(ctx, tenantID, "ok", fenceAt, 0))
		})
	})

	t.Run("UpdateDestinationIfLive", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		original := newMCPDestination(tenantID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Minute)))
		require.NoError(t, store.CreateDestination(ctx, original))

		t.Run("updates the live generation", func(t *testing.T) {
			updated := original
			later := time.Now().Add(time.Hour)
			updated.ExpiresAt = &later
			updated.Credentials = map[string]string{"secret": "rotated"}
			updated.Metadata = map[string]string{"k": "v"}
			updated.CreatedAt = time.Now().Add(time.Hour) // ignored: the stored generation is kept
			updated.UpdatedAt = time.Now()

			res, err := store.UpdateDestinationIfLive(ctx, updated, original.CreatedAt)
			require.NoError(t, err)
			assert.False(t, res.WasDisabled)
			assert.Empty(t, res.ResumeKey)

			got, err := store.RetrieveDestination(ctx, tenantID, original.ID)
			require.NoError(t, err)
			expected := updated
			expected.CreatedAt = original.CreatedAt
			assertEqualDestination(t, expected, *got)
			original = *got
		})

		t.Run("re-enables and reports it", func(t *testing.T) {
			changed, err := store.DisableDestination(ctx, tenantID, original.ID, time.Now())
			require.NoError(t, err)
			require.True(t, changed)

			res, err := store.UpdateDestinationIfLive(ctx, original, original.CreatedAt)
			require.NoError(t, err)
			assert.True(t, res.WasDisabled)
			got, err := store.RetrieveDestination(ctx, tenantID, original.ID)
			require.NoError(t, err)
			assert.Nil(t, got.DisabledAt)
		})

		t.Run("not found", func(t *testing.T) {
			_, err := store.UpdateDestinationIfLive(ctx, newMCPDestination(tenantID), time.Now())
			require.ErrorIs(t, err, driver.ErrDestinationNotFound)
		})

		t.Run("conflict on another generation", func(t *testing.T) {
			_, err := store.UpdateDestinationIfLive(ctx, original, original.CreatedAt.Add(-time.Second))
			require.ErrorIs(t, err, driver.ErrDestinationConflict)
		})

		t.Run("delete between retrieve and update does not resurrect", func(t *testing.T) {
			retrieved, err := store.RetrieveDestination(ctx, tenantID, original.ID)
			require.NoError(t, err)

			res, err := store.DeleteDestinationIf(ctx, tenantID, original.ID, driver.DeleteCondition{Reason: driver.DeleteReasonUnsubscribed})
			require.NoError(t, err)
			require.True(t, res.Deleted)

			_, err = store.UpdateDestinationIfLive(ctx, *retrieved, retrieved.CreatedAt)
			require.ErrorIs(t, err, driver.ErrDestinationDeleted)
			assertNotLive(t, ctx, store, original)

			changed, err := store.DisableDestination(ctx, tenantID, original.ID, time.Now())
			require.ErrorIs(t, err, driver.ErrDestinationDeleted)
			assert.False(t, changed)
			assertNotLive(t, ctx, store, original)
		})

		t.Run("conflict after re-creation", func(t *testing.T) {
			stale := original
			recreated := original
			recreated.CreatedAt = original.CreatedAt.Add(time.Second)
			require.NoError(t, store.CreateDestination(ctx, recreated))

			_, err := store.UpdateDestinationIfLive(ctx, stale, stale.CreatedAt)
			require.ErrorIs(t, err, driver.ErrDestinationConflict)
		})
	})

	t.Run("DisableDestination", func(t *testing.T) {
		ctx, store := newStore(t)
		d := newMCPDestination(idgen.String(), testutil.DestinationFactory.WithFilter(models.Filter{"data": map[string]any{"x": "1"}}))
		require.NoError(t, store.CreateDestination(ctx, d))

		t.Run("does not revert a concurrent update", func(t *testing.T) {
			// The disabler read d, then a refresh committed.
			refreshed := d
			later := time.Now().Add(time.Hour)
			refreshed.ExpiresAt = &later
			refreshed.Credentials = map[string]string{"secret": "new"}
			_, err := store.UpdateDestinationIfLive(ctx, refreshed, d.CreatedAt)
			require.NoError(t, err)

			at := time.Now()
			changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, at)
			require.NoError(t, err)
			assert.True(t, changed)

			got, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
			require.NoError(t, err)
			assertEqualTimePtr(t, &at, got.DisabledAt, "DisabledAt")
			assertEqualTimePtr(t, &later, got.ExpiresAt, "ExpiresAt")
			assert.Equal(t, "new", got.Credentials["secret"])
			assert.Equal(t, d.Filter, got.Filter)

			matched, err := matchedIDs(store.MatchEvent(ctx, testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(d.TenantID),
				testutil.EventFactory.WithTopic("order.created"),
			), true))
			require.NoError(t, err)
			assert.NotContains(t, matched, d.ID)

			list, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: d.TenantID})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.NotNil(t, list[0].DisabledAt)
		})

		t.Run("already disabled", func(t *testing.T) {
			changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now().Add(time.Hour))
			require.NoError(t, err)
			assert.False(t, changed)
		})

		t.Run("missing", func(t *testing.T) {
			_, err := store.DisableDestination(ctx, d.TenantID, idgen.Destination(), time.Now())
			require.ErrorIs(t, err, driver.ErrDestinationNotFound)
		})
	})

	t.Run("EnableDestination", func(t *testing.T) {
		matches := func(t *testing.T, ctx context.Context, store driver.TenantStore, d models.Destination, data string) bool {
			t.Helper()
			matched, err := matchedIDs(store.MatchEvent(ctx, testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(d.TenantID),
				testutil.EventFactory.WithTopic("order.created"),
				testutil.EventFactory.WithData(json.RawMessage(data)),
			), true))
			require.NoError(t, err)
			return slices.Contains(matched, d.ID)
		}
		filterX := models.Filter{"data": map[string]any{"x": "1"}}

		t.Run("re-enables and reports it", func(t *testing.T) {
			ctx, store := newStore(t)
			d := newMCPDestination(idgen.String(), testutil.DestinationFactory.WithFilter(filterX))
			require.NoError(t, store.CreateDestination(ctx, d))
			changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now())
			require.NoError(t, err)
			require.True(t, changed)
			require.False(t, matches(t, ctx, store, d, `{"x":"1"}`))

			res, err := store.EnableDestination(ctx, d.TenantID, d.ID)
			require.NoError(t, err)
			assert.True(t, res.WasDisabled)
			assert.Empty(t, res.ResumeKey)

			got, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
			require.NoError(t, err)
			assert.Nil(t, got.DisabledAt)
			assertEqualDestination(t, d, *got)
			assert.True(t, matches(t, ctx, store, d, `{"x":"1"}`), "matched again")
			list, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: d.TenantID})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Nil(t, list[0].DisabledAt)

			t.Run("already enabled", func(t *testing.T) {
				res, err := store.EnableDestination(ctx, d.TenantID, d.ID)
				require.NoError(t, err)
				assert.False(t, res.WasDisabled)
				again, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
				require.NoError(t, err)
				assertEqualDestination(t, *got, *again)
			})
		})

		// The enabler read the destination, then a refresh committed (new
		// expiry, secret and filter): enabling keeps every refreshed field.
		t.Run("does not revert a refresh between read and enable", func(t *testing.T) {
			for _, refreshEnables := range []bool{false, true} {
				ctx, store := newStore(t)
				d := newMCPDestination(idgen.String(),
					testutil.DestinationFactory.WithFilter(filterX),
					testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Minute)))
				require.NoError(t, store.CreateDestination(ctx, d))
				changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now())
				require.NoError(t, err)
				require.True(t, changed)

				read, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
				require.NoError(t, err)
				refreshed := *read
				later := time.Now().Add(time.Hour)
				refreshed.ExpiresAt = &later
				refreshed.Credentials = map[string]string{"secret": "new"}
				refreshed.Filter = models.Filter{"data": map[string]any{"x": "2"}}
				if refreshEnables {
					refreshed.DisabledAt = nil
				}
				_, err = store.UpdateDestinationIfLive(ctx, refreshed, read.CreatedAt)
				require.NoError(t, err)

				res, err := store.EnableDestination(ctx, d.TenantID, d.ID)
				require.NoError(t, err)
				assert.Equal(t, !refreshEnables, res.WasDisabled, "refresh enables: %v", refreshEnables)

				got, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
				require.NoError(t, err)
				assert.Nil(t, got.DisabledAt)
				assertEqualTimePtr(t, &later, got.ExpiresAt, "ExpiresAt")
				assert.Equal(t, "new", got.Credentials["secret"])
				assert.Equal(t, refreshed.Filter, got.Filter)
				assert.True(t, matches(t, ctx, store, d, `{"x":"2"}`), "the refreshed filter matches")
				assert.False(t, matches(t, ctx, store, d, `{"x":"1"}`), "the old filter is gone")
			}
		})

		t.Run("concurrent refreshes are never reverted", func(t *testing.T) {
			ctx, store := newStore(t)
			for round := range 10 {
				d := newMCPDestination(idgen.String(), testutil.DestinationFactory.WithFilter(filterX))
				require.NoError(t, store.CreateDestination(ctx, d))
				changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now())
				require.NoError(t, err)
				require.True(t, changed)

				refreshed := d
				now := time.Now()
				refreshed.DisabledAt = &now
				refreshed.Filter = models.Filter{"data": map[string]any{"x": "2"}}
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					_, err := store.UpdateDestinationIfLive(ctx, refreshed, d.CreatedAt)
					assert.NoError(t, err)
				}()
				go func() {
					defer wg.Done()
					_, err := store.EnableDestination(ctx, d.TenantID, d.ID)
					assert.NoError(t, err)
				}()
				wg.Wait()

				// Whatever the order, the stored filter is the refresh's and
				// matching agrees with the stored destination.
				got, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
				require.NoError(t, err)
				assert.Equal(t, refreshed.Filter, got.Filter, "round %d", round)
				assert.Equal(t, got.DisabledAt == nil, matches(t, ctx, store, d, `{"x":"2"}`), "round %d", round)
				assert.False(t, matches(t, ctx, store, d, `{"x":"1"}`), "round %d", round)
			}
		})

		t.Run("not live", func(t *testing.T) {
			ctx, store := newStore(t)
			d := newMCPDestination(idgen.String(), testutil.DestinationFactory.WithDisabledAt(time.Now()))
			require.NoError(t, store.CreateDestination(ctx, d))

			_, err := store.EnableDestination(ctx, d.TenantID, idgen.Destination())
			require.ErrorIs(t, err, driver.ErrDestinationNotFound)

			res, err := store.DeleteDestinationIf(ctx, d.TenantID, d.ID, driver.DeleteCondition{Reason: driver.DeleteReasonRevoked})
			require.NoError(t, err)
			require.True(t, res.Deleted)
			_, err = store.EnableDestination(ctx, d.TenantID, d.ID, driver.WithResumeParkedRetries())
			require.ErrorIs(t, err, driver.ErrDestinationDeleted)
			assertNotLive(t, ctx, store, d)
		})

		t.Run("takes only WithResumeParkedRetries", func(t *testing.T) {
			ctx, store := newStore(t)
			d := newMCPDestination(idgen.String(), testutil.DestinationFactory.WithDisabledAt(time.Now()))
			require.NoError(t, store.CreateDestination(ctx, d))
			for _, opt := range []driver.WriteOption{
				driver.WithBuckets(driver.Bucket{Name: "b"}),
				driver.WithNotDeletedSince(time.Now()),
			} {
				_, err := store.EnableDestination(ctx, d.TenantID, d.ID, opt)
				assert.Error(t, err)
			}
			got, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
			require.NoError(t, err)
			assert.NotNil(t, got.DisabledAt, "unchanged")
		})
	})

	t.Run("DeleteDestinationIf", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		expiresAt := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
		d := newMCPDestination(tenantID, testutil.DestinationFactory.WithExpiresAt(expiresAt))
		require.NoError(t, store.CreateDestination(ctx, d))

		t.Run("missing", func(t *testing.T) {
			res, err := store.DeleteDestinationIf(ctx, tenantID, idgen.Destination(), driver.DeleteCondition{})
			require.NoError(t, err)
			assert.Equal(t, driver.DeleteResult{Gone: true}, res)
		})

		live := func(t *testing.T, c driver.DeleteCondition) {
			t.Helper()
			res, err := store.DeleteDestinationIf(ctx, tenantID, d.ID, c)
			require.NoError(t, err)
			assert.True(t, res.Live)
			assert.False(t, res.Deleted)
			assert.Equal(t, expiresAt.UnixMilli(), res.ExpiresAtMs)
			assert.Equal(t, "mcp", res.Type)
			got, err := store.RetrieveDestination(ctx, tenantID, d.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
		}

		t.Run("other type", func(t *testing.T) {
			live(t, driver.DeleteCondition{Type: "webhook"})
		})
		t.Run("other generation", func(t *testing.T) {
			other := d.CreatedAt.Add(time.Millisecond)
			live(t, driver.DeleteCondition{ExpectedCreatedAt: &other})
		})
		t.Run("not expired yet", func(t *testing.T) {
			before := expiresAt.Add(-time.Millisecond)
			live(t, driver.DeleteCondition{ExpiredBefore: &before})
		})

		t.Run("deletes on match, expiry inclusive", func(t *testing.T) {
			createdAt := d.CreatedAt
			res, err := store.DeleteDestinationIf(ctx, tenantID, d.ID, driver.DeleteCondition{
				Type:              "mcp",
				ExpectedCreatedAt: &createdAt,
				ExpiredBefore:     &expiresAt,
				Reason:            driver.DeleteReasonExpired,
			})
			require.NoError(t, err)
			assert.Equal(t, driver.DeleteResult{
				Deleted:     true,
				ExpiresAtMs: expiresAt.UnixMilli(),
				Type:        "mcp",
				Topics:      []string{"order.created"},
			}, res)
			assertNotLive(t, ctx, store, d)
		})

		t.Run("gone afterwards", func(t *testing.T) {
			res, err := store.DeleteDestinationIf(ctx, tenantID, d.ID, driver.DeleteCondition{})
			require.NoError(t, err)
			assert.True(t, res.Gone)
			assert.False(t, res.Deleted)
			require.NoError(t, store.DeleteDestination(ctx, tenantID, d.ID), "generic delete of a tombstone")
		})

		t.Run("no expiry never matches ExpiredBefore", func(t *testing.T) {
			forever := newMCPDestination(tenantID)
			require.NoError(t, store.CreateDestination(ctx, forever))
			now := time.Now().Add(time.Hour)
			res, err := store.DeleteDestinationIf(ctx, tenantID, forever.ID, driver.DeleteCondition{ExpiredBefore: &now})
			require.NoError(t, err)
			assert.True(t, res.Live)
			assert.Zero(t, res.ExpiresAtMs)
			assert.Equal(t, driver.NoExpiryScore, res.Score())
		})
	})

	t.Run("RefreshVersusSweep", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()

		t.Run("refresh first keeps the destination", func(t *testing.T) {
			d := newMCPDestination(tenantID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(-time.Second)))
			require.NoError(t, store.CreateDestination(ctx, d))
			// The sweeper listed d as expired; the client refreshes first.
			later := time.Now().Add(time.Hour).Truncate(time.Millisecond)
			refreshed := d
			refreshed.ExpiresAt = &later
			_, err := store.UpdateDestinationIfLive(ctx, refreshed, d.CreatedAt)
			require.NoError(t, err)

			now := time.Now()
			res, err := store.DeleteDestinationIf(ctx, tenantID, d.ID, driver.DeleteCondition{ExpiredBefore: &now, Reason: driver.DeleteReasonExpired})
			require.NoError(t, err)
			assert.True(t, res.Live)
			assert.Equal(t, later.UnixMilli(), res.ExpiresAtMs)
		})

		t.Run("sweep first wins", func(t *testing.T) {
			d := newMCPDestination(tenantID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(-time.Second)))
			require.NoError(t, store.CreateDestination(ctx, d))
			now := time.Now()
			res, err := store.DeleteDestinationIf(ctx, tenantID, d.ID, driver.DeleteCondition{ExpiredBefore: &now, Reason: driver.DeleteReasonExpired})
			require.NoError(t, err)
			require.True(t, res.Deleted)

			later := time.Now().Add(time.Hour)
			refreshed := d
			refreshed.ExpiresAt = &later
			_, err = store.UpdateDestinationIfLive(ctx, refreshed, d.CreatedAt)
			require.ErrorIs(t, err, driver.ErrDestinationDeleted)
			assertNotLive(t, ctx, store, d)

			// The client then subscribes anew over the expired tombstone.
			require.NoError(t, store.CreateDestination(ctx, refreshed, driver.WithNotDeletedSince(now.Add(-time.Minute))))
		})

		t.Run("concurrent", func(t *testing.T) {
			for range 10 {
				d := newMCPDestination(tenantID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(-time.Second)))
				require.NoError(t, store.CreateDestination(ctx, d))
				later := time.Now().Add(time.Hour)
				refreshed := d
				refreshed.ExpiresAt = &later

				var wg sync.WaitGroup
				var res driver.DeleteResult
				var delErr, updErr error
				wg.Add(2)
				go func() {
					defer wg.Done()
					now := time.Now()
					res, delErr = store.DeleteDestinationIf(ctx, tenantID, d.ID, driver.DeleteCondition{ExpiredBefore: &now})
				}()
				go func() {
					defer wg.Done()
					_, updErr = store.UpdateDestinationIfLive(ctx, refreshed, d.CreatedAt)
				}()
				wg.Wait()
				require.NoError(t, delErr)

				if res.Deleted {
					require.ErrorIs(t, updErr, driver.ErrDestinationDeleted)
					assertNotLive(t, ctx, store, d)
				} else {
					require.NoError(t, updErr)
					require.True(t, res.Live)
					got, err := store.RetrieveDestination(ctx, tenantID, d.ID)
					require.NoError(t, err)
					assertEqualTimePtr(t, &later, got.ExpiresAt, "ExpiresAt")
				}
			}
		})
	})
}
