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
				Data:     json.RawMessage(`{}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			require.Len(t, matched, 3)
			for _, id := range matched {
				require.Contains(t, []string{data.destinations[0].ID, data.destinations[1].ID, data.destinations[4].ID}, id)
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
				Data:          json.RawMessage(`{}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
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
				Data:          json.RawMessage(`{}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
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
				Data:          json.RawMessage(`{}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
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

			destinations, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: data.tenant.ID})
			require.NoError(t, err)
			assert.Len(t, destinations, 5)

			// Match user.created (now 4 destinations match)
			event := models.Event{
				ID:       idgen.Event(),
				Topic:    "user.created",
				Time:     time.Now(),
				TenantID: data.tenant.ID,
				Metadata: map[string]string{},
				Data:     json.RawMessage(`{}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			require.Len(t, matched, 4)

			// Match user.updated (now only 2: wildcard + destinations[4])
			event = models.Event{
				ID:       idgen.Event(),
				Topic:    "user.updated",
				Time:     time.Now(),
				TenantID: data.tenant.ID,
				Metadata: map[string]string{},
				Data:     json.RawMessage(`{}`),
			}
			matched, err = matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			require.Len(t, matched, 2)
			for _, id := range matched {
				require.Contains(t, []string{data.destinations[0].ID, data.destinations[4].ID}, id)
			}
		})
	})

	t.Run("MatchByWildcardTopic", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		tenant := models.Tenant{ID: idgen.String()}
		require.NoError(t, store.UpsertTenant(ctx, tenant))

		destUserFamily := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_user_family"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"user.*"}),
		)
		destCreatedFamily := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_created_family"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*.created"}),
		)
		destOrderCompletedFamily := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_order_completed_family"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"order.*.completed"}),
		)
		destExact := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_exact"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		)

		require.NoError(t, store.CreateDestination(ctx, destUserFamily))
		require.NoError(t, store.CreateDestination(ctx, destCreatedFamily))
		require.NoError(t, store.CreateDestination(ctx, destOrderCompletedFamily))
		require.NoError(t, store.CreateDestination(ctx, destExact))

		t.Run("matches prefix and suffix wildcard subscriptions", func(t *testing.T) {
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(tenant.ID),
				testutil.EventFactory.WithTopic("user.created"),
			)
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"dest_user_family", "dest_created_family", "dest_exact"}, matched)
		})

		t.Run("ignores wildcard subscriptions when disabled", func(t *testing.T) {
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(tenant.ID),
				testutil.EventFactory.WithTopic("user.created"),
			)
			matched, err := matchedIDs(store.MatchEvent(ctx, event, false))
			require.NoError(t, err)
			assert.Equal(t, []string{"dest_exact"}, matched)
		})

		t.Run("matches separator agnostic middle wildcard subscription", func(t *testing.T) {
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(tenant.ID),
				testutil.EventFactory.WithTopic("order.payment.completed"),
			)
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"dest_order_completed_family"}, matched)
		})

		t.Run("does not overmatch unrelated topic", func(t *testing.T) {
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(tenant.ID),
				testutil.EventFactory.WithTopic("order.payment.failed"),
			)
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			assert.Empty(t, matched)
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
				Data:     json.RawMessage(`{"type":"order.created"}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			assert.Len(t, matched, 2)
			assert.Contains(t, matched, "dest_no_filter")
			assert.Contains(t, matched, "dest_filter_order_created")
		})

		t.Run("event with nested data matches nested filter", func(t *testing.T) {
			event := models.Event{
				ID:       idgen.Event(),
				TenantID: tenant.ID,
				Topic:    "order",
				Time:     time.Now(),
				Metadata: map[string]string{},
				Data:     json.RawMessage(`{"type":"order.created","customer":{"id":"cust_123","tier":"premium"}}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			assert.Len(t, matched, 3)
			assert.Contains(t, matched, "dest_no_filter")
			assert.Contains(t, matched, "dest_filter_order_created")
			assert.Contains(t, matched, "dest_filter_premium")
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
				Data:     json.RawMessage(`{"type":"order.created"}`),
			}
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			for _, id := range matched {
				assert.NotEqual(t, "dest_topic_and_filter", id)
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
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			require.Len(t, matched, 2)
			for _, id := range matched {
				require.Contains(t, []string{data.destinations[0].ID, data.destinations[3].ID}, id)
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
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
			require.NoError(t, err)
			require.Len(t, matched, 1)
			require.Equal(t, data.destinations[3].ID, matched[0])
		})

		t.Run("should match after re-enabled destination", func(t *testing.T) {
			destination := data.destinations[0]
			destination.DisabledAt = nil
			require.NoError(t, store.UpsertDestination(ctx, destination))

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithTenantID(data.tenant.ID),
				testutil.EventFactory.WithTopic("user.deleted"),
			)
			matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
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
		matched, err := matchedIDs(store.MatchEvent(ctx, event, true))
		require.NoError(t, err)
		require.Len(t, matched, 2)
		for _, id := range matched {
			require.Contains(t, []string{data.destinations[1].ID, data.destinations[4].ID}, id)
		}
	})
}

// matchedIDs converts a MatchEvent result to its destination IDs.
func matchedIDs(matched []driver.MatchedDestination, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(matched))
	for i, m := range matched {
		ids[i] = m.ID
	}
	return ids, nil
}

func testMatchExpiryAndExactTopics(t *testing.T, newHarness HarnessMaker) {
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
	event := func(tenantID, topic string) models.Event {
		return testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithTopic(topic),
			testutil.EventFactory.WithData(json.RawMessage(`{"region":"eu","amount":5}`)),
		)
	}

	t.Run("ReturnsTypes", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		webhook := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		)
		mcp := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithType("mcp"),
			testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		)
		require.NoError(t, store.CreateDestination(ctx, webhook))
		require.NoError(t, store.CreateDestination(ctx, mcp))

		matched, err := store.MatchEvent(ctx, event(tenantID, "user.created"), true)
		require.NoError(t, err)
		assert.ElementsMatch(t, []driver.MatchedDestination{
			{ID: webhook.ID, Type: "webhook"},
			{ID: mcp.ID, Type: "mcp"},
		}, matched)
	})

	t.Run("ExpiredAndMatch", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		expired := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithExpiresAt(time.Now().Add(-time.Millisecond)),
		)
		live := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenantID),
			testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)),
		)
		forever := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenantID))
		for _, d := range []models.Destination{expired, live, forever} {
			require.NoError(t, store.CreateDestination(ctx, d))
		}

		matched, err := matchedIDs(store.MatchEvent(ctx, event(tenantID, "user.created"), true))
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{live.ID, forever.ID}, matched)

		t.Run("matches again once refreshed", func(t *testing.T) {
			later := time.Now().Add(time.Hour)
			expired.ExpiresAt = &later
			_, err := store.UpdateDestinationIfLive(ctx, expired, expired.CreatedAt)
			require.NoError(t, err)

			matched, err := matchedIDs(store.MatchEvent(ctx, event(tenantID, "user.created"), true))
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{expired.ID, live.ID, forever.ID}, matched)
		})

		t.Run("model agrees", func(t *testing.T) {
			retrieved, err := store.RetrieveDestination(ctx, tenantID, live.ID)
			require.NoError(t, err)
			assert.True(t, retrieved.MatchEvent(event(tenantID, "user.created"), true))
			past := time.Now().Add(-time.Second)
			retrieved.ExpiresAt = &past
			assert.False(t, retrieved.MatchEvent(event(tenantID, "user.created"), true))
		})
	})

	t.Run("ExactTopicTypes", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		mcp := func(topics ...string) models.Destination {
			d := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenantID),
				testutil.DestinationFactory.WithType("mcp"),
				testutil.DestinationFactory.WithTopics(topics),
			)
			require.NoError(t, store.CreateDestination(ctx, d))
			return d
		}
		exact := mcp("user.created")
		// Neither "*" nor a pattern matches anything for an exact-topic type.
		mcp("*")
		mcp("user.*")
		webhook := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenantID))
		require.NoError(t, store.CreateDestination(ctx, webhook))

		for _, tc := range []struct {
			topic string
			want  []string
		}{
			{"user.created", []string{exact.ID, webhook.ID}},
			{"user.updated", []string{webhook.ID}},
			{"", []string{webhook.ID}},
			{"*", []string{webhook.ID}},
		} {
			t.Run("topic "+tc.topic, func(t *testing.T) {
				matched, err := matchedIDs(store.MatchEvent(ctx, event(tenantID, tc.topic), true))
				require.NoError(t, err)
				assert.ElementsMatch(t, tc.want, matched)
			})
		}
	})

	t.Run("SharedFilterInput", func(t *testing.T) {
		ctx, store := newStore(t)
		tenantID := idgen.String()
		var want []string
		for i := range 20 {
			region := "eu"
			if i%2 == 1 {
				region = "us"
			}
			d := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenantID),
				testutil.DestinationFactory.WithType("mcp"),
				testutil.DestinationFactory.WithTopics([]string{"user.created"}),
				testutil.DestinationFactory.WithFilter(models.Filter{
					"data": map[string]any{"region": region, "amount": map[string]any{"$gte": float64(i % 7)}},
				}),
			)
			require.NoError(t, store.CreateDestination(ctx, d))
			if region == "eu" && i%7 <= 5 {
				want = append(want, d.ID)
			}
		}
		matched, err := matchedIDs(store.MatchEvent(ctx, event(tenantID, "user.created"), true))
		require.NoError(t, err)
		assert.ElementsMatch(t, want, matched)
	})
}
