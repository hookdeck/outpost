package apirouter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/tenantstore/memtenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// racingStore runs afterRetrieve right after each RetrieveDestination, so a
// test can land a competing write between a handler's read and its write.
type racingStore struct {
	tenantstore.TenantStore
	mu            sync.Mutex
	retrieves     int
	afterRetrieve func(n int)
	// afterTenantCheck runs once, right after the first RetrieveTenant.
	afterTenantCheck func()
}

func (s *racingStore) RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error) {
	tenant, err := s.TenantStore.RetrieveTenant(ctx, tenantID)
	s.mu.Lock()
	hook := s.afterTenantCheck
	s.afterTenantCheck = nil
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return tenant, err
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

func (s *racingStore) stopRacing() {
	s.mu.Lock()
	s.afterRetrieve = nil
	s.mu.Unlock()
}

// competingUpdate applies mutate to the stored destination as another writer
// would, bypassing the handler.
func competingUpdate(t *testing.T, store tenantstore.TenantStore, tenantID, destinationID string, mutate func(*models.Destination)) {
	t.Helper()
	current, err := store.RetrieveDestination(t.Context(), tenantID, destinationID)
	require.NoError(t, err)
	mutate(current)
	require.NoError(t, store.UpsertDestination(t.Context(), *current))
}

func newRacingAPITest(t *testing.T) (*apiTest, *racingStore) {
	t.Helper()
	rs := &racingStore{TenantStore: memtenantstore.New()}
	h := newAPITest(t, withTenantStore(rs))
	require.NoError(t, rs.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	require.NoError(t, rs.CreateDestination(t.Context(), df.Any(
		df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
	)))
	return h, rs
}

func (a *apiTest) patchDestination(body map[string]any) *httptest.ResponseRecorder {
	a.t.Helper()
	return a.do(a.withAPIKey(a.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", body)))
}

func (a *apiTest) putDestination(action string) *httptest.ResponseRecorder {
	a.t.Helper()
	return a.do(a.withAPIKey(httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/"+action, nil)))
}

func (a *apiTest) requireDestinationGone() {
	a.t.Helper()
	resp := a.do(a.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)))
	require.Equal(a.t, http.StatusNotFound, resp.Code)
	destinations, err := a.tenantStore.ListDestination(a.t.Context(), tenantstore.ListDestinationRequest{TenantID: "t1"})
	require.NoError(a.t, err)
	require.Empty(a.t, destinations)
}

func TestAPI_DestinationWriteRaces(t *testing.T) {
	deleteOnRetrieve := func(rs *racingStore) {
		rs.afterRetrieve = func(int) {
			require.NoError(t, rs.TenantStore.DeleteDestination(t.Context(), "t1", "d1"))
		}
	}

	t.Run("update racing a delete returns 404 and stays deleted", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		deleteOnRetrieve(rs)

		resp := h.patchDestination(map[string]any{"topics": []string{"user.deleted"}})
		assert.Equal(t, http.StatusNotFound, resp.Code)

		rs.stopRacing()
		h.requireDestinationGone()
	})

	t.Run("disable racing a delete returns 404 and stays deleted", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		deleteOnRetrieve(rs)

		resp := h.putDestination("disable")
		assert.Equal(t, http.StatusNotFound, resp.Code)

		rs.stopRacing()
		h.requireDestinationGone()
	})

	t.Run("enable racing a delete returns 404 and stays deleted", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		competingUpdate(t, rs.TenantStore, "t1", "d1", func(d *models.Destination) {
			now := time.Now()
			d.DisabledAt = &now
		})
		deleteOnRetrieve(rs)

		resp := h.putDestination("enable")
		assert.Equal(t, http.StatusNotFound, resp.Code)

		rs.stopRacing()
		h.requireDestinationGone()
	})

	t.Run("update racing another update keeps both changes", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		rs.afterRetrieve = func(n int) {
			if n == 1 {
				competingUpdate(t, rs.TenantStore, "t1", "d1", func(d *models.Destination) {
					now := time.Now()
					d.DisabledAt = &now
					d.Metadata = map[string]string{"by": "other"}
				})
			}
		}

		resp := h.patchDestination(map[string]any{"topics": []string{"user.deleted"}})
		require.Equal(t, http.StatusOK, resp.Code)
		var display destregistry.DestinationDisplay
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &display))
		assert.Equal(t, models.Topics{"user.deleted"}, display.Topics)

		rs.stopRacing()
		stored, err := rs.TenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Equal(t, models.Topics{"user.deleted"}, stored.Topics)
		assert.Equal(t, models.Metadata{"by": "other"}, stored.Metadata)
		assert.NotNil(t, stored.DisabledAt, "a disable made during the update must survive it")
	})

	t.Run("disable racing an update keeps the update", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		rs.afterRetrieve = func(n int) {
			if n == 1 {
				competingUpdate(t, rs.TenantStore, "t1", "d1", func(d *models.Destination) {
					d.Config = map[string]string{"url": "https://example.com/new"}
				})
			}
		}

		resp := h.putDestination("disable")
		require.Equal(t, http.StatusOK, resp.Code)

		rs.stopRacing()
		stored, err := rs.TenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.NotNil(t, stored.DisabledAt)
		assert.Equal(t, models.Config{"url": "https://example.com/new"}, stored.Config)
	})

	t.Run("update that keeps losing the race returns 409", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		rs.afterRetrieve = func(n int) {
			competingUpdate(t, rs.TenantStore, "t1", "d1", func(d *models.Destination) {
				d.Metadata = map[string]string{"n": string(rune('a' + n))}
			})
		}

		resp := h.patchDestination(map[string]any{"topics": []string{"user.deleted"}})
		assert.Equal(t, http.StatusConflict, resp.Code)

		rs.stopRacing()
		stored, err := rs.TenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Equal(t, models.Topics{"user.created"}, stored.Topics)
	})

	t.Run("disable that keeps losing the race returns 409", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		rs.afterRetrieve = func(n int) {
			competingUpdate(t, rs.TenantStore, "t1", "d1", func(d *models.Destination) {
				d.Metadata = map[string]string{"n": string(rune('a' + n))}
			})
		}

		resp := h.putDestination("disable")
		assert.Equal(t, http.StatusConflict, resp.Code)

		rs.stopRacing()
		stored, err := rs.TenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Nil(t, stored.DisabledAt)
	})

	t.Run("create racing a tenant delete returns 404", func(t *testing.T) {
		h, rs := newRacingAPITest(t)
		rs.afterTenantCheck = func() {
			require.NoError(t, rs.TenantStore.DeleteTenant(t.Context(), "t1"))
		}

		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())))
		assert.Equal(t, http.StatusNotFound, resp.Code)

		destinations, err := rs.TenantStore.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: "t1"})
		require.NoError(t, err)
		assert.Empty(t, destinations)
	})
}
