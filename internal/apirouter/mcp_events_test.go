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
	h := newAPITest(t, append([]apiTestOption{withDestRegistry(registry), withMCPVerifier(verifier)}, opts...)...)
	require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	return h, verifier
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
		requireValidation422(t, raw, "config.arguments.* failed invalid validation")
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
			{Name: "user.updated", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`), PayloadSchema: json.RawMessage(`{"type":"object","properties":{"plan":{"type":"string"}}}`)},
		})
		require.NoError(t, err)
		h2, _ := newMCPCatalogAPITestWithVerifier(t, changed, withTenantStore(h.tenantStore))
		raw := patchMCP(t, h2, map[string]any{"credentials": map[string]string{"secret": mcpSecret(2)}}, false)
		assert.Equal(t, http.StatusOK, raw.Code, raw.Body.String())
	})

}

type stubResponse struct {
	Code int
	Body string
}
