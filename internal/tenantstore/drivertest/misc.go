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

func testMisc(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("MaxDestinationsPerTenant", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		maxDestinations := 2
		store, err := h.MakeDriverWithMaxDest(ctx, maxDestinations)
		require.NoError(t, err)

		tenant := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		require.NoError(t, store.UpsertTenant(ctx, tenant))

		// Should be able to create up to maxDestinations
		for i := 0; i < maxDestinations; i++ {
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenant.ID),
			)
			err := store.CreateDestination(ctx, destination)
			require.NoError(t, err, "Should be able to create destination %d", i+1)
		}

		// Should fail when trying to create one more
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
		)
		err = store.CreateDestination(ctx, destination)
		require.Error(t, err)
		require.ErrorIs(t, err, driver.ErrMaxDestinationsPerTenantReached)

		// Should be able to create after deleting one
		destinations, err := store.ListDestinationByTenant(ctx, tenant.ID)
		require.NoError(t, err)
		require.NoError(t, store.DeleteDestination(ctx, tenant.ID, destinations[0].ID))

		destination = testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
		)
		err = store.CreateDestination(ctx, destination)
		require.NoError(t, err, "Should be able to create destination after deleting one")
	})

	t.Run("DeploymentIsolation", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store1, store2, err := h.MakeIsolatedDrivers(ctx)
		require.NoError(t, err)

		// Use same tenant ID and destination ID for both
		tenantID := idgen.String()
		destinationID := idgen.Destination()

		tenant := models.Tenant{
			ID:        tenantID,
			CreatedAt: time.Now(),
		}
		require.NoError(t, store1.UpsertTenant(ctx, tenant))
		require.NoError(t, store2.UpsertTenant(ctx, tenant))

		// Create destination with different config in each
		destination1 := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(destinationID),
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithConfig(map[string]string{"deployment": "dp_001"}),
		)
		destination2 := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(destinationID),
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithConfig(map[string]string{"deployment": "dp_002"}),
		)

		require.NoError(t, store1.CreateDestination(ctx, destination1))
		require.NoError(t, store2.CreateDestination(ctx, destination2))

		// Verify each store sees its own data
		retrieved1, err := store1.RetrieveDestination(ctx, tenantID, destinationID)
		require.NoError(t, err)
		assert.Equal(t, "dp_001", retrieved1.Config["deployment"])

		retrieved2, err := store2.RetrieveDestination(ctx, tenantID, destinationID)
		require.NoError(t, err)
		assert.Equal(t, "dp_002", retrieved2.Config["deployment"])

		// Verify list operations are isolated
		list1, err := store1.ListDestinationByTenant(ctx, tenantID)
		require.NoError(t, err)
		require.Len(t, list1, 1)
		assert.Equal(t, "dp_001", list1[0].Config["deployment"])

		list2, err := store2.ListDestinationByTenant(ctx, tenantID)
		require.NoError(t, err)
		require.Len(t, list2, 1)
		assert.Equal(t, "dp_002", list2[0].Config["deployment"])

		// Verify deleting from one doesn't affect the other
		require.NoError(t, store1.DeleteDestination(ctx, tenantID, destinationID))

		_, err = store1.RetrieveDestination(ctx, tenantID, destinationID)
		require.ErrorIs(t, err, driver.ErrDestinationDeleted)

		retrieved2Again, err := store2.RetrieveDestination(ctx, tenantID, destinationID)
		require.NoError(t, err)
		assert.Equal(t, "dp_002", retrieved2Again.Config["deployment"])
	})
}
