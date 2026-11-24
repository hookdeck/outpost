package drivertest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// RunExcludedTypesTests checks that MatchEvent skips destinations of the
// excluded type and keeps matching the others. newStore(true) must exclude
// models.DestinationTypeMCP; newStore(false) must not.
func RunExcludedTypesTests(t *testing.T, newStore func(t *testing.T, excludeMCP bool) driver.TenantStore) {
	t.Helper()

	setup := func(t *testing.T, excludeMCP bool) (driver.TenantStore, models.Event, models.Destination, models.Destination) {
		ctx := context.Background()
		store := newStore(t, excludeMCP)
		tenant := models.Tenant{ID: idgen.String(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		webhook := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)
		mcp := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithType(models.DestinationTypeMCP),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)
		require.NoError(t, store.CreateDestination(ctx, webhook))
		require.NoError(t, store.CreateDestination(ctx, mcp))
		event := models.Event{
			ID:       idgen.Event(),
			Topic:    "user.created",
			Time:     time.Now(),
			TenantID: tenant.ID,
			Metadata: map[string]string{},
			Data:     json.RawMessage(`{}`),
		}
		return store, event, webhook, mcp
	}

	t.Run("excluded type is not matched", func(t *testing.T) {
		store, event, webhook, _ := setup(t, true)
		matched, err := store.MatchEvent(context.Background(), event, true)
		require.NoError(t, err)
		require.Equal(t, []string{webhook.ID}, matched)
	})

	t.Run("type is matched when not excluded", func(t *testing.T) {
		store, event, webhook, mcp := setup(t, false)
		matched, err := store.MatchEvent(context.Background(), event, true)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{webhook.ID, mcp.ID}, matched)
	})
}
