package apirouter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listUnsupportedStore wraps a TenantStore and overrides ListTenant
// to return ErrListTenantNotSupported, simulating a store backend
// (e.g., Redis without RediSearch) that doesn't support listing.
type listUnsupportedStore struct {
	tenantstore.TenantStore
}

func (s *listUnsupportedStore) ListTenant(_ context.Context, _ tenantstore.ListTenantRequest) (*tenantstore.TenantPaginatedResult, error) {
	return nil, tenantstore.ErrListTenantNotSupported
}

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

		t.Run("jwt nonexistent tenant returns 401", func(t *testing.T) {
			h := newAPITest(t)
			// t1 doesn't exist — AuthMiddleware rejects before handler runs
			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})

		t.Run("api key deleted tenant recreates", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.DeleteTenant(t.Context(), "t1")

			// Upsert on deleted tenant should recreate it
			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusCreated, resp.Code)

			// Verify tenant exists again in store
			tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "t1")
			require.NoError(t, err)
			assert.Equal(t, "t1", tenant.ID)
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

		t.Run("Pagination", func(t *testing.T) {
			h := newAPITest(t)

			now := time.Now()
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"), tf.WithCreatedAt(now.Add(-2*time.Second))))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"), tf.WithCreatedAt(now.Add(-1*time.Second))))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t3"), tf.WithCreatedAt(now)))

			t.Run("forward pagination first page", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "t3", result.Models[0].ID)
				assert.Equal(t, 3, result.Count)
				assert.NotNil(t, result.Pagination.Next)
				assert.Nil(t, result.Pagination.Prev)
			})

			t.Run("next cursor returns second page", func(t *testing.T) {
				// Get first page
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=1", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))
				require.NotNil(t, page1.Pagination.Next)

				// Get second page
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tenants?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var page2 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))
				require.Len(t, page2.Models, 1)
				assert.Equal(t, "t2", page2.Models[0].ID)
				assert.Equal(t, 3, page2.Count)
				assert.NotNil(t, page2.Pagination.Next)
				assert.NotNil(t, page2.Pagination.Prev)
			})

			t.Run("last page has no next cursor", func(t *testing.T) {
				// Navigate to last page
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=1", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tenants?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page2 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tenants?limit=1&next=%s", *page2.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var page3 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page3))
				require.Len(t, page3.Models, 1)
				assert.Equal(t, "t1", page3.Models[0].ID)
				assert.Equal(t, 3, page3.Count)
				assert.Nil(t, page3.Pagination.Next)
				assert.NotNil(t, page3.Pagination.Prev)
			})

			t.Run("prev cursor returns previous page", func(t *testing.T) {
				// Navigate to last page
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=1", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tenants?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page2 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tenants?limit=1&next=%s", *page2.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page3 tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page3))
				require.NotNil(t, page3.Pagination.Prev)

				// Go back
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tenants?limit=1&prev=%s", *page3.Pagination.Prev), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var prevPage tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &prevPage))
				require.Len(t, prevPage.Models, 1)
				assert.Equal(t, "t2", prevPage.Models[0].ID)
			})

			t.Run("dir asc reverses order", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=1&dir=asc", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "t1", result.Models[0].ID)
			})

			t.Run("limit caps results", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=2", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result tenantstore.TenantPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 2)
				assert.Equal(t, 3, result.Count)
				assert.NotNil(t, result.Pagination.Next)
			})
		})

		t.Run("Validation", func(t *testing.T) {
			t.Run("invalid dir returns 422", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?dir=sideways", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("both next and prev returns 400", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?next=abc&prev=def", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusBadRequest, resp.Code)
			})
		})

		t.Run("list not supported returns 501", func(t *testing.T) {
			h := newAPITest(t, withTenantStore(&listUnsupportedStore{tenantstore.NewMemTenantStore()}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotImplemented, resp.Code)
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

	t.Run("RetrieveToken", func(t *testing.T) {
		t.Run("api key returns token and tenant id", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/token", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(t, "t1", body["tenant_id"])
			assert.NotEmpty(t, body["token"])

			// Verify the returned JWT is valid and has correct claims
			claims, err := apirouter.JWT.Extract(testJWTSecret, body["token"])
			require.NoError(t, err)
			assert.Equal(t, "t1", claims.TenantID)
		})

		t.Run("nonexistent tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/nope/token", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt returns 403", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/token", nil)
			resp := h.do(h.withJWT(req, "t1"))

			// Token endpoint is admin-only; JWT auth should be rejected
			require.Equal(t, http.StatusForbidden, resp.Code)
		})

		t.Run("no auth returns 401", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/token", nil)
			resp := h.do(req)

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})
	})

	t.Run("RetrievePortal", func(t *testing.T) {
		t.Run("api key returns redirect url with token", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/portal", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(t, "t1", body["tenant_id"])
			assert.NotEmpty(t, body["redirect_url"])
			assert.True(t, strings.Contains(body["redirect_url"], "token="))
		})

		t.Run("theme dark", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/portal?theme=dark", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.True(t, strings.Contains(body["redirect_url"], "theme=dark"))
		})

		t.Run("theme light", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/portal?theme=light", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.True(t, strings.Contains(body["redirect_url"], "theme=light"))
		})

		t.Run("invalid theme omitted", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/portal?theme=neon", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.False(t, strings.Contains(body["redirect_url"], "theme="))
		})

		t.Run("nonexistent tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/nope/portal", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt returns 403", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/portal", nil)
			resp := h.do(h.withJWT(req, "t1"))

			// Portal endpoint is admin-only; JWT auth should be rejected
			require.Equal(t, http.StatusForbidden, resp.Code)
		})

		t.Run("no auth returns 401", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/portal", nil)
			resp := h.do(req)

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})
	})
}
