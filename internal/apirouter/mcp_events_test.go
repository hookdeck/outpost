package apirouter_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mcpEventsCatalog(t *testing.T) *topicschema.Catalog {
	t.Helper()
	c, err := topicschema.NewCatalog(testutil.TestTopics, topicschema.Definitions{
		"user.created": {
			Description: "A user signed up.",
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{
				"status":{"type":"string","enum":["paid","pending"]},
				"total":{"type":"number","x-mcp-filter":false}
			}}`),
		},
		"user.updated": {
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
	resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/mcp/events"+query, nil)))
	var out mcpEventsResult
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return &out, resp.Code
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
		var input struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(ev.InputSchema, &input))
		assert.Contains(t, input.Properties, "status")
		assert.NotContains(t, input.Properties, "total", "x-mcp-filter: false hides the property")
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
		resp := h.do(h.jsonReq(http.MethodGet, "/api/v1/mcp/events", nil))
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		resp = h.do(h.withJWT(h.jsonReq(http.MethodGet, "/api/v1/mcp/events", nil), "t1"))
		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("not found without a catalog", func(t *testing.T) {
		off := newAPITest(t)
		_, code := listMCPEvents(t, off, "")
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestAPI_MCPDestinationsWithCatalog(t *testing.T) {
	create := func(t *testing.T, h *apiTest, mutate ...func(map[string]any)) (*stubResponse, map[string]any) {
		t.Helper()
		body := mcpDestination(append([]func(map[string]any){func(d map[string]any) {
			d["config"] = map[string]string{"url": mcpCallbackURL, "principal": "user-1", "arguments": `{"status":"paid"}`}
		}}, mutate...)...)
		resp := createMCP(t, h, body)
		return &stubResponse{Code: resp.Code, Body: resp.Body.String()}, body
	}

	t.Run("derives the filter from the arguments", func(t *testing.T) {
		h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))
		resp, _ := create(t, h)
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body)
		var created struct {
			Filter json.RawMessage `json:"filter"`
		}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &created))
		assert.JSONEq(t, `{"data":{"status":"paid"}}`, string(created.Filter))

		stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"data": map[string]any{"status": "paid"}}, map[string]any(stored.Filter))
	})

	t.Run("no arguments, no filter", func(t *testing.T) {
		h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))
		resp, _ := create(t, h, func(d map[string]any) {
			d["config"] = map[string]string{"url": mcpCallbackURL}
		})
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body)
		stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
		require.NoError(t, err)
		assert.Empty(t, stored.Filter)
	})

	t.Run("topic must be a catalog event", func(t *testing.T) {
		h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))
		resp, _ := create(t, h, func(d map[string]any) { d["topics"] = []string{"user.deleted"} })
		assert.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		assert.Contains(t, resp.Body, "topics failed not_found validation")
	})

	t.Run("arguments are validated", func(t *testing.T) {
		h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))
		raw := createMCP(t, h, mcpDestination(func(d map[string]any) {
			d["config"] = map[string]string{"url": mcpCallbackURL, "arguments": `{"status":"late"}`}
		}))
		requireValidation422(t, raw, "config.arguments.status failed invalid validation")

		raw = createMCP(t, h, mcpDestination(func(d map[string]any) {
			d["config"] = map[string]string{"url": mcpCallbackURL, "arguments": `{"total":1}`}
		}))
		requireValidation422(t, raw, "config.arguments failed invalid validation")
	})

	t.Run("filter must be absent", func(t *testing.T) {
		h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))
		raw := createMCP(t, h, mcpDestination(func(d map[string]any) {
			d["filter"] = map[string]any{"data": map[string]any{"status": "pending"}}
		}))
		requireValidation422(t, raw, "filter failed invalid validation")
	})

	t.Run("rejected creates send no verification challenge", func(t *testing.T) {
		h, verifier := newMCPCatalogAPITestWithVerifier(t, mcpEventsCatalog(t))
		for name, mutate := range map[string]func(map[string]any){
			"filter": func(d map[string]any) { d["filter"] = map[string]any{"data": map[string]any{"status": "pending"}} },
			"topic":  func(d map[string]any) { d["topics"] = []string{"user.deleted"} },
			"arguments": func(d map[string]any) {
				d["config"] = map[string]string{"url": mcpCallbackURL, "arguments": `{"status":"late"}`}
			},
		} {
			resp := createMCP(t, h, mcpDestination(mutate))
			assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, name)
		}
		assert.Empty(t, verifier.requests)
	})

	t.Run("secret rotation after the definitions changed", func(t *testing.T) {
		h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))
		resp, _ := create(t, h)
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body)

		changed, err := topicschema.NewCatalog(testutil.TestTopics, topicschema.Definitions{
			"user.updated": {PayloadSchema: json.RawMessage(`{"type":"object","properties":{"plan":{"type":"string"}}}`)},
		})
		require.NoError(t, err)
		h2, _ := newMCPCatalogAPITestWithVerifier(t, changed, withTenantStore(h.tenantStore))
		raw := patchMCP(t, h2, map[string]any{"credentials": map[string]string{"secret": mcpSecret(2)}}, false)
		assert.Equal(t, http.StatusOK, raw.Code, raw.Body.String())
	})

	t.Run("filter is immutable on update", func(t *testing.T) {
		h := newMCPCatalogAPITest(t, mcpEventsCatalog(t))
		resp, _ := create(t, h)
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body)

		raw := patchMCP(t, h, map[string]any{"filter": map[string]any{"data": map[string]any{"status": "pending"}}}, false)
		requireValidation422(t, raw, "filter failed immutable validation")

		raw = patchMCP(t, h, map[string]any{"credentials": map[string]string{"secret": mcpSecret(2)}}, false)
		require.Equal(t, http.StatusOK, raw.Code, raw.Body.String())
	})
}

type stubResponse struct {
	Code int
	Body string
}
