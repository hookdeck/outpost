package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_Topics(t *testing.T) {
	t.Run("with API key returns topics", func(t *testing.T) {
		h := newAPITest(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusOK, resp.Code)

		var topics []string
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &topics))
		assert.Equal(t, []string{"user.created", "order.completed"}, topics)
	})

	t.Run("with JWT returns topics", func(t *testing.T) {
		h := newAPITest(t)

		// JWT auth middleware resolves the tenant, so it must exist
		h.tenantStore.UpsertTenant(t.Context(), models.Tenant{ID: "t1"})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(h.withJWT(req, "t1"))

		assert.Equal(t, http.StatusOK, resp.Code)

		var topics []string
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &topics))
		assert.Equal(t, []string{"user.created", "order.completed"}, topics)
	})

	t.Run("without auth returns 401", func(t *testing.T) {
		h := newAPITest(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(req)

		assert.Equal(t, http.StatusUnauthorized, resp.Code)
	})
}
