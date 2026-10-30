package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const eventsYAML = `experimental:
  mcp:
    enabled: true
    events_schema:
      events:
        - name: order.created
          description: From YAML.
          inputSchema:
            type: object
            properties:
              status: {type: string, enum: [paid, pending]}
          payloadSchema:
            type: object
            properties:
              status: {type: string, enum: [paid, pending]}
  `

const eventsFile = `events:
  - name: order.created
    description: From file.
    inputSchema: {type: object, properties: {}}
    payloadSchema:
      type: object
      properties:
        total: {type: number}
`

const eventsJSON = `{"events":[{"name":"order.created","description":"From JSON.","inputSchema":{"type":"object","properties":{}},"payloadSchema":{"type":"object"}}]}`

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

func TestMCPEvents_YAMLPath(t *testing.T) {
	for file, doc := range map[string]string{"events.yaml": eventsFile, "EVENTS.JSON": eventsJSON} {
		cfg, err := parseMCPErr(nil, "experimental:\n  mcp:\n    enabled: true\n    events_schema: "+file+"\n", map[string]string{file: doc})
		require.NoError(t, err, file)
		assert.Contains(t, []string{"From file.", "From JSON."}, catalogDescription(t, cfg, "order.created"))
	}
}

func TestMCPEvents_EnvPathWinsOverYAML(t *testing.T) {
	cfg, err := parseMCPErr(map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": "events.yaml"}, eventsYAML, map[string]string{"events.yaml": eventsFile})
	require.NoError(t, err)
	assert.Equal(t, "From file.", catalogDescription(t, cfg, "order.created"))
}

func TestMCPEvents_JSONFile(t *testing.T) {
	cfg, err := parseMCPErr(map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_EVENTS_SCHEMA": "events.json"}, "", map[string]string{"events.json": eventsJSON})
	require.NoError(t, err)
	assert.Equal(t, "From JSON.", catalogDescription(t, cfg, "order.created"))

	_, err = parseMCPErr(map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_EVENTS_SCHEMA": "events.json"}, "", map[string]string{
		"events.json": "{\n\t\"events\": [{\"name\": \"a\", \"description\": \"caf\\u00e9\", \"inputSchema\": {\"type\": \"object\", \"properties\": {}}, \"payloadSchema\": {\"type\": \"object\"}}]\n}",
	})
	require.NoError(t, err)
}

func TestMCPEvents_UnsetForms(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		yaml string
	}{
		"absent":     {},
		"empty env":  {env: map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": ""}},
		"blank env":  {env: map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": "   "}},
		"yaml null":  {yaml: "experimental:\n  mcp:\n    events_schema: null\n"},
		"yaml empty": {yaml: "experimental:\n  mcp:\n    events_schema: ''\n"},
		"no events":  {yaml: "experimental:\n  mcp:\n    events_schema: {events: []}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true"}
			for k, v := range tc.env {
				env[k] = v
			}
			cfg := parseMCP(t, env, tc.yaml)
			assert.Nil(t, cfg.MCPCatalog())
			assert.Contains(t, cfg.MCPWarnings(), "EXPERIMENTAL_MCP_ENABLED is true but EXPERIMENTAL_MCP_EVENTS_SCHEMA (experimental.mcp.events_schema) defines no events: every mcp subscription is refused.")
		})
	}
}

func TestMCPEvents_NamesMustBeInTopics(t *testing.T) {
	_, err := parseMCPErr(map[string]string{"TOPICS": "order.updated"}, eventsYAML, nil)
	require.ErrorIs(t, err, config.ErrInvalidMCPConfig)
	assert.Contains(t, err.Error(), "order.created")

	cfg, err := parseMCPErr(map[string]string{"TOPICS": "order.created,order.updated"}, eventsYAML, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"order.created", "order.updated"}, cfg.Topics)
	require.Len(t, cfg.MCPCatalog().Events(), 1)
	assert.NotContains(t, cfg.MCPWarnings(), "defines no events")
}

