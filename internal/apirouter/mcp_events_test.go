package apirouter_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func mcpEventsCatalog(t *testing.T) *topicschema.Catalog {
	t.Helper()
	c, err := topicschema.NewCatalog(testutil.TestTopics, topicschema.Definitions{
		{
			Name:        "user.created",
			Description: "A user signed up.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["paid","pending"]}}}`),
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{
				"status":{"type":"string","enum":["paid","pending"]},
				"total":{"type":"number"}
			}}`),
		},
		{
			Name:          "user.updated",
			InputSchema:   json.RawMessage(`{"type":"object","properties":{}}`),
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{"plan":{"type":"string"}}}`),
		},
	})
	require.NoError(t, err)
	return c
}

func newMCPCatalogAPITest(t *testing.T, catalog *topicschema.Catalog) *apiTest {
	t.Helper()
	h, _ := newMCPCatalogAPITestWithVerifier(t, catalog)
	return h
}

func newMCPCatalogAPITestWithVerifier(t *testing.T, catalog *topicschema.Catalog, opts ...apiTestOption) (*apiTest, *stubVerifier) {
	t.Helper()
	verifier := &stubVerifier{}
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	p, err := destmcp.New(registry.MetadataLoader(), destmcp.Config{
		Client:   &http.Client{},
		Guard:    &netguard.Guard{Resolver: publicResolver{}},
		Verifier: verifier,
		Catalog:  catalog,
	})
	require.NoError(t, err)
	require.NoError(t, registry.RegisterProvider(destmcp.Type, p))
	h := newAPITest(t, append([]apiTestOption{withDestRegistry(registry), withMCPVerifier(verifier), withMCPCatalog(catalog)}, opts...)...)
	require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	return h, verifier
}

type mcpEventsResult struct {
	ResultType string `json:"resultType"`
	Events     []struct {
		Name          string          `json:"name"`
		Description   string          `json:"description"`
		InputSchema   json.RawMessage `json:"inputSchema"`
		PayloadSchema json.RawMessage `json:"payloadSchema"`
		Delivery      []string        `json:"delivery"`
	} `json:"events"`
	NextCursor string `json:"nextCursor"`
}

func listMCPEvents(t *testing.T, h *apiTest, query string) (*mcpEventsResult, int) {
	t.Helper()
	resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/mcp/events/list"+query, nil)))
	var out mcpEventsResult
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return &out, resp.Code
}

func TestAPI_MCPEvents_Unavailable(t *testing.T) {
	h, _ := newMCPCatalogAPITestWithVerifier(t, nil, withMCPEventsError(errors.New("EXPERIMENTAL_MCP_EVENTS_SCHEMA: invalid mcp events: invalid JSON")))

	resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/mcp/events/list", nil)))
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
	var body struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "MCP Events unavailable", body.Message)

	t.Run("the request log carries the cause", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		h, _ := newMCPCatalogAPITestWithVerifier(t, nil,
			withMCPEventsError(errors.New("EXPERIMENTAL_MCP_EVENTS_SCHEMA: invalid mcp events: invalid JSON")),
			withLogger(logging.NewTestLogger(zap.New(core))))
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/mcp/events/list", nil)))
		require.Equal(t, http.StatusServiceUnavailable, resp.Code)

		entries := logs.FilterMessage("request completed").All()
		require.Len(t, entries, 1)
		assert.Contains(t, entries[0].ContextMap()["error"], "invalid mcp events: invalid JSON")
	})

	t.Run("the rest of the API runs", func(t *testing.T) {
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)))
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}

