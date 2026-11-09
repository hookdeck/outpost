package drivertest

import (
	"context"
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

func testInternal(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	setup := func(t *testing.T, maxDest int) (context.Context, driver.TenantStore, models.Tenant) {
		t.Helper()
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)
		store, err := h.MakeDriverWithMaxDest(ctx, maxDest)
		require.NoError(t, err)
		tenant := models.Tenant{ID: idgen.String(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		return ctx, store, tenant
	}
	webhook := func(tenantID string, opts ...func(*models.Destination)) models.Destination {
		return testutil.DestinationFactory.Any(append([]func(*models.Destination){
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithType("webhook"),
		}, opts...)...)
	}
	internal := func(tenantID string, opts ...func(*models.Destination)) models.Destination {
		return testutil.DestinationFactory.Any(append([]func(*models.Destination){
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithType(models.DestinationTypeMCP),
		}, opts...)...)
	}
	ids := func(destinations []models.Destination) []string {
		out := make([]string, len(destinations))
		for i, d := range destinations {
			out[i] = d.ID
		}
		return out
	}

	t.Run("ExcludeInternal on list", func(t *testing.T) {
		ctx, store, tenant := setup(t, 20)
		hook := webhook(tenant.ID)
		sub := internal(tenant.ID)
		require.NoError(t, store.CreateDestination(ctx, hook))
		require.NoError(t, store.CreateDestination(ctx, sub))

		all, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{hook.ID, sub.ID}, ids(all), "internal destinations are listed by default")

		listed, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID, ExcludeInternal: true})
		require.NoError(t, err)
		assert.Equal(t, []string{hook.ID}, ids(listed))

		byID, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID, IDs: []string{hook.ID, sub.ID}, ExcludeInternal: true})
		require.NoError(t, err)
		assert.Equal(t, []string{hook.ID}, ids(byID))

		byID, err = store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID, IDs: []string{sub.ID}})
		require.NoError(t, err)
		assert.Equal(t, []string{sub.ID}, ids(byID))
	})

	t.Run("tenant count and topics exclude internal destinations", func(t *testing.T) {
		ctx, store, tenant := setup(t, 20)
		require.NoError(t, store.CreateDestination(ctx, webhook(tenant.ID, testutil.DestinationFactory.WithTopics([]string{"user.created"}))))
		require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID, testutil.DestinationFactory.WithTopics([]string{"user.deleted"}))))

		got, err := store.RetrieveTenant(ctx, tenant.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.DestinationsCount)
		assert.Equal(t, []string{"user.created"}, got.Topics)

		other := models.Tenant{ID: idgen.String(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		require.NoError(t, store.UpsertTenant(ctx, other))
		require.NoError(t, store.CreateDestination(ctx, internal(other.ID, testutil.DestinationFactory.WithTopics([]string{"user.deleted"}))))
		got, err = store.RetrieveTenant(ctx, other.ID)
		require.NoError(t, err)
		assert.Zero(t, got.DestinationsCount)
		assert.Empty(t, got.Topics)
	})

	t.Run("limit ignores internal destinations", func(t *testing.T) {
		ctx, store, tenant := setup(t, 2)
		for range 3 {
			require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID)))
		}
		require.NoError(t, store.CreateDestination(ctx, webhook(tenant.ID)))
		require.NoError(t, store.CreateDestination(ctx, webhook(tenant.ID)))
		require.ErrorIs(t, store.CreateDestination(ctx, webhook(tenant.ID)), driver.ErrMaxDestinationsPerTenantReached)
		require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID)), "internal destinations are created at the limit")

		got, err := store.RetrieveTenant(ctx, tenant.ID)
		require.NoError(t, err)
		assert.Equal(t, 2, got.DestinationsCount)
	})

	t.Run("a negative limit refuses every webhook even with internal destinations present", func(t *testing.T) {
		ctx, store, tenant := setup(t, -1)
		require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID)))
		require.ErrorIs(t, store.CreateDestination(ctx, webhook(tenant.ID)), driver.ErrMaxDestinationsPerTenantReached)
	})

	t.Run("limit stays atomic with internal destinations present", func(t *testing.T) {
		ctx, store, _ := setup(t, 3)

		for round := 0; round < 10; round++ {
			tenant := models.Tenant{ID: idgen.String(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
			require.NoError(t, store.UpsertTenant(ctx, tenant))
			for range 5 {
				require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID)))
			}
			for range 2 {
				require.NoError(t, store.CreateDestination(ctx, webhook(tenant.ID)))
			}

			errs := make([]error, 16)
			var wg sync.WaitGroup
			for i := range errs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = store.CreateDestination(ctx, webhook(tenant.ID))
				}()
			}
			wg.Wait()

			created := 0
			for _, err := range errs {
				if err == nil {
					created++
				} else {
					assert.ErrorIs(t, err, driver.ErrMaxDestinationsPerTenantReached)
				}
			}
			assert.Equal(t, 1, created)
			got, err := store.RetrieveTenant(ctx, tenant.ID)
			require.NoError(t, err)
			assert.Equal(t, 3, got.DestinationsCount)
		}
	})

	t.Run("creating an internal destination removes expired ones", func(t *testing.T) {
		ctx, store, tenant := setup(t, 20)
		expired := internal(tenant.ID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(-time.Minute)))
		live := internal(tenant.ID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)))
		require.NoError(t, store.CreateDestination(ctx, expired))
		require.NoError(t, store.CreateDestination(ctx, live))

		require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)))))

		_, err := store.RetrieveDestination(ctx, tenant.ID, expired.ID)
		require.ErrorIs(t, err, driver.ErrDestinationDeleted)
		kept, err := store.RetrieveDestination(ctx, tenant.ID, live.ID)
		require.NoError(t, err)
		require.NotNil(t, kept)
	})

	t.Run("one create removes a bounded number of expired internal destinations", func(t *testing.T) {
		ctx, store, tenant := setup(t, 20)
		const expiredCount = 250
		expiredIDs := make([]string, expiredCount)
		for i := range expiredIDs {
			d := internal(tenant.ID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(-time.Minute)))
			expiredIDs[i] = d.ID
			require.NoError(t, store.UpsertDestination(ctx, d))
		}
		remaining := func() int {
			n := 0
			for _, id := range expiredIDs {
				if d, err := store.RetrieveDestination(ctx, tenant.ID, id); err == nil && d != nil {
					n++
				}
			}
			return n
		}

		require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)))))
		first := remaining()
		assert.Equal(t, 150, first)

		for range 3 {
			require.NoError(t, store.CreateDestination(ctx, internal(tenant.ID, testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)))))
		}
		assert.Zero(t, remaining())
	})
}
