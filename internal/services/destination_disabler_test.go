package services_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// racingStore runs afterRetrieve right after each RetrieveDestination.
type racingStore struct {
	tenantstore.TenantStore
	mu            sync.Mutex
	retrieves     int
	afterRetrieve func(n int)
}

func (s *racingStore) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error) {
	destination, err := s.TenantStore.RetrieveDestination(ctx, tenantID, destinationID)
	s.mu.Lock()
	s.retrieves++
	n, hook := s.retrieves, s.afterRetrieve
	s.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return destination, err
}

func newDisablerFixture(t *testing.T) (*racingStore, models.Destination) {
	t.Helper()
	store := &racingStore{TenantStore: tenantstore.NewMemTenantStore()}
	tenant := testutil.TenantFactory.Any()
	require.NoError(t, store.UpsertTenant(t.Context(), tenant))
	destination := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenant.ID))
	require.NoError(t, store.CreateDestination(t.Context(), destination))
	return store, destination
}

func TestDestinationDisabler(t *testing.T) {
	t.Run("disables a destination", func(t *testing.T) {
		store, destination := newDisablerFixture(t)

		require.NoError(t, services.NewDestinationDisabler(store).DisableDestination(t.Context(), destination.TenantID, destination.ID))

		stored, err := store.TenantStore.RetrieveDestination(t.Context(), destination.TenantID, destination.ID)
		require.NoError(t, err)
		assert.NotNil(t, stored.DisabledAt)
	})

	t.Run("a destination deleted meanwhile stays deleted", func(t *testing.T) {
		store, destination := newDisablerFixture(t)
		store.afterRetrieve = func(int) {
			require.NoError(t, store.TenantStore.DeleteDestination(t.Context(), destination.TenantID, destination.ID))
		}

		require.NoError(t, services.NewDestinationDisabler(store).DisableDestination(t.Context(), destination.TenantID, destination.ID))

		store.afterRetrieve = nil
		_, err := store.TenantStore.RetrieveDestination(t.Context(), destination.TenantID, destination.ID)
		assert.ErrorIs(t, err, tenantstore.ErrDestinationDeleted)
		destinations, err := store.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: destination.TenantID})
		require.NoError(t, err)
		assert.Empty(t, destinations)
	})

	t.Run("a user change made meanwhile survives", func(t *testing.T) {
		store, destination := newDisablerFixture(t)
		store.afterRetrieve = func(n int) {
			if n != 1 {
				return
			}
			current, err := store.TenantStore.RetrieveDestination(t.Context(), destination.TenantID, destination.ID)
			require.NoError(t, err)
			current.Config = map[string]string{"url": "https://example.com/new"}
			current.Metadata = map[string]string{"by": "user"}
			require.NoError(t, store.TenantStore.UpsertDestination(t.Context(), *current))
		}

		require.NoError(t, services.NewDestinationDisabler(store).DisableDestination(t.Context(), destination.TenantID, destination.ID))

		store.afterRetrieve = nil
		stored, err := store.TenantStore.RetrieveDestination(t.Context(), destination.TenantID, destination.ID)
		require.NoError(t, err)
		assert.NotNil(t, stored.DisabledAt)
		assert.Equal(t, models.Config{"url": "https://example.com/new"}, stored.Config)
		assert.Equal(t, models.Metadata{"by": "user"}, stored.Metadata)
	})

	t.Run("gives up with an error when it keeps losing the race", func(t *testing.T) {
		store, destination := newDisablerFixture(t)
		store.afterRetrieve = func(int) {
			current, err := store.TenantStore.RetrieveDestination(t.Context(), destination.TenantID, destination.ID)
			require.NoError(t, err)
			current.UpdatedAt = time.Now()
			require.NoError(t, store.TenantStore.UpsertDestination(t.Context(), *current))
		}

		err := services.NewDestinationDisabler(store).DisableDestination(t.Context(), destination.TenantID, destination.ID)
		assert.ErrorIs(t, err, tenantstore.ErrDestinationConflict)
	})
}
