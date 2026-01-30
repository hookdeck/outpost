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

func testCRUD(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("InitIdempotency", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			err := store.Init(ctx)
			require.NoError(t, err, "Init call %d should not fail", i+1)
		}
	})

	t.Run("TenantCRUD", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		now := time.Now()
		input := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: now,
			UpdatedAt: now,
		}

		t.Run("gets empty", func(t *testing.T) {
			actual, err := store.RetrieveTenant(ctx, input.ID)
			assert.Nil(t, actual)
			assert.NoError(t, err)
		})

		t.Run("sets", func(t *testing.T) {
			err := store.UpsertTenant(ctx, input)
			require.NoError(t, err)

			retrieved, err := store.RetrieveTenant(ctx, input.ID)
			require.NoError(t, err)
			assert.Equal(t, input.ID, retrieved.ID)
			assertEqualTime(t, input.CreatedAt, retrieved.CreatedAt, "CreatedAt")
		})

		t.Run("gets", func(t *testing.T) {
			actual, err := store.RetrieveTenant(ctx, input.ID)
			require.NoError(t, err)
			assert.Equal(t, input.ID, actual.ID)
			assertEqualTime(t, input.CreatedAt, actual.CreatedAt, "CreatedAt")
		})

		t.Run("overrides", func(t *testing.T) {
			input.CreatedAt = time.Now()
			err := store.UpsertTenant(ctx, input)
			require.NoError(t, err)

			actual, err := store.RetrieveTenant(ctx, input.ID)
			require.NoError(t, err)
			assert.Equal(t, input.ID, actual.ID)
			assertEqualTime(t, input.CreatedAt, actual.CreatedAt, "CreatedAt")
		})

		t.Run("clears", func(t *testing.T) {
			require.NoError(t, store.DeleteTenant(ctx, input.ID))

			actual, err := store.RetrieveTenant(ctx, input.ID)
			assert.ErrorIs(t, err, driver.ErrTenantDeleted)
			assert.Nil(t, actual)
		})

		t.Run("deletes again", func(t *testing.T) {
			assert.NoError(t, store.DeleteTenant(ctx, input.ID))
		})

		t.Run("deletes non-existent", func(t *testing.T) {
			assert.ErrorIs(t, store.DeleteTenant(ctx, "non-existent-tenant"), driver.ErrTenantNotFound)
		})

		t.Run("creates & overrides deleted resource", func(t *testing.T) {
			require.NoError(t, store.UpsertTenant(ctx, input))

			actual, err := store.RetrieveTenant(ctx, input.ID)
			require.NoError(t, err)
			assert.Equal(t, input.ID, actual.ID)
			assertEqualTime(t, input.CreatedAt, actual.CreatedAt, "CreatedAt")
		})

		t.Run("upserts with metadata", func(t *testing.T) {
			input.Metadata = map[string]string{
				"environment": "production",
				"team":        "platform",
			}
			err := store.UpsertTenant(ctx, input)
			require.NoError(t, err)

			retrieved, err := store.RetrieveTenant(ctx, input.ID)
			require.NoError(t, err)
			assert.Equal(t, input.ID, retrieved.ID)
			assert.Equal(t, input.Metadata, retrieved.Metadata)
		})

		t.Run("updates metadata", func(t *testing.T) {
			input.Metadata = map[string]string{
				"environment": "staging",
				"team":        "engineering",
				"region":      "us-west-2",
			}
			err := store.UpsertTenant(ctx, input)
			require.NoError(t, err)

			retrieved, err := store.RetrieveTenant(ctx, input.ID)
			require.NoError(t, err)
			assert.Equal(t, input.Metadata, retrieved.Metadata)
		})

		t.Run("handles nil metadata", func(t *testing.T) {
			input.Metadata = nil
			err := store.UpsertTenant(ctx, input)
			require.NoError(t, err)

			retrieved, err := store.RetrieveTenant(ctx, input.ID)
			require.NoError(t, err)
			assert.Nil(t, retrieved.Metadata)
		})

		t.Run("sets updated_at on create", func(t *testing.T) {
			newTenant := testutil.TenantFactory.Any()
			err := store.UpsertTenant(ctx, newTenant)
			require.NoError(t, err)

			retrieved, err := store.RetrieveTenant(ctx, newTenant.ID)
			require.NoError(t, err)
			assertEqualTime(t, newTenant.UpdatedAt, retrieved.UpdatedAt, "UpdatedAt")
		})

		t.Run("updates updated_at on upsert", func(t *testing.T) {
			originalTime := time.Now().Add(-2 * time.Second).Truncate(time.Millisecond)
			updatedTime := originalTime.Add(1 * time.Second)

			original := testutil.TenantFactory.Any(
				testutil.TenantFactory.WithUpdatedAt(originalTime),
			)
			err := store.UpsertTenant(ctx, original)
			require.NoError(t, err)

			updated := original
			updated.UpdatedAt = updatedTime
			err = store.UpsertTenant(ctx, updated)
			require.NoError(t, err)

			retrieved, err := store.RetrieveTenant(ctx, updated.ID)
			require.NoError(t, err)
			assert.True(t, retrieved.UpdatedAt.After(originalTime) || retrieved.UpdatedAt.Equal(originalTime.Add(time.Second)))
		})
	})

	t.Run("DestinationCRUD", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		now := time.Now()
		input := models.Destination{
			ID:     idgen.Destination(),
			Type:   "rabbitmq",
			Topics: []string{"user.created", "user.updated"},
			Config: map[string]string{
				"server_url": "localhost:5672",
				"exchange":   "events",
			},
			Credentials: map[string]string{
				"username": "guest",
				"password": "guest",
			},
			DeliveryMetadata: map[string]string{
				"app-id": "test-app",
				"source": "outpost",
			},
			Metadata: map[string]string{
				"environment": "test",
				"team":        "platform",
			},
			CreatedAt:  now,
			UpdatedAt:  now,
			DisabledAt: nil,
			TenantID:   idgen.String(),
		}

		t.Run("gets empty", func(t *testing.T) {
			actual, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
			require.NoError(t, err)
			assert.Nil(t, actual)
		})

		t.Run("sets", func(t *testing.T) {
			err := store.CreateDestination(ctx, input)
			require.NoError(t, err)
		})

		t.Run("gets", func(t *testing.T) {
			actual, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
			require.NoError(t, err)
			assertEqualDestination(t, input, *actual)
		})

		t.Run("updates", func(t *testing.T) {
			input.Topics = []string{"*"}
			input.DeliveryMetadata = map[string]string{
				"app-id":  "updated-app",
				"version": "2.0",
			}
			input.Metadata = map[string]string{
				"environment": "staging",
			}
			err := store.UpsertDestination(ctx, input)
			require.NoError(t, err)

			actual, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
			require.NoError(t, err)
			assertEqualDestination(t, input, *actual)
		})

		t.Run("clears", func(t *testing.T) {
			err := store.DeleteDestination(ctx, input.TenantID, input.ID)
			require.NoError(t, err)

			actual, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
			assert.ErrorIs(t, err, driver.ErrDestinationDeleted)
			assert.Nil(t, actual)
		})

		t.Run("creates & overrides deleted resource", func(t *testing.T) {
			err := store.CreateDestination(ctx, input)
			require.NoError(t, err)

			actual, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
			require.NoError(t, err)
			assertEqualDestination(t, input, *actual)
		})

		t.Run("err when creates duplicate", func(t *testing.T) {
			assert.ErrorIs(t, store.CreateDestination(ctx, input), driver.ErrDuplicateDestination)
			require.NoError(t, store.DeleteDestination(ctx, input.TenantID, input.ID))
		})

		t.Run("handles nil delivery_metadata and metadata", func(t *testing.T) {
			inputWithNilFields := testutil.DestinationFactory.Any()
			err := store.CreateDestination(ctx, inputWithNilFields)
			require.NoError(t, err)

			actual, err := store.RetrieveDestination(ctx, inputWithNilFields.TenantID, inputWithNilFields.ID)
			require.NoError(t, err)
			assert.Nil(t, actual.DeliveryMetadata)
			assert.Nil(t, actual.Metadata)
			require.NoError(t, store.DeleteDestination(ctx, inputWithNilFields.TenantID, inputWithNilFields.ID))
		})

		t.Run("sets updated_at on create", func(t *testing.T) {
			newDest := testutil.DestinationFactory.Any()
			err := store.CreateDestination(ctx, newDest)
			require.NoError(t, err)

			retrieved, err := store.RetrieveDestination(ctx, newDest.TenantID, newDest.ID)
			require.NoError(t, err)
			assertEqualTime(t, newDest.UpdatedAt, retrieved.UpdatedAt, "UpdatedAt")
			require.NoError(t, store.DeleteDestination(ctx, newDest.TenantID, newDest.ID))
		})

		t.Run("updates updated_at on upsert", func(t *testing.T) {
			originalTime := time.Now().Add(-2 * time.Second).Truncate(time.Millisecond)
			updatedTime := originalTime.Add(1 * time.Second)

			original := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithUpdatedAt(originalTime),
			)
			err := store.CreateDestination(ctx, original)
			require.NoError(t, err)

			updated := original
			updated.UpdatedAt = updatedTime
			updated.Topics = []string{"updated.topic"}
			err = store.UpsertDestination(ctx, updated)
			require.NoError(t, err)

			retrieved, err := store.RetrieveDestination(ctx, updated.TenantID, updated.ID)
			require.NoError(t, err)
			assert.True(t, retrieved.UpdatedAt.After(originalTime) || retrieved.UpdatedAt.Equal(originalTime.Add(time.Second)))
			require.NoError(t, store.DeleteDestination(ctx, updated.TenantID, updated.ID))
		})
	})

	t.Run("ListDestinationEmpty", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		destinations, err := store.ListDestinationByTenant(ctx, idgen.String())
		require.NoError(t, err)
		assert.Empty(t, destinations)
	})

	t.Run("DeleteTenantAndAssociatedDestinations", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		tenant := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		require.NoError(t, store.UpsertTenant(ctx, tenant))

		destinationIDs := []string{idgen.Destination(), idgen.Destination(), idgen.Destination()}
		for _, id := range destinationIDs {
			require.NoError(t, store.UpsertDestination(ctx, testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithID(id),
				testutil.DestinationFactory.WithTenantID(tenant.ID),
			)))
		}

		require.NoError(t, store.DeleteTenant(ctx, tenant.ID))

		_, err = store.RetrieveTenant(ctx, tenant.ID)
		assert.ErrorIs(t, err, driver.ErrTenantDeleted)
		for _, id := range destinationIDs {
			_, err := store.RetrieveDestination(ctx, tenant.ID, id)
			assert.ErrorIs(t, err, driver.ErrDestinationDeleted)
		}
	})

	t.Run("DeleteDestination", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any()
		require.NoError(t, store.CreateDestination(ctx, destination))

		t.Run("no error when deleting existing", func(t *testing.T) {
			assert.NoError(t, store.DeleteDestination(ctx, destination.TenantID, destination.ID))
		})

		t.Run("no error when deleting already-deleted", func(t *testing.T) {
			assert.NoError(t, store.DeleteDestination(ctx, destination.TenantID, destination.ID))
		})

		t.Run("error when deleting non-existent", func(t *testing.T) {
			err := store.DeleteDestination(ctx, destination.TenantID, idgen.Destination())
			assert.ErrorIs(t, err, driver.ErrDestinationNotFound)
		})

		t.Run("returns ErrDestinationDeleted when retrieving deleted", func(t *testing.T) {
			dest, err := store.RetrieveDestination(ctx, destination.TenantID, destination.ID)
			assert.ErrorIs(t, err, driver.ErrDestinationDeleted)
			assert.Nil(t, dest)
		})

		t.Run("does not return deleted in list", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, destination.TenantID)
			assert.NoError(t, err)
			assert.Empty(t, destinations)
		})
	})

	t.Run("DestinationEnableDisable", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		input := testutil.DestinationFactory.Any()
		require.NoError(t, store.UpsertDestination(ctx, input))

		t.Run("should disable", func(t *testing.T) {
			now := time.Now()
			input.DisabledAt = &now
			require.NoError(t, store.UpsertDestination(ctx, input))

			actual, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
			require.NoError(t, err)
			assertEqualTimePtr(t, input.DisabledAt, actual.DisabledAt, "DisabledAt")
		})

		t.Run("should enable", func(t *testing.T) {
			input.DisabledAt = nil
			require.NoError(t, store.UpsertDestination(ctx, input))

			actual, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
			require.NoError(t, err)
			assertEqualTimePtr(t, input.DisabledAt, actual.DisabledAt, "DisabledAt")
		})
	})

	t.Run("FilterPersistence", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		tenant := models.Tenant{ID: idgen.String()}
		require.NoError(t, store.UpsertTenant(ctx, tenant))

		t.Run("stores and retrieves destination with filter", func(t *testing.T) {
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenant.ID),
				testutil.DestinationFactory.WithTopics([]string{"*"}),
				testutil.DestinationFactory.WithFilter(models.Filter{
					"data": map[string]any{"type": "order.created"},
				}),
			)
			require.NoError(t, store.CreateDestination(ctx, destination))

			retrieved, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
			require.NoError(t, err)
			assert.NotNil(t, retrieved.Filter)
			assert.Equal(t, "order.created", retrieved.Filter["data"].(map[string]any)["type"])
		})

		t.Run("stores destination with nil filter", func(t *testing.T) {
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenant.ID),
				testutil.DestinationFactory.WithTopics([]string{"*"}),
			)
			require.NoError(t, store.CreateDestination(ctx, destination))

			retrieved, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
			require.NoError(t, err)
			assert.Nil(t, retrieved.Filter)
		})

		t.Run("updates destination filter", func(t *testing.T) {
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenant.ID),
				testutil.DestinationFactory.WithTopics([]string{"*"}),
				testutil.DestinationFactory.WithFilter(models.Filter{
					"data": map[string]any{"type": "order.created"},
				}),
			)
			require.NoError(t, store.CreateDestination(ctx, destination))

			destination.Filter = models.Filter{
				"data": map[string]any{"type": "order.updated"},
			}
			require.NoError(t, store.UpsertDestination(ctx, destination))

			retrieved, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
			require.NoError(t, err)
			assert.Equal(t, "order.updated", retrieved.Filter["data"].(map[string]any)["type"])
		})

		t.Run("removes destination filter", func(t *testing.T) {
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithTenantID(tenant.ID),
				testutil.DestinationFactory.WithTopics([]string{"*"}),
				testutil.DestinationFactory.WithFilter(models.Filter{
					"data": map[string]any{"type": "order.created"},
				}),
			)
			require.NoError(t, store.CreateDestination(ctx, destination))

			destination.Filter = nil
			require.NoError(t, store.UpsertDestination(ctx, destination))

			retrieved, err := store.RetrieveDestination(ctx, tenant.ID, destination.ID)
			require.NoError(t, err)
			assert.Nil(t, retrieved.Filter)
		})
	})
}

