package drivertest

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMatch(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("MatchByTopic", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		t.Run("match by topic", func(t *testing.T) {
			event := models.Event{
				ID:       idgen.Event(),
				Topic:    "user.created",
				Time:     time.Now(),
				TenantID: data.tenant.ID,
				Metadata: map[string]string{},
				Data:     map[string]interface{}{},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 3)
			for _, summary := range matched {
				require.Contains(t, []string{data.destinations[0].ID, data.destinations[1].ID, data.destinations[4].ID}, summary.ID)
			}
		})

		t.Run("ignores destination_id and matches by topic only", func(t *testing.T) {
			event := models.Event{
				ID:            idgen.Event(),
				Topic:         "user.created",
				Time:          time.Now(),
				TenantID:      data.tenant.ID,
				DestinationID: data.destinations[1].ID,
				Metadata:      map[string]string{},
				Data:          map[string]interface{}{},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 3)
		})

		t.Run("ignores non-existent destination_id", func(t *testing.T) {
			event := models.Event{
				ID:            idgen.Event(),
				Topic:         "user.created",
				Time:          time.Now(),
				TenantID:      data.tenant.ID,
				DestinationID: "not-found",
				Metadata:      map[string]string{},
				Data:          map[string]interface{}{},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 3)
		})

		t.Run("ignores destination_id with mismatched topic", func(t *testing.T) {
			event := models.Event{
				ID:            idgen.Event(),
				Topic:         "user.created",
				Time:          time.Now(),
				TenantID:      data.tenant.ID,
				DestinationID: data.destinations[3].ID, // user.deleted
				Metadata:      map[string]string{},
				Data:          map[string]interface{}{},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 3)
		})

		t.Run("match after destination is updated", func(t *testing.T) {
			updatedDestination := data.destinations[2] // user.updated
			updatedDestination.Topics = []string{"user.created"}
			require.NoError(t, store.UpsertDestination(ctx, updatedDestination))

			actual, err := store.RetrieveDestination(ctx, updatedDestination.TenantID, updatedDestination.ID)
			require.NoError(t, err)
			assert.Equal(t, updatedDestination.Topics, actual.Topics)

			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID)
			require.NoError(t, err)
			assert.Len(t, destinations, 5)

			// Match user.created (now 4 destinations match)
			event := models.Event{
				ID:       idgen.Event(),
				Topic:    "user.created",
				Time:     time.Now(),
				TenantID: data.tenant.ID,
				Metadata: map[string]string{},
				Data:     map[string]interface{}{},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 4)

			// Match user.updated (now only 2: wildcard + destinations[4])
			event = models.Event{
				ID:       idgen.Event(),
				Topic:    "user.updated",
				Time:     time.Now(),
				TenantID: data.tenant.ID,
				Metadata: map[string]string{},
				Data:     map[string]interface{}{},
			}
			matched, err = store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 2)
			for _, summary := range matched {
				require.Contains(t, []string{data.destinations[0].ID, data.destinations[4].ID}, summary.ID)
			}
		})
	})

	t.Run("MatchEventWithFilter", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		tenant := models.Tenant{ID: idgen.String()}
		require.NoError(t, store.UpsertTenant(ctx, tenant))

		destNoFilter := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_no_filter"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)
		destFilterOrderCreated := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_filter_order_created"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
			testutil.DestinationFactory.WithFilter(models.Filter{
				"data": map[string]any{"type": "order.created"},
			}),
		)
		destFilterOrderUpdated := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_filter_order_updated"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
			testutil.DestinationFactory.WithFilter(models.Filter{
				"data": map[string]any{"type": "order.updated"},
			}),
		)
		destFilterPremiumCustomer := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_filter_premium"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
			testutil.DestinationFactory.WithFilter(models.Filter{
				"data": map[string]any{
					"customer": map[string]any{"tier": "premium"},
				},
			}),
		)

		require.NoError(t, store.CreateDestination(ctx, destNoFilter))
		require.NoError(t, store.CreateDestination(ctx, destFilterOrderCreated))
		require.NoError(t, store.CreateDestination(ctx, destFilterOrderUpdated))
		require.NoError(t, store.CreateDestination(ctx, destFilterPremiumCustomer))

		t.Run("event matches only destinations with matching filter", func(t *testing.T) {
			event := models.Event{
				ID:       idgen.Event(),
				TenantID: tenant.ID,
				Topic:    "order",
				Time:     time.Now(),
				Metadata: map[string]string{},
				Data:     map[string]interface{}{"type": "order.created"},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			assert.Len(t, matched, 2)
			ids := []string{}
			for _, dest := range matched {
				ids = append(ids, dest.ID)
			}
			assert.Contains(t, ids, "dest_no_filter")
			assert.Contains(t, ids, "dest_filter_order_created")
		})

		t.Run("event with nested data matches nested filter", func(t *testing.T) {
			event := models.Event{
				ID:       idgen.Event(),
				TenantID: tenant.ID,
				Topic:    "order",
				Time:     time.Now(),
				Metadata: map[string]string{},
				Data: map[string]interface{}{
					"type": "order.created",
					"customer": map[string]interface{}{
						"id":   "cust_123",
						"tier": "premium",
					},
				},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			assert.Len(t, matched, 3)
			ids := []string{}
			for _, dest := range matched {
				ids = append(ids, dest.ID)
			}
			assert.Contains(t, ids, "dest_no_filter")
			assert.Contains(t, ids, "dest_filter_order_created")
			assert.Contains(t, ids, "dest_filter_premium")
		})

		t.Run("topic filter takes precedence before content filter", func(t *testing.T) {
			destTopicAndFilter := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithID("dest_topic_and_filter"),
				testutil.DestinationFactory.WithTenantID(tenant.ID),
				testutil.DestinationFactory.WithTopics([]string{"user.created"}),
				testutil.DestinationFactory.WithFilter(models.Filter{
					"data": map[string]any{"type": "order.created"},
				}),
			)
			require.NoError(t, store.CreateDestination(ctx, destTopicAndFilter))

			event := models.Event{
				ID:       idgen.Event(),
				TenantID: tenant.ID,
				Topic:    "order",
				Time:     time.Now(),
				Metadata: map[string]string{},
				Data:     map[string]interface{}{"type": "order.created"},
			}
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			for _, dest := range matched {
				assert.NotEqual(t, "dest_topic_and_filter", dest.ID)
			}
		})
	})

	t.Run("DisableAndMatch", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		t.Run("initial match user.deleted", func(t *testing.T) {
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(data.tenant.ID),
				testutil.EventFactory.WithTopic("user.deleted"),
			)
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 2)
			for _, summary := range matched {
				require.Contains(t, []string{data.destinations[0].ID, data.destinations[3].ID}, summary.ID)
			}
		})

		t.Run("should not match disabled destination", func(t *testing.T) {
			destination := data.destinations[0]
			now := time.Now()
			destination.DisabledAt = &now
			require.NoError(t, store.UpsertDestination(ctx, destination))

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(data.tenant.ID),
				testutil.EventFactory.WithTopic("user.deleted"),
			)
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 1)
			require.Equal(t, data.destinations[3].ID, matched[0].ID)
		})

		t.Run("should match after re-enabled destination", func(t *testing.T) {
			destination := data.destinations[0]
			destination.DisabledAt = nil
			require.NoError(t, store.UpsertDestination(ctx, destination))

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(data.tenant.ID),
				testutil.EventFactory.WithTopic("user.deleted"),
			)
			matched, err := store.MatchEvent(ctx, event)
			require.NoError(t, err)
			require.Len(t, matched, 2)
		})
	})

	t.Run("DeleteAndMatch", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[0].ID))

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(data.tenant.ID),
			testutil.EventFactory.WithTopic("user.created"),
		)
		matched, err := store.MatchEvent(ctx, event)
		require.NoError(t, err)
		require.Len(t, matched, 2)
		for _, summary := range matched {
			require.Contains(t, []string{data.destinations[1].ID, data.destinations[4].ID}, summary.ID)
		}
	})
}
