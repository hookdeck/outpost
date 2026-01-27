package drivertest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfListTenantNotSupported probes the store and skips the test if ListTenant is not supported.
func skipIfListTenantNotSupported(t *testing.T, store driver.TenantStore, ctx context.Context) {
	t.Helper()
	_, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 1})
	if errors.Is(err, driver.ErrListTenantNotSupported) {
		t.Skip("ListTenant not supported by this driver")
	}
}

// multiDestinationData holds shared test data for multi-destination tests.
type multiDestinationData struct {
	tenant       models.Tenant
	destinations []models.Destination
}

func setupMultiDestination(t *testing.T, ctx context.Context, store driver.TenantStore) multiDestinationData {
	t.Helper()
	data := multiDestinationData{
		tenant: models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		},
		destinations: make([]models.Destination, 5),
	}
	require.NoError(t, store.UpsertTenant(ctx, data.tenant))

	destinationTopicList := [][]string{
		{"*"},
		{"user.created"},
		{"user.updated"},
		{"user.deleted"},
		{"user.created", "user.updated"},
	}
	baseTime := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		data.destinations[i] = testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(idgen.Destination()),
			testutil.DestinationFactory.WithTenantID(data.tenant.ID),
			testutil.DestinationFactory.WithTopics(destinationTopicList[i]),
			testutil.DestinationFactory.WithCreatedAt(baseTime.Add(time.Duration(i)*time.Second)),
		)
		require.NoError(t, store.UpsertDestination(ctx, data.destinations[i]))
	}

	// Insert & Delete destination to ensure cleanup
	toBeDeletedID := idgen.Destination()
	require.NoError(t, store.UpsertDestination(ctx,
		testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(toBeDeletedID),
			testutil.DestinationFactory.WithTenantID(data.tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)))
	require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, toBeDeletedID))

	return data
}