func TestAPI_MCPEvents(t *testing.T) {
	h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))

	t.Run("lists the catalog", func(t *testing.T) {
		out, code := listMCPEvents(t, h, "")
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Events, 2)
		assert.Empty(t, out.NextCursor)
		assert.Equal(t, "complete", out.ResultType)

		ev := out.Events[0]
		assert.Equal(t, "user.created", ev.Name)
		assert.Equal(t, "A user signed up.", ev.Description)
		assert.Equal(t, []string{"webhook"}, ev.Delivery)
		assert.JSONEq(t, `{"type":"object","properties":{"status":{"type":"string","enum":["paid","pending"]},"total":{"type":"number"}}}`, string(ev.PayloadSchema))
		assert.JSONEq(t, `{"type":"object","properties":{"status":{"type":"string","enum":["paid","pending"]}}}`, string(ev.InputSchema))
		assert.Equal(t, "user.updated", out.Events[1].Name)
	})

	t.Run("topics restricts", func(t *testing.T) {
		out, code := listMCPEvents(t, h, "?topics=user.updated,user.deleted,nope")
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Events, 1)
		assert.Equal(t, "user.updated", out.Events[0].Name)

		out, code = listMCPEvents(t, h, "?topics=user.deleted")
		require.Equal(t, http.StatusOK, code)
		assert.NotNil(t, out.Events)
		assert.Empty(t, out.Events)
	})

	t.Run("pages", func(t *testing.T) {
		first, code := listMCPEvents(t, h, "?limit=1")
		require.Equal(t, http.StatusOK, code)
		require.Len(t, first.Events, 1)
		assert.Equal(t, "user.created", first.Events[0].Name)
		require.NotEmpty(t, first.NextCursor)

		second, code := listMCPEvents(t, h, "?limit=1&cursor="+first.NextCursor)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, second.Events, 1)
		assert.Equal(t, "user.updated", second.Events[0].Name)
		assert.Empty(t, second.NextCursor)
	})

	t.Run("bad paging parameters", func(t *testing.T) {
		for _, q := range []string{"?limit=0", "?limit=101", "?limit=x", "?cursor=!!"} {
			_, code := listMCPEvents(t, h, q)
			assert.Equal(t, http.StatusBadRequest, code, q)
		}
	})

	t.Run("admin only", func(t *testing.T) {
		resp := h.do(h.jsonReq(http.MethodGet, "/api/v1/mcp/events/list", nil))
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		resp = h.do(h.withJWT(h.jsonReq(http.MethodGet, "/api/v1/mcp/events/list", nil), "t1"))
		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("not found without a catalog", func(t *testing.T) {
		off := newAPITest(t)
		_, code := listMCPEvents(t, off, "")
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestAPI_MCPSubscribeWithCatalog(t *testing.T) {
	newHarness := func(t *testing.T) *apiTest {
		t.Helper()
		h, _ := newMCPCatalogAPITestWithVerifier(t, mcpEventsCatalog(t), withMCPSubscribe(mcpTTLRules))
		return h
	}

	t.Run("derives the filter from the arguments", func(t *testing.T) {
		h := newHarness(t)
		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeParams()))

		stored := storedSubscription(t, h, r.ID)
		require.NotNil(t, stored)
		assert.Equal(t, map[string]any{"data": map[string]any{"status": "paid"}}, map[string]any(stored.Filter))
	})

	t.Run("no arguments, no filter", func(t *testing.T) {
		h := newHarness(t)
		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeParams(func(p map[string]any) { delete(p, "arguments") })))

		stored := storedSubscription(t, h, r.ID)
		require.NotNil(t, stored)
		assert.Empty(t, stored.Filter)
	})

	t.Run("a refresh after the definitions changed", func(t *testing.T) {
		h := newHarness(t)
		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeParams()))

		changed, err := topicschema.NewCatalog(testutil.TestTopics, topicschema.Definitions{
			{Name: "user.updated", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`), PayloadSchema: json.RawMessage(`{"type":"object","properties":{"plan":{"type":"string"}}}`)},
		})
		require.NoError(t, err)
		h2, _ := newMCPCatalogAPITestWithVerifier(t, changed, withTenantStore(h.tenantStore), withMCPSubscribe(mcpTTLRules))

		refreshed := requireSubscribed(t, postMCP(h2, mcpSubscribePath, subscribeParams(func(p map[string]any) {
			p["delivery"] = deliveryParams(mcpCallbackURL, mcpSecret(2))
		})))

		assert.Equal(t, r.ID, refreshed.ID)
		assert.Equal(t, mcpSecret(2), storedSubscription(t, h2, r.ID).Credentials["secret"])
	})
}
