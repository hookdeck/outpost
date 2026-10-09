package apirouter_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (m *mcpTest) getTypes(version, path string) (int, []byte) {
	m.t.Helper()
	resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/"+version+path, nil)))
	return resp.Code, resp.Body.Bytes()
}

func TestMCPDestinationTypes(t *testing.T) {
	m := newMCPTest(t, withMCPServerURL("https://mcp.acme.com/mcp"))
	webhook := fakeWebhookProvider{}.Metadata()

	t.Run("v1 is unchanged and leaves mcp out", func(t *testing.T) {
		code, body := m.getTypes("v1", "/destination-types")
		require.Equal(t, http.StatusOK, code)
		want, err := json.Marshal([]*metadata.ProviderMetadata{webhook})
		require.NoError(t, err)
		assert.Equal(t, string(want), string(body), "the bytes of the metadata, no create_mode")

		code, body = m.getTypes("v1", "/destination-types/webhook")
		require.Equal(t, http.StatusOK, code)
		want, err = json.Marshal(webhook)
		require.NoError(t, err)
		assert.Equal(t, string(want), string(body))

		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v1/destination-types/mcp", nil)))
		testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination type not found")
	})

	t.Run("v2 adds create_mode and renders the mcp instructions", func(t *testing.T) {
		code, body := m.getTypes("v2", "/destination-types")
		require.Equal(t, http.StatusOK, code)
		var types []map[string]any
		require.NoError(t, json.Unmarshal(body, &types))
		require.Len(t, types, 2)

		assert.Equal(t, "webhook", types[0]["type"])
		assert.Equal(t, "form", types[0]["create_mode"])
		assert.Equal(t, webhook.Instructions, types[0]["instructions"], "only mcp instructions are a template")

		assert.Equal(t, "mcp", types[1]["type"])
		assert.Equal(t, "external", types[1]["create_mode"])
		const rendered = "Connect https\\:\\/\\/mcp\\.acme\\.com\\/mcp to your agent.\n\n- order\\.created\n- order\\.shipped\n"
		assert.Equal(t, rendered, types[1]["instructions"])

		code, one := m.getTypes("v2", "/destination-types/mcp")
		require.Equal(t, http.StatusOK, code)
		var mcpType map[string]any
		require.NoError(t, json.Unmarshal(one, &mcpType))
		assert.Equal(t, types[1], mcpType)

		code, one = m.getTypes("v2", "/destination-types/webhook")
		require.Equal(t, http.StatusOK, code)
		var webhookType map[string]any
		require.NoError(t, json.Unmarshal(one, &webhookType))
		assert.Equal(t, types[0], webhookType)

		assert.Equal(t, mcpInstructionsTemplate, m.provider.Metadata().Instructions, "the shared metadata is never modified")
	})

	t.Run("v2 leaves mcp out until a topic is MCP-enabled", func(t *testing.T) {
		m := newMCPTest(t, withMCPCatalog(topicschema.EmptyCatalog(mcpTopics)))
		code, body := m.getTypes("v2", "/destination-types")
		require.Equal(t, http.StatusOK, code)
		var types []map[string]any
		require.NoError(t, json.Unmarshal(body, &types))
		require.Len(t, types, 1)
		assert.Equal(t, "webhook", types[0]["type"])

		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v2/destination-types/mcp", nil)))
		testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination type not found")
	})

	t.Run("without MCP_SERVER_URL", func(t *testing.T) {
		m := newMCPTest(t, withoutMCPDeps())
		code, body := m.getTypes("v2", "/destination-types/mcp")
		require.Equal(t, http.StatusOK, code)
		var mcpType map[string]any
		require.NoError(t, json.Unmarshal(body, &mcpType))
		assert.Equal(t, "Connect your MCP server to your agent.\n\n- order\\.created\n- order\\.shipped\n", mcpType["instructions"])
	})

	t.Run("JWT", func(t *testing.T) {
		resp := m.do(m.withJWT(m.jsonReq(http.MethodGet, "/api/v2/destination-types", nil), mcpTenant))
		require.Equal(t, http.StatusOK, resp.Code)
	})
}