func testList(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("MultiDestinationRetrieveTenantDestinationsCount", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		tenant, err := store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, 5, tenant.DestinationsCount)
	})

	t.Run("MultiDestinationRetrieveTenantTopics", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		// destinations[0] has topics ["*"]
		tenant, err := store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"*"}, tenant.Topics)

		// After deleting wildcard destination, topics should aggregate remaining
		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[0].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[1].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[2].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[3].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[4].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{}, tenant.Topics)
	})

	t.Run("MultiDestinationListDestinationByTenant", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Len(t, destinations, 5)
		for index, destination := range destinations {
			require.Equal(t, data.destinations[index].ID, destination.ID)
		}
	})

	t.Run("MultiDestinationListDestinationWithOpts", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		t.Run("filter by type: webhook", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type: []string{"webhook"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 5)
		})

		t.Run("filter by type: rabbitmq", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type: []string{"rabbitmq"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 0)
		})

		t.Run("filter by type: webhook,rabbitmq", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type: []string{"webhook", "rabbitmq"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 5)
		})

		t.Run("filter by topic: user.created", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Topics: []string{"user.created"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 3)
		})

		t.Run("filter by topic: user.created,user.updated", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Topics: []string{"user.created", "user.updated"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 2)
		})

		t.Run("filter by type: rabbitmq, topic: user.created,user.updated", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type:   []string{"rabbitmq"},
				Topics: []string{"user.created", "user.updated"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 0)
		})

		t.Run("filter by topic: *", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Topics: []string{"*"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 1)
		})
	})

	t.Run("ListTenantEnrichment", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))
		skipIfListTenantNotSupported(t, store, ctx)

		// Create 25 tenants
		tenants := make([]models.Tenant, 25)
		baseTime := time.Now()
		for i := range tenants {
			tenants[i] = testutil.TenantFactory.Any(
				testutil.TenantFactory.WithCreatedAt(baseTime.Add(time.Duration(i)*time.Second)),
				testutil.TenantFactory.WithUpdatedAt(baseTime.Add(time.Duration(i)*time.Second)),
			)
			require.NoError(t, store.UpsertTenant(ctx, tenants[i]))
		}
		tenantWithDests := tenants[24]
		for i := range 2 {
			dest := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithID(fmt.Sprintf("dest_suite_%d", i)),
				testutil.DestinationFactory.WithTenantID(tenantWithDests.ID),
			)
			require.NoError(t, store.UpsertDestination(ctx, dest))
		}

		t.Run("returns total count independent of pagination", func(t *testing.T) {
			resp1, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 2})
			require.NoError(t, err)
			assert.Equal(t, 25, resp1.Count)
			assert.Len(t, resp1.Models, 2)

			var nextCursor string
			if resp1.Pagination.Next != nil {
				nextCursor = *resp1.Pagination.Next
			}
			resp2, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 2, Next: nextCursor})
			require.NoError(t, err)
			assert.Equal(t, 25, resp2.Count)
		})

		t.Run("does not include destinations in tenant list", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 100})
			require.NoError(t, err)
			assert.Equal(t, 25, resp.Count)
			for _, tenant := range resp.Models {
				assert.NotContains(t, tenant.ID, "dest_")
			}
		})

		t.Run("returns destinations_count and topics", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 100})
			require.NoError(t, err)

			var found *models.Tenant
			for i := range resp.Models {
				if resp.Models[i].ID == tenantWithDests.ID {
					found = &resp.Models[i]
					break
				}
			}
			require.NotNil(t, found)
			assert.Equal(t, 2, found.DestinationsCount)
			assert.NotNil(t, found.Topics)

			var without *models.Tenant
			for i := range resp.Models {
				if resp.Models[i].ID != tenantWithDests.ID {
					without = &resp.Models[i]
					break
				}
			}
			require.NotNil(t, without)
			assert.Equal(t, 0, without.DestinationsCount)
			assert.Empty(t, without.Topics)
		})
	})

	t.Run("ListTenantExcludesDeleted", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))
		skipIfListTenantNotSupported(t, store, ctx)

		// Create initial tenants
		for i := 0; i < 5; i++ {
			require.NoError(t, store.UpsertTenant(ctx, testutil.TenantFactory.Any()))
		}

		t.Run("deleted tenant not returned", func(t *testing.T) {
			initialResp, err := store.ListTenant(ctx, driver.ListTenantRequest{})
			require.NoError(t, err)
			initialCount := initialResp.Count

			tenant1 := testutil.TenantFactory.Any()
			tenant2 := testutil.TenantFactory.Any()
			require.NoError(t, store.UpsertTenant(ctx, tenant1))
			require.NoError(t, store.UpsertTenant(ctx, tenant2))

			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{})
			require.NoError(t, err)
			assert.Equal(t, initialCount+2, resp.Count)

			require.NoError(t, store.DeleteTenant(ctx, tenant1.ID))

			resp, err = store.ListTenant(ctx, driver.ListTenantRequest{})
			require.NoError(t, err)
			assert.Equal(t, initialCount+1, resp.Count)

			for _, tenant := range resp.Models {
				assert.NotEqual(t, tenant1.ID, tenant.ID)
			}

			_ = store.DeleteTenant(ctx, tenant2.ID)
		})
	})

	t.Run("ListTenantInputValidation", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))
		skipIfListTenantNotSupported(t, store, ctx)

		// Create 25 tenants for pagination tests
		tenants := make([]models.Tenant, 25)
		baseTime := time.Now()
		for i := range tenants {
			tenants[i] = testutil.TenantFactory.Any(
				testutil.TenantFactory.WithCreatedAt(baseTime.Add(time.Duration(i)*time.Second)),
				testutil.TenantFactory.WithUpdatedAt(baseTime.Add(time.Duration(i)*time.Second)),
			)
			require.NoError(t, store.UpsertTenant(ctx, tenants[i]))
		}

		t.Run("invalid dir returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{Dir: "invalid"})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidOrder)
		})

		t.Run("conflicting cursors returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Next: "somecursor",
				Prev: "anothercursor",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrConflictingCursors)
		})

		t.Run("invalid next cursor returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Next: "not-valid-base62!!!",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidCursor)
		})

		t.Run("invalid prev cursor returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Prev: "not-valid-base62!!!",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidCursor)
		})

		t.Run("malformed cursor format returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Next: "abc123",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidCursor)
		})

		t.Run("limit zero uses default", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 0})
			require.NoError(t, err)
			assert.Equal(t, 20, len(resp.Models))
			assert.Equal(t, 25, resp.Count)
		})

		t.Run("limit negative uses default", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: -5})
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})

		t.Run("limit exceeding max is capped", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 1000})
			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, 25, len(resp.Models))
			assert.Equal(t, 25, resp.Count)
		})

		t.Run("empty dir uses default desc", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Dir: ""})
			require.NoError(t, err)
			require.Len(t, resp.Models, 20)
			assert.Equal(t, tenants[24].ID, resp.Models[0].ID)
			assert.Equal(t, tenants[23].ID, resp.Models[1].ID)
		})
	})

	t.Run("ListTenantKeysetPagination", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))
		skipIfListTenantNotSupported(t, store, ctx)

		t.Run("add during traversal does not cause duplicate", func(t *testing.T) {
			prefix := fmt.Sprintf("add_edge_%d_", time.Now().UnixNano())
			tenantIDs := make([]string, 15)
			baseTime := time.Now().Add(20 * time.Hour)
			for i := 0; i < 15; i++ {
				tenantIDs[i] = fmt.Sprintf("%s%02d", prefix, i)
				tenant := models.Tenant{
					ID:        tenantIDs[i],
					CreatedAt: baseTime.Add(time.Duration(i) * time.Second),
					UpdatedAt: baseTime,
				}
				require.NoError(t, store.UpsertTenant(ctx, tenant))
			}

			resp1, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 5})
			require.NoError(t, err)
			require.Len(t, resp1.Models, 5)

			// Add new tenant with newest timestamp
			newTenantID := prefix + "NEW"
			newTenant := models.Tenant{
				ID:        newTenantID,
				CreatedAt: baseTime.Add(time.Hour),
				UpdatedAt: baseTime,
			}
			require.NoError(t, store.UpsertTenant(ctx, newTenant))
			tenantIDs = append(tenantIDs, newTenantID)

			var nextCursor string
			if resp1.Pagination.Next != nil {
				nextCursor = *resp1.Pagination.Next
			}
			resp2, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Limit: 5,
				Next:  nextCursor,
			})
			require.NoError(t, err)
			require.NotEmpty(t, resp2.Models)

			page1IDs := make(map[string]bool)
			for _, tenant := range resp1.Models {
				page1IDs[tenant.ID] = true
			}
			for _, tenant := range resp2.Models {
				assert.False(t, page1IDs[tenant.ID],
					"keyset pagination: no duplicates, but found %s", tenant.ID)
			}

			// Cleanup
			for _, id := range tenantIDs {
				_ = store.DeleteTenant(ctx, id)
			}
		})
	})
}