func TestMCPEvents_Errors(t *testing.T) {
	enabled := map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true"}
	cases := []struct {
		name  string
		env   map[string]string
		yaml  string
		files map[string]string
		want  string
	}{
		{"url in env", map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": " HTTPS://example.com/e.json"}, "", nil,
			"EXPERIMENTAL_MCP_EVENTS_SCHEMA: URLs are not supported; set a file path or inline definitions"},
		{"url in yaml", nil, "experimental:\n  mcp:\n    events_schema: http://example.com/e.json\n", nil,
			"experimental.mcp.events_schema: URLs are not supported; set a file path or inline definitions"},
		{"missing file", map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": "missing.yaml"}, "", nil,
			`EXPERIMENTAL_MCP_EVENTS_SCHEMA: reading "missing.yaml": `},
		{"bad file", map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": "bad.json"}, "", map[string]string{"bad.json": "{"},
			`EXPERIMENTAL_MCP_EVENTS_SCHEMA "bad.json": invalid mcp events: invalid JSON`},
		{"old format", map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": "old.yaml"}, "", map[string]string{"old.yaml": "a:\n  payload_schema: {type: object}\n"},
			`EXPERIMENTAL_MCP_EVENTS_SCHEMA "old.yaml": invalid mcp events:`},
		{"no events key", nil, "experimental:\n  mcp:\n    events_schema: {}\n", nil,
			`experimental.mcp.events_schema: invalid mcp events: must be an object with an "events" list`},
		{"invalid event", nil, "experimental:\n  mcp:\n    events_schema:\n      events:\n        - name: a\n", nil,
			`experimental.mcp.events_schema: invalid mcp events:`},
		{"list", nil, "experimental:\n  mcp:\n    events_schema: [a]\n", nil, `experimental.mcp.events_schema must be a mapping or a file path`},
		{"number", nil, "experimental:\n  mcp:\n    events_schema: 3\n", nil, `experimental.mcp.events_schema must be a mapping or a file path`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range enabled {
				env[k] = v
			}
			for k, v := range tc.env {
				env[k] = v
			}
			_, err := parseMCPErr(env, tc.yaml, tc.files)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
	_, err := parseMCPErr(map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true"}, "experimental:\n  mcp:\n    events_schema:\n      events:\n        - name: a\n", nil)
	assert.ErrorIs(t, err, config.ErrInvalidMCPConfig)
	assert.Contains(t, err.Error(), `event "a": inputSchema is required`)
}

func TestMCPEvents_IgnoredWhenMCPDisabled(t *testing.T) {
	cfg := parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": "missing.yaml"}, "")
	assert.Nil(t, cfg.MCPCatalog())
	assert.Empty(t, cfg.MCPWarnings())
}

func TestMCPEvents_InlineJSONInEnv(t *testing.T) {
	for name, value := range map[string]string{"plain": eventsJSON, "leading space": "  \n" + eventsJSON} {
		t.Run(name, func(t *testing.T) {
			cfg := parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_EVENTS_SCHEMA": value}, "")
			assert.Equal(t, "From JSON.", catalogDescription(t, cfg, "order.created"))
		})
	}

	t.Run("wins over YAML", func(t *testing.T) {
		cfg := parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_EVENTS_SCHEMA": eventsJSON}, eventsYAML)
		assert.Equal(t, "From JSON.", catalogDescription(t, cfg, "order.created"))
	})

	t.Run("invalid JSON names the setting", func(t *testing.T) {
		_, err := parseMCPErr(map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_EVENTS_SCHEMA": `{"events":`}, "", nil)
		require.ErrorIs(t, err, config.ErrInvalidMCPConfig)
		assert.Contains(t, err.Error(), "EXPERIMENTAL_MCP_EVENTS_SCHEMA: invalid mcp events: invalid JSON")
	})

	t.Run("no events", func(t *testing.T) {
		cfg := parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_EVENTS_SCHEMA": `{"events":[]}`}, "")
		assert.Nil(t, cfg.MCPCatalog())
		assert.Contains(t, cfg.MCPWarnings()[len(cfg.MCPWarnings())-1], "defines no events")
	})

	t.Run("missing events list", func(t *testing.T) {
		_, err := parseMCPErr(map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_EVENTS_SCHEMA": `{}`}, "", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `EXPERIMENTAL_MCP_EVENTS_SCHEMA: invalid mcp events: must be an object with an "events" list`)
	})
}
