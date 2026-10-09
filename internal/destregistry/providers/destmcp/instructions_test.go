package destmcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadata(t *testing.T) {
	t.Parallel()
	p := newProvider(t)
	meta := p.Metadata()
	assert.Equal(t, destmcp.Type, meta.Type)
	assert.Equal(t, metadata.CreateModeExternal, meta.CreateMode)
	assert.Equal(t, "MCP Events", meta.Label)
	assert.NotEmpty(t, meta.Description)
	assert.True(t, strings.HasPrefix(meta.Icon, "<svg "), "a static inline SVG")
	for _, unsafe := range []string{"<script", "href", "javascript:", "on", "<image", "<foreignObject"} {
		if unsafe == "on" {
			assert.NotRegexp(t, `\son[a-z]+=`, meta.Icon, "no event handler attributes")
			continue
		}
		assert.NotContains(t, meta.Icon, unsafe)
	}

	var configKeys []string
	for _, f := range meta.ConfigFields {
		configKeys = append(configKeys, f.Key)
		assert.True(t, f.Disabled, "%s is read-only", f.Key)
		assert.False(t, f.Sensitive, f.Key)
	}
	assert.Equal(t, []string{"url", "subscription_id", "principal", "event", "arguments", "schema_hash"}, configKeys)

	sensitive := map[string]bool{}
	for _, f := range meta.CredentialFields {
		sensitive[f.Key] = f.Sensitive
		assert.True(t, f.Disabled, "%s is read-only", f.Key)
	}
	assert.Equal(t, map[string]bool{"secret": true, "previous_secret": true, "previous_secret_invalid_at": false}, sensitive)
}

func TestInstructions(t *testing.T) {
	t.Parallel()
	p := newProvider(t)
	tmpl := p.Metadata().Instructions
	require.Contains(t, tmpl, "{{", "the metadata holds the template; the API renders it")

	t.Run("with server URL and topics", func(t *testing.T) {
		t.Parallel()
		got, err := destmcp.RenderInstructions(tmpl, destmcp.InstructionsData{
			ServerURL: "https://mcp.example.com/mcp",
			Topics:    []string{"order.created", "order.shipped"},
		})
		require.NoError(t, err)
		assert.Equal(t, "# MCP Events\n"+
			"\n"+
			"AI agents subscribe to events through the MCP server at `https://mcp.example.com/mcp`, not from a form. Each subscription delivers the events the agent asked for to its MCP client, such as ChatGPT.\n"+
			"\n"+
			"## Subscribe an agent\n"+
			"\n"+
			"1. Connect `https://mcp.example.com/mcp` in ChatGPT or another MCP client that supports MCP Events.\n"+
			"2. Ask your agent to subscribe to an event, for example `order.created`.\n"+
			"\n"+
			"Events agents can subscribe to:\n"+
			"\n"+
			"- `order.created`\n"+
			"- `order.shipped`\n"+
			"\n"+
			"## Manage subscriptions\n"+
			"\n"+
			"Each subscription shows up as a destination once an agent subscribes. Agents own their subscriptions, so they can't be edited here: disconnect one to stop its deliveries.\n", got)
	})

	t.Run("without server URL or topics", func(t *testing.T) {
		t.Parallel()
		got, err := destmcp.RenderInstructions(tmpl, destmcp.InstructionsData{})
		require.NoError(t, err)
		assert.Equal(t, "# MCP Events\n"+
			"\n"+
			"AI agents subscribe to events through our MCP server, not from a form. Each subscription delivers the events the agent asked for to its MCP client, such as ChatGPT.\n"+
			"\n"+
			"## Subscribe an agent\n"+
			"\n"+
			"1. Connect our MCP server in ChatGPT or another MCP client that supports MCP Events.\n"+
			"2. Ask your agent to subscribe to an event.\n"+
			"\n"+
			"No events are available to agents yet.\n"+
			"\n"+
			"## Manage subscriptions\n"+
			"\n"+
			"Each subscription shows up as a destination once an agent subscribes. Agents own their subscriptions, so they can't be edited here: disconnect one to stop its deliveries.\n", got)
		assert.NotContains(t, got, "``", "no empty code spans")
		assert.NotContains(t, got, "{{")
	})

	t.Run("values can't break the markup", func(t *testing.T) {
		t.Parallel()
		got, err := destmcp.RenderInstructions(tmpl, destmcp.InstructionsData{
			ServerURL: "https://mcp.example.com/`x`\n\n# injected",
			Topics:    []string{"a`b", "``c"},
		})
		require.NoError(t, err)
		assert.Contains(t, got, "``https://mcp.example.com/`x`  # injected``")
		assert.Contains(t, got, "- ``a`b``\n")
		assert.Contains(t, got, "- ``` ``c ```\n")
		assert.NotContains(t, got, "\n# injected")
	})

	t.Run("provider renders its catalog topics", func(t *testing.T) {
		t.Parallel()
		got, err := p.Instructions("https://mcp.example.com/mcp")
		require.NoError(t, err)
		assert.Contains(t, got, "- `order.created`\n- `order.shipped`\n")
		assert.NotContains(t, got, topicAudit, "only MCP-enabled topics")
	})
}

func TestCodeSpan(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"":          "",
		"plain":     "`plain`",
		"a`b":       "``a`b``",
		"a``b":      "```a``b```",
		"`edge":     "`` `edge ``",
		"edge`":     "`` edge` ``",
		"line\nend": "`line end`",
		"tab\there": "`tab here`",
		"ünïcode":   "`ünïcode`",
	}
	for in, want := range tests {
		assert.Equal(t, want, destmcp.CodeSpan(in), "%q", in)
	}
}

// An instructions override that isn't a valid template fails at startup,
// not when a client asks for it.
func TestInstructions_InvalidOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "mcp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp", "instructions.md"), []byte("Connect {{.ServerURL"), 0o644))
	_, err := destmcp.New(metadata.NewMetadataLoader(dir), destmcp.Config{})
	assert.ErrorContains(t, err, "mcp instructions template")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp", "instructions.md"), []byte("Connect {{.ServerURL}} for {{range .Topics}}{{.}} {{end}}"), 0o644))
	p, err := destmcp.New(metadata.NewMetadataLoader(dir), destmcp.Config{Catalog: newCatalog(t)})
	require.NoError(t, err)
	got, err := p.Instructions("https://mcp.example.com")
	require.NoError(t, err)
	assert.Equal(t, "Connect `https://mcp.example.com` for `order.created` `order.shipped` ", got)
}
