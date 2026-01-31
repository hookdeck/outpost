package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_Tenants(t *testing.T) {
	t.Run("Upsert", func(t *testing.T) {
		t.Run("api key creates tenant", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusCreated, resp.Code)

			// Verify tenant exists in store
			tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "t1")
			require.NoError(t, err)
			assert.Equal(t, "t1", tenant.ID)
		})

		t.Run("api key updates metadata", func(t *testing.T) {
			h := newAPITest(t)

			// Create tenant first
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			// Update with metadata
			req := h.jsonReq(http.MethodPut, "/api/v1/tenants/t1", map[string]any{
				"metadata": map[string]string{"env": "prod"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			// Verify metadata in store
			tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "t1")
			require.NoError(t, err)
			assert.Equal(t, models.Metadata{"env": "prod"}, tenant.Metadata)
		})

		t.Run("jwt updates own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPut, "/api/v1/tenants/t1", map[string]any{
				"metadata": map[string]string{"role": "owner"},
			})
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "t1")
			require.NoError(t, err)
			assert.Equal(t, models.Metadata{"role": "owner"}, tenant.Metadata)
		})
	})

	t.Run("Retrieve", func(t *testing.T) {
		t.Run("api key returns tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var tenant models.Tenant
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &tenant))
			assert.Equal(t, "t1", tenant.ID)
		})

		t.Run("jwt returns own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var tenant models.Tenant
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &tenant))
			assert.Equal(t, "t1", tenant.ID)
		})
	})

	t.Run("List", func(t *testing.T) {
		t.Run("api key returns all tenants", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var result tenantstore.TenantPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Equal(t, 2, result.Count)
			assert.Len(t, result.Models, 2)
		})

		t.Run("jwt returns only own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var result tenantstore.TenantPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Equal(t, 1, result.Count)
			assert.Len(t, result.Models, 1)
			assert.Equal(t, "t1", result.Models[0].ID)
		})
	})

	t.Run("Delete", func(t *testing.T) {
		t.Run("api key deletes tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			// Subsequent GET returns 404
			req = httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
			resp = h.do(h.withAPIKey(req))
			assert.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt deletes own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			// Verify deleted in store
			_, err := h.tenantStore.RetrieveTenant(t.Context(), "t1")
			assert.ErrorIs(t, err, tenantstore.ErrTenantDeleted)
		})
	})

	t.Run("jwt other tenant returns 403", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t2", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("deleted tenant jwt returns 401", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.DeleteTenant(t.Context(), "t1")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		resp := h.do(req)

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})
}
