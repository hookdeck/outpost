package drivertest

import (
	"context"
	"fmt"
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

func testParking(t *testing.T, newHarness HarnessMaker) {
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
	expireAt := func() time.Time { return time.Now().Add(time.Hour) }
	// disabled creates a disabled destination.
	disabled := func(t *testing.T, ctx context.Context, store driver.TenantStore) models.Destination {
		t.Helper()
		d := newMCPDestination(idgen.String(), testutil.DestinationFactory.WithDisabledAt(time.Now()))
		require.NoError(t, store.CreateDestination(ctx, d))
		return d
	}
	// resume enables d, moving its parked retries to a resume set.
	resume := func(t *testing.T, ctx context.Context, store driver.TenantStore, d models.Destination) string {
		t.Helper()
		d.DisabledAt = nil
		res, err := store.UpdateDestinationIfLive(ctx, d, d.CreatedAt, driver.WithResumeParkedRetries())
		require.NoError(t, err)
		return res.ResumeKey
	}
	popAll := func(t *testing.T, ctx context.Context, store driver.TenantStore, tenantID, key string) []string {
		t.Helper()
		var all []string
		for {
			members, err := store.PopResumeMembers(ctx, tenantID, key, 3)
			require.NoError(t, err)
			if len(members) == 0 {
				return all
			}
			all = append(all, members...)
		}
	}

	t.Run("ParkOnlyWhileDisabled", func(t *testing.T) {
		ctx, store := newStore(t)
		enabled := newMCPDestination(idgen.String())
		require.NoError(t, store.CreateDestination(ctx, enabled))

		res, err := store.ParkRetry(ctx, enabled.TenantID, enabled.ID, "r1", 10, expireAt())
		require.NoError(t, err)
		assert.Equal(t, driver.ParkResultEnabled, res)

		res, err = store.ParkRetry(ctx, enabled.TenantID, idgen.Destination(), "r1", 10, expireAt())
		require.NoError(t, err)
		assert.Equal(t, driver.ParkResultGone, res, "missing")

		d := disabled(t, ctx, store)
		for _, member := range []string{"r1", "r2", "r1"} {
			res, err = store.ParkRetry(ctx, d.TenantID, d.ID, member, 2, expireAt())
			require.NoError(t, err)
			assert.Equal(t, driver.ParkResultParked, res, member)
		}
		res, err = store.ParkRetry(ctx, d.TenantID, d.ID, "r3", 2, expireAt())
		require.NoError(t, err)
		assert.Equal(t, driver.ParkResultFull, res)

		res2, err := store.DeleteDestinationIf(ctx, d.TenantID, d.ID, driver.DeleteCondition{})
		require.NoError(t, err)
		require.True(t, res2.Deleted)
		res, err = store.ParkRetry(ctx, d.TenantID, d.ID, "r4", 2, expireAt())
		require.NoError(t, err)
		assert.Equal(t, driver.ParkResultGone, res, "deleted")

		t.Run("validation", func(t *testing.T) {
			_, err := store.ParkRetry(ctx, d.TenantID, d.ID, "", 2, expireAt())
			assert.Error(t, err)
			_, err = store.ParkRetry(ctx, d.TenantID, d.ID, "r", 0, expireAt())
			assert.Error(t, err)
			_, err = store.ParkRetry(ctx, d.TenantID, d.ID, "r", 2, time.Time{})
			assert.Error(t, err)
		})
	})

	t.Run("Resume", func(t *testing.T) {
		ctx, store := newStore(t)
		d := disabled(t, ctx, store)
		for _, member := range []string{"r1", "r2", "r3", "r4"} {
			res, err := store.ParkRetry(ctx, d.TenantID, d.ID, member, 10, expireAt())
			require.NoError(t, err)
			require.Equal(t, driver.ParkResultParked, res)
		}

		key := resume(t, ctx, store, d)
		require.NotEmpty(t, key)
		res, err := store.ParkRetry(ctx, d.TenantID, d.ID, "r5", 10, expireAt())
		require.NoError(t, err)
		assert.Equal(t, driver.ParkResultEnabled, res, "enabled again: deliver")

		assert.ElementsMatch(t, []string{"r1", "r2", "r3", "r4"}, popAll(t, ctx, store, d.TenantID, key))
		require.NoError(t, store.DeleteResumeSet(ctx, d.TenantID, key))

		t.Run("nothing parked", func(t *testing.T) {
			changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now())
			require.NoError(t, err)
			require.True(t, changed)
			assert.Empty(t, resume(t, ctx, store, d))
		})

		t.Run("not while the update keeps it disabled", func(t *testing.T) {
			changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now())
			require.NoError(t, err)
			require.True(t, changed)
			_, err = store.ParkRetry(ctx, d.TenantID, d.ID, "r6", 10, expireAt())
			require.NoError(t, err)

			stillDisabled := d
			now := time.Now()
			stillDisabled.DisabledAt = &now
			res, err := store.UpdateDestinationIfLive(ctx, stillDisabled, d.CreatedAt, driver.WithResumeParkedRetries())
			require.NoError(t, err)
			assert.Empty(t, res.ResumeKey)

			key := resume(t, ctx, store, d)
			require.NotEmpty(t, key)
			assert.Equal(t, []string{"r6"}, popAll(t, ctx, store, d.TenantID, key))
		})

		t.Run("not without the option", func(t *testing.T) {
			changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now())
			require.NoError(t, err)
			require.True(t, changed)
			_, err = store.ParkRetry(ctx, d.TenantID, d.ID, "r7", 10, expireAt())
			require.NoError(t, err)

			res, err := store.UpdateDestinationIfLive(ctx, d, d.CreatedAt)
			require.NoError(t, err)
			assert.True(t, res.WasDisabled)
			assert.Empty(t, res.ResumeKey)
		})
	})

	t.Run("ResumeKeyValidation", func(t *testing.T) {
		ctx, store := newStore(t)
		d := disabled(t, ctx, store)
		_, err := store.ParkRetry(ctx, d.TenantID, d.ID, "r1", 10, expireAt())
		require.NoError(t, err)
		key := resume(t, ctx, store, d)
		require.NotEmpty(t, key)

		for _, tc := range []struct{ tenantID, key string }{
			{idgen.String(), key},
			{d.TenantID, "tenant:{" + d.TenantID + "}:destination:" + d.ID},
			{d.TenantID, "tenant:{" + d.TenantID + "}:destinations"},
			{d.TenantID, "unrelated"},
		} {
			_, err := store.PopResumeMembers(ctx, tc.tenantID, tc.key, 1)
			assert.ErrorIs(t, err, driver.ErrInvalidResumeKey, tc.key)
			assert.ErrorIs(t, store.DeleteResumeSet(ctx, tc.tenantID, tc.key), driver.ErrInvalidResumeKey, tc.key)
		}
		_, err = store.PopResumeMembers(ctx, d.TenantID, key, 0)
		assert.Error(t, err)

		got, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
		require.NoError(t, err)
		require.NotNil(t, got, "the destination is untouched")
		assert.Equal(t, []string{"r1"}, popAll(t, ctx, store, d.TenantID, key))
	})

	t.Run("DeletesDropParkedRetries", func(t *testing.T) {
		ctx, store := newStore(t)

		d := disabled(t, ctx, store)
		_, err := store.ParkRetry(ctx, d.TenantID, d.ID, "r1", 10, expireAt())
		require.NoError(t, err)
		res, err := store.DeleteDestinationIf(ctx, d.TenantID, d.ID, driver.DeleteCondition{})
		require.NoError(t, err)
		require.True(t, res.Deleted)
		require.NoError(t, store.CreateDestination(ctx, d))
		assert.Empty(t, resume(t, ctx, store, d), "a new generation starts with no parked retries")

		tenant := testutil.TenantFactory.Any()
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		e := newMCPDestination(tenant.ID, testutil.DestinationFactory.WithDisabledAt(time.Now()))
		require.NoError(t, store.CreateDestination(ctx, e))
		_, err = store.ParkRetry(ctx, e.TenantID, e.ID, "r1", 10, expireAt())
		require.NoError(t, err)
		require.NoError(t, store.DeleteTenant(ctx, tenant.ID))
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		require.NoError(t, store.CreateDestination(ctx, e))
		assert.Empty(t, resume(t, ctx, store, e))
	})

	t.Run("ExpiredParkedSet", func(t *testing.T) {
		ctx, store := newStore(t)
		d := disabled(t, ctx, store)
		_, err := store.ParkRetry(ctx, d.TenantID, d.ID, "r1", 10, time.Now().Add(-time.Second))
		require.NoError(t, err)
		assert.Empty(t, resume(t, ctx, store, d))
	})

	t.Run("ParkWhileResuming", func(t *testing.T) {
		ctx, store := newStore(t)
		for round := range 5 {
			d := disabled(t, ctx, store)
			const parkers = 20

			var wg sync.WaitGroup
			var mu sync.Mutex
			var parked []string
			var key string
			for i := range parkers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					member := fmt.Sprintf("r%d", i)
					res, err := store.ParkRetry(ctx, d.TenantID, d.ID, member, 1000, expireAt())
					assert.NoError(t, err)
					assert.Contains(t, []driver.ParkResult{driver.ParkResultParked, driver.ParkResultEnabled}, res)
					if res == driver.ParkResultParked {
						mu.Lock()
						parked = append(parked, member)
						mu.Unlock()
					}
				}()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				enabled := d
				enabled.DisabledAt = nil
				res, err := store.UpdateDestinationIfLive(ctx, enabled, d.CreatedAt, driver.WithResumeParkedRetries())
				assert.NoError(t, err)
				key = res.ResumeKey
			}()
			wg.Wait()

			// Every retry parked before the resume is in its set; none was
			// parked after it, so nothing is left behind.
			var resumed []string
			if key != "" {
				resumed = popAll(t, ctx, store, d.TenantID, key)
			}
			assert.ElementsMatch(t, parked, resumed, "round %d", round)

			changed, err := store.DisableDestination(ctx, d.TenantID, d.ID, time.Now())
			require.NoError(t, err)
			require.True(t, changed)
			assert.Empty(t, resume(t, ctx, store, d), "round %d: nothing left parked", round)
		}
	})
}
