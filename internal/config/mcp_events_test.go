package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const eventsYAML = `mcp:
  enabled: true
  events:
    order.created:
      description: From YAML.
      payload_schema:
        type: object
        properties:
          status: {type: string, enum: [paid, pending]}
`

const eventsFile = `order.created:
  description: From file.
  payload_schema:
    type: object
    properties:
      total: {type: number}
`

func catalogDescription(t *testing.T, cfg *config.Config, topic string) string {
	t.Helper()
	require.NotNil(t, cfg.MCPCatalog())
	ev, ok := cfg.MCPCatalog().Event(topic)
	require.True(t, ok, topic)
	return ev.Description
}

func TestMCPEvents_LoadFromYAML(t *testing.T) {
	cfg := parseMCP(t, nil, eventsYAML)
	assert.Equal(t, "From YAML.", catalogDescription(t, cfg, "order.created"))
	assert.Empty(t, cfg.Topics, "TOPICS stays unset")
}

func TestMCPEvents_FileWinsOverYAML(t *testing.T) {
	cfg, err := parseMCPErr(map[string]string{"MCP_EVENTS_FILE": "events.yaml"}, eventsYAML, map[string]string{"events.yaml": eventsFile})
	require.NoError(t, err)
	assert.Equal(t, "From file.", catalogDescription(t, cfg, "order.created"))
}

func TestMCPEvents_JSONFile(t *testing.T) {
	cfg, err := parseMCPErr(map[string]string{"MCP_ENABLED": "true", "MCP_EVENTS_FILE": "events.json"}, "", map[string]string{
		"events.json": `{"order.created":{"description":"From JSON.","payload_schema":{"type":"object"}}}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "From JSON.", catalogDescription(t, cfg, "order.created"))
}

func TestMCPEvents_JSONFileUsesJSONRules(t *testing.T) {
	env := map[string]string{"MCP_ENABLED": "true", "MCP_EVENTS_FILE": "events.json"}
	doc := `{"order.created":{"description":"\ud83d\ude00 a\/b","payload_schema":{"type":"object"}}}`
	cfg, err := parseMCPErr(env, "", map[string]string{"events.json": doc})
	require.NoError(t, err)
	assert.Equal(t, "\U0001F600 a/b", catalogDescription(t, cfg, "order.created"))

	env["MCP_EVENTS_FILE"] = "events.yaml"
	_, err = parseMCPErr(env, "", map[string]string{"events.yaml": doc})
	require.ErrorIs(t, err, config.ErrInvalidMCPConfig, "other extensions are YAML")
}

func TestMCPEvents_KeysMustBeInTopics(t *testing.T) {
	_, err := parseMCPErr(map[string]string{"TOPICS": "order.updated"}, eventsYAML, nil)
	require.ErrorIs(t, err, config.ErrInvalidMCPConfig)
	assert.Contains(t, err.Error(), "order.created")

	cfg, err := parseMCPErr(map[string]string{"TOPICS": "order.created,order.updated"}, eventsYAML, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"order.created", "order.updated"}, cfg.Topics)
	require.Len(t, cfg.MCPCatalog().Events(), 1)
}

func TestMCPEvents_InvalidDefinitionsFailStartup(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml  string
		files map[string]string
		env   map[string]string
	}{
		"schema is not valid JSON Schema": {yaml: "mcp:\n  enabled: true\n  events:\n    a:\n      payload_schema: {type: nope}\n"},
		"schema is not an object":         {yaml: "mcp:\n  enabled: true\n  events:\n    a:\n      payload_schema: {type: string}\n"},
		"missing schema":                  {yaml: "mcp:\n  enabled: true\n  events:\n    a:\n      description: x\n"},
		"unknown field":                   {yaml: "mcp:\n  enabled: true\n  events:\n    a:\n      payload_schema: {type: object}\n      desciption: x\n"},
		"mcp key":                         {yaml: "mcp:\n  enabled: true\n  events:\n    a:\n      payload_schema: {type: object}\n      mcp: {enabled: false}\n"},
		"name key":                        {yaml: "mcp:\n  enabled: true\n  events:\n    a:\n      payload_schema: {type: object}\n      name: a\n"},
		"merge key":                       {yaml: "mcp:\n  enabled: true\n  events:\n    a: &a\n      payload_schema: {type: object}\n    b:\n      <<: *a\n"},
		"missing file":                    {yaml: "mcp:\n  enabled: true\n", env: map[string]string{"MCP_EVENTS_FILE": "missing.yaml"}},
		"bad file":                        {yaml: "mcp:\n  enabled: true\n", env: map[string]string{"MCP_EVENTS_FILE": "bad.json"}, files: map[string]string{"bad.json": "{"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseMCPErr(tc.env, tc.yaml, tc.files)
			require.ErrorIs(t, err, config.ErrInvalidMCPConfig)
		})
	}
}

func TestMCPEvents_ErrorsNameEventDefinitions(t *testing.T) {
	_, err := parseMCPErr(nil, "mcp:\n  enabled: true\n  events:\n    a:\n      description: x\n", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mcp events")
	assert.Contains(t, err.Error(), "payload_schema is required")
}

func TestMCPEvents_IgnoredWhenMCPDisabled(t *testing.T) {
	cfg := parseMCP(t, map[string]string{"MCP_EVENTS_FILE": "missing.yaml"}, "mcp:\n  events:\n    a:\n      payload_schema: {type: nope}\n")
	assert.Nil(t, cfg.MCPCatalog())
}

func TestMCPEvents_JSONFileWithTabsAndEscapes(t *testing.T) {
	cfg, err := parseMCPErr(map[string]string{"MCP_ENABLED": "true", "MCP_EVENTS_FILE": "events.json"}, "", map[string]string{
		"events.json": "{\n\t\"order.created\": {\n\t\t\"description\": \"caf\\u00e9\",\n\t\t\"payload_schema\": {\"type\": \"object\"}\n\t}\n}",
	})
	require.NoError(t, err)
	assert.Equal(t, "café", catalogDescription(t, cfg, "order.created"))
}

func TestMCPEvents_NoneConfigured(t *testing.T) {
	cfg := parseMCP(t, map[string]string{"MCP_ENABLED": "true"}, "")
	assert.Nil(t, cfg.MCPCatalog())
}
