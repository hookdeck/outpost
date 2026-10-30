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

func newTenantWithDestination(t *testing.T, ctx context.Context, store driver.TenantStore) (models.Tenant, models.Destination) {
	t.Helper()
	tenant := testutil.TenantFactory.Any()
	require.NoError(t, store.UpsertTenant(ctx, tenant))
	destination := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID))
	require.NoError(t, store.CreateDestination(ctx, destination))
	return tenant, destination
}

func testMutations(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	setup := func(t *testing.T, maxDest int) (context.Context, driver.TenantStore) {
		t.Helper()
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)
		var store driver.TenantStore
		if maxDest > 0 {
			store, err = h.MakeDriverWithMaxDest(ctx, maxDest)
		} else {
			store, err = h.MakeDriver(ctx)
		}
		require.NoError(t, err)
		return ctx, store
	}

	t.Run("UpdateDestination writes a retrieved destination", func(t *testing.T) {
		ctx, store := setup(t, 0)
		tenant, destination := newTenantWithDestination(t, ctx, store)

		current, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)
		current.Metadata = map[string]string{"k": "v1"}
		require.NoError(t, store.UpdateDestination(ctx, *current))

		current, err = store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)
		assert.Equal(t, models.Metadata{"k": "v1"}, current.Metadata)

		current.Metadata = map[string]string{"k": "v2"}
		require.NoError(t, store.UpdateDestination(ctx, *current))
		current, err = store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)
		assert.Equal(t, models.Metadata{"k": "v2"}, current.Metadata)
	})

	t.Run("UpdateDestination of an unknown destination is not found", func(t *testing.T) {
		ctx, store := setup(t, 0)
		tenant, _ := newTenantWithDestination(t, ctx, store)

		unknown := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID))
		assert.ErrorIs(t, store.UpdateDestination(ctx, unknown), driver.ErrDestinationNotFound)

		destinations, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID})
		require.NoError(t, err)
		assert.Len(t, destinations, 1)
	})

	t.Run("UpdateDestination after delete does not resurrect", func(t *testing.T) {
		ctx, store := setup(t, 1)
		tenant, destination := newTenantWithDestination(t, ctx, store)

		read, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)
		require.NoError(t, store.DeleteDestination(ctx, tenant.ID, destination.ID))

		read.Metadata = map[string]string{"k": "late"}
		assert.ErrorIs(t, store.UpdateDestination(ctx, *read), driver.ErrDestinationNotFound)

		_, err = store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		assert.ErrorIs(t, err, driver.ErrDestinationDeleted)
		destinations, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID})
		require.NoError(t, err)
		assert.Empty(t, destinations)
		matched, err := store.MatchEvent(ctx, models.Event{TenantID: tenant.ID, Topic: destination.Topics[0]}, false)
		require.NoError(t, err)
		assert.Empty(t, matched)

		// The limit is 1: the dead destination must not count toward it.
		other := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID))
		require.NoError(t, store.CreateDestination(ctx, other))
	})

	t.Run("UpdateDestination from a stale read conflicts", func(t *testing.T) {
		ctx, store := setup(t, 0)
		tenant, destination := newTenantWithDestination(t, ctx, store)

		a, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)
		b, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)

		b.Metadata = map[string]string{"by": "b"}
		require.NoError(t, store.UpdateDestination(ctx, *b))

		now := time.Now()
		a.DisabledAt = &now
		assert.ErrorIs(t, store.UpdateDestination(ctx, *a), driver.ErrDestinationConflict)

		current, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)
		assert.Equal(t, models.Metadata{"by": "b"}, current.Metadata)
		assert.Nil(t, current.DisabledAt)

		// Re-reading and re-applying succeeds.
		current.DisabledAt = &now
		require.NoError(t, store.UpdateDestination(ctx, *current))
		current, err = store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)
		assert.Equal(t, models.Metadata{"by": "b"}, current.Metadata)
		assert.NotNil(t, current.DisabledAt)
	})

	t.Run("UpdateDestination conflicts with an unchecked write", func(t *testing.T) {
		ctx, store := setup(t, 0)
		tenant, destination := newTenantWithDestination(t, ctx, store)

		read, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
		require.NoError(t, err)

		other := *read
		other.Metadata = map[string]string{"by": "upsert"}
		require.NoError(t, store.UpsertDestination(ctx, other))

		read.Metadata = map[string]string{"by": "stale"}
		assert.ErrorIs(t, store.UpdateDestination(ctx, *read), driver.ErrDestinationConflict)
	})

	t.Run("concurrent updates from one version: exactly one wins", func(t *testing.T) {
		ctx, store := setup(t, 0)
		tenant, destination := newTenantWithDestination(t, ctx, store)

		const writers = 8
		reads := make([]models.Destination, writers)
		for i := range reads {
			read, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
			require.NoError(t, err)
			read.Metadata = map[string]string{"writer": idgen.String()}
			reads[i] = *read
		}

		errs := make([]error, writers)
		var wg sync.WaitGroup
		for i := range reads {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = store.UpdateDestination(ctx, reads[i])
			}()
		}
		wg.Wait()

		wins := 0
		for i, err := range errs {
			if err == nil {
				wins++
				current, rerr := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
				require.NoError(t, rerr)
				assert.Equal(t, reads[i].Metadata, current.Metadata)
			} else {
				assert.ErrorIs(t, err, driver.ErrDestinationConflict)
			}
		}
		assert.Equal(t, 1, wins)
	})

	t.Run("CreateDestination for an unknown tenant is tenant not found", func(t *testing.T) {
		ctx, store := setup(t, 0)
		destination := testutil.DestinationFactory.Any()
		assert.ErrorIs(t, store.CreateDestination(ctx, destination), driver.ErrTenantNotFound)

		destinations, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: destination.TenantID})
		require.NoError(t, err)
		assert.Empty(t, destinations)
	})

	t.Run("CreateDestination after DeleteTenant is tenant not found", func(t *testing.T) {
		ctx, store := setup(t, 0)
		tenant, _ := newTenantWithDestination(t, ctx, store)
		require.NoError(t, store.DeleteTenant(ctx, tenant.ID))

		late := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID))
		assert.ErrorIs(t, store.CreateDestination(ctx, late), driver.ErrTenantNotFound)

		// Re-creating the tenant must not bring the destination back.
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		destinations, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID})
		require.NoError(t, err)
		assert.Empty(t, destinations)
		retrieved, err := store.RetrieveTenant(ctx, tenant.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, retrieved.DestinationsCount)
	})

	t.Run("destination creates racing a tenant delete leave no live destination", func(t *testing.T) {
		ctx, store := setup(t, 0)

		for round := 0; round < 20; round++ {
			tenant := testutil.TenantFactory.Any()
			require.NoError(t, store.UpsertTenant(ctx, tenant))
			require.NoError(t, store.CreateDestination(ctx, testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenant.ID),
			)))

			const creators = 10
			created := make([]models.Destination, creators)
			errs := make([]error, creators)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := range created {
				created[i] = testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID))
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					errs[i] = store.CreateDestination(ctx, created[i])
				}()
			}
			var deleteErr error
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				deleteErr = store.DeleteTenant(ctx, tenant.ID)
			}()
			close(start)
			wg.Wait()

			require.NoError(t, deleteErr)
			for i, err := range errs {
				if err == nil {
					_, rerr := store.RetrieveDestination(ctx, tenant.ID, created[i].ID)
					assert.ErrorIs(t, rerr, driver.ErrDestinationDeleted, "round %d: destination created before the delete must be deleted with the tenant", round)
				} else {
					assert.ErrorIs(t, err, driver.ErrTenantNotFound)
				}
			}
			destinations, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenant.ID})
			require.NoError(t, err)
			assert.Empty(t, destinations, "round %d", round)
		}
	})
}
