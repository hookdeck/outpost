package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/topicschema"
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

	t.Run("no topics configured returns an empty list", func(t *testing.T) {
		h := newAPITest(t, withTopics(nil))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `[]`, resp.Body.String())
	})

	t.Run("a topic catalog does not change the names", func(t *testing.T) {
		h := newAPITest(t, withTopicCatalog(topicschema.EmptyCatalog([]string{"order.created"})))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil)
		resp := h.do(h.withAPIKey(req))

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

func TestAPI_TopicsV2(t *testing.T) {
	// Every topic object of the schema-less test topics.
	const testTopicObjects = `[
		{"name":"user.created","validation":"off","mcp":{"enabled":false}},
		{"name":"user.deleted","validation":"off","mcp":{"enabled":false}},
		{"name":"user.updated","validation":"off","mcp":{"enabled":false}}
	]`

	t.Run("without a catalog returns the configured topics without schemas", func(t *testing.T) {
		h := newAPITest(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/topics", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, testTopicObjects, resp.Body.String())
	})

	t.Run("with JWT returns topic objects", func(t *testing.T) {
		h := newAPITest(t)
		require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))

		req := httptest.NewRequest(http.MethodGet, "/api/v2/topics", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, testTopicObjects, resp.Body.String())
	})

	t.Run("returns the catalog's topics in its order", func(t *testing.T) {
		catalog := topicschema.EmptyCatalog([]string{"order.created", "invoice.paid"})
		h := newAPITest(t, withTopicCatalog(catalog))
		req := httptest.NewRequest(http.MethodGet, "/api/v2/topics", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `[
			{"name":"order.created","validation":"off","mcp":{"enabled":false}},
			{"name":"invoice.paid","validation":"off","mcp":{"enabled":false}}
		]`, resp.Body.String())

		var topics []topicschema.Topic
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &topics))
		assert.Equal(t, catalog.Topics(), topics)
	})

	t.Run("no topics configured returns an empty list", func(t *testing.T) {
		for _, catalog := range []*topicschema.Catalog{nil, topicschema.EmptyCatalog(nil)} {
			h := newAPITest(t, withTopics(nil), withTopicCatalog(catalog))
			req := httptest.NewRequest(http.MethodGet, "/api/v2/topics", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			assert.JSONEq(t, `[]`, resp.Body.String())
		}
	})

	t.Run("concurrent requests get the same body", func(t *testing.T) {
		h := newAPITest(t)
		bodies := make([]string, 16)
		var wg sync.WaitGroup
		for i := range bodies {
			wg.Go(func() {
				resp := httptest.NewRecorder()
				h.router.ServeHTTP(resp, h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v2/topics", nil)))
				bodies[i] = resp.Body.String()
			})
		}
		wg.Wait()
		for _, body := range bodies {
			assert.JSONEq(t, testTopicObjects, body)
		}
	})

	t.Run("without auth returns 401", func(t *testing.T) {
		h := newAPITest(t)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/topics", nil)
		testutil.RequireErrorResponse(t, h.do(req), http.StatusUnauthorized, "unauthorized")
	})
}
