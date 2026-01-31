package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validDestination is a minimal valid create-destination payload.
func validDestination() map[string]any {
	return map[string]any{
		"type":   "webhook",
		"topics": []string{"user.created"},
		"config": map[string]string{"url": "https://example.com/hook"},
	}
}

func TestAPI_Destinations(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		t.Run("api key creates destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusCreated, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "t1", dest.TenantID)
			assert.Equal(t, "webhook", dest.Type)
			assert.Equal(t, models.Topics{"user.created"}, dest.Topics)

			// Verify in store
			dests, err := h.tenantStore.ListDestinationByTenant(t.Context(), "t1")
			require.NoError(t, err)
			assert.Len(t, dests, 1)
		})

		t.Run("jwt creates destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusCreated, resp.Code)
		})

		t.Run("missing type returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
				"topics": []string{"user.created"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("missing topics returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
				"type": "webhook",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("invalid topic returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
				"type":   "webhook",
				"topics": []string{"order.completed"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})
	})

	t.Run("Retrieve", func(t *testing.T) {
		t.Run("api key returns destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "d1", dest.ID)
		})

		t.Run("nonexistent destination returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/nope", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt returns destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("List", func(t *testing.T) {
		t.Run("api key returns all destinations for tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dests []destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dests))
			assert.Len(t, dests, 2)
		})

		t.Run("jwt returns destinations on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var dests []destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dests))
			assert.Len(t, dests, 1)
		})
	})

	t.Run("Update", func(t *testing.T) {
		t.Run("api key updates destination topics", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Topics{"user.deleted"}, dest.Topics)
		})

		t.Run("api key updates destination config", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithConfig(map[string]string{"url": "https://old.example.com"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"config": map[string]string{"url": "https://new.example.com"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "https://new.example.com", dest.Config["url"])
		})

		t.Run("jwt updates destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("nonexistent destination returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/nope", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t2"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("Delete", func(t *testing.T) {
		t.Run("api key deletes destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			// Subsequent GET returns 404
			req = httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp = h.do(h.withAPIKey(req))
			assert.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("deleted destination returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))
			h.tenantStore.DeleteDestination(t.Context(), "t1", "d1")

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt deletes destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("Enable/Disable", func(t *testing.T) {
		t.Run("api key disables destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.NotNil(t, dest.DisabledAt)
		})

		t.Run("api key enables disabled destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			// Disable first
			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			h.do(h.withAPIKey(req))

			// Enable
			req = httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/enable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Nil(t, dest.DisabledAt)
		})

		t.Run("enable already enabled is noop", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/enable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Nil(t, dest.DisabledAt)
		})

		t.Run("jwt disable on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("enable destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/enable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("disable destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("jwt other tenant returns 403", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t2/destinations", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)
		resp := h.do(req)

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})
}