// assertEqualTime compares two times by truncating to millisecond precision.
func assertEqualTime(t *testing.T, expected, actual time.Time, field string) {
	t.Helper()
	expectedTrunc := expected.Truncate(time.Millisecond)
	actualTrunc := actual.Truncate(time.Millisecond)
	assert.True(t, expectedTrunc.Equal(actualTrunc),
		"expected %s %v, got %v", field, expectedTrunc, actualTrunc)
}

// assertEqualTimePtr compares two optional times by truncating to millisecond precision.
func assertEqualTimePtr(t *testing.T, expected, actual *time.Time, field string) {
	t.Helper()
	if expected == nil {
		assert.Nil(t, actual, "%s should be nil", field)
		return
	}
	require.NotNil(t, actual, "%s should not be nil", field)
	assertEqualTime(t, *expected, *actual, field)
}

// assertEqualDestination compares two destinations field-by-field.
func assertEqualDestination(t *testing.T, expected, actual models.Destination) {
	t.Helper()
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Type, actual.Type)
	assert.Equal(t, expected.Topics, actual.Topics)
	assert.Equal(t, expected.Filter, actual.Filter)
	assert.Equal(t, expected.Config, actual.Config)
	assert.Equal(t, expected.Credentials, actual.Credentials)
	assert.Equal(t, expected.DeliveryMetadata, actual.DeliveryMetadata)
	assert.Equal(t, expected.Metadata, actual.Metadata)
	assertEqualTime(t, expected.CreatedAt, actual.CreatedAt, "CreatedAt")
	assertEqualTime(t, expected.UpdatedAt, actual.UpdatedAt, "UpdatedAt")
	assertEqualTimePtr(t, expected.DisabledAt, actual.DisabledAt, "DisabledAt")
}
