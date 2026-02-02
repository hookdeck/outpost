package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_Topics(t *testing.T) {
	t.Run("with API key returns topics", func(t *testing.T) {
		h := newAPITest(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var topics []string
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &topics))
		assert.Equal(t, testutil.TestTopics, topics)
	})

	t.Run("with JWT returns topics", func(t *testing.T) {
		h := newAPITest(t)

		// JWT auth middleware resolves the tenant, so it must exist
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusOK, resp.Code)

		var topics []string
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &topics))
		assert.Equal(t, testutil.TestTopics, topics)
	})

	t.Run("without auth returns 401", func(t *testing.T) {
		h := newAPITest(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(req)

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})
}
