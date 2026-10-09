package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataLoader(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("loads from embedded metadata.json", func(t *testing.T) {
		loader := NewMetadataLoader("")
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)
		assert.Equal(t, "webhook", metadata.Type)
	})

	t.Run("merges filesystem metadata.json", func(t *testing.T) {
		providerDir := filepath.Join(tmpDir, "webhook")
		require.NoError(t, os.MkdirAll(providerDir, 0755))

		writeTestFile(t, filepath.Join(providerDir, "metadata.json"), `{
			"label": "Custom Label",
			"description": "Custom Description",
			"icon": "custom-icon"
		}`)

		loader := NewMetadataLoader(tmpDir)
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)

		// UI fields should be overridden
		assert.Equal(t, "Custom Label", metadata.Label)
		assert.Equal(t, "Custom Description", metadata.Description)
		assert.Equal(t, "custom-icon", metadata.Icon)

		// Core fields should be preserved
		assert.Equal(t, "webhook", metadata.Type)
		assert.NotEmpty(t, metadata.ConfigFields)
	})

	t.Run("preserves core fields during merge", func(t *testing.T) {
		providerDir := filepath.Join(tmpDir, "webhook")
		require.NoError(t, os.MkdirAll(providerDir, 0755))

		writeTestFile(t, filepath.Join(providerDir, "metadata.json"), `{
			"type": "different-type",
			"config_fields": [],
			"credential_fields": [],
			"label": "Custom Label"
		}`)

		loader := NewMetadataLoader(tmpDir)
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)

		// Core fields should not be overridden
		assert.Equal(t, "webhook", metadata.Type)
		assert.NotEmpty(t, metadata.ConfigFields)

		// UI fields should be overridden
		assert.Equal(t, "Custom Label", metadata.Label)
	})

	t.Run("loads instructions.md separately", func(t *testing.T) {
		providerDir := filepath.Join(tmpDir, "webhook")
		require.NoError(t, os.MkdirAll(providerDir, 0755))

		customInstructions := "# Custom Instructions"
		writeTestFile(t, filepath.Join(providerDir, "instructions.md"), customInstructions)
		writeTestFile(t, filepath.Join(providerDir, "metadata.json"), `{
			"label": "Custom Label"
		}`)

		loader := NewMetadataLoader(tmpDir)
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)

		assert.Equal(t, customInstructions, metadata.Instructions)
		assert.Equal(t, "Custom Label", metadata.Label)
		assert.Equal(t, "webhook", metadata.Type)
	})

	t.Run("returns error when provider doesn't exist", func(t *testing.T) {
		loader := NewMetadataLoader(tmpDir)
		_, err := loader.Load("nonexistent")
		assert.Error(t, err)
	})
}

func TestMetadataLoader_MCP(t *testing.T) {
	t.Run("loads the embedded mcp metadata", func(t *testing.T) {
		metadata, err := NewMetadataLoader("").Load("mcp")
		require.NoError(t, err)
		assert.Equal(t, "mcp", metadata.Type)
		assert.Equal(t, CreateModeExternal, metadata.CreateMode)
		assert.Equal(t, "MCP Events", metadata.Label)
		assert.NotEmpty(t, metadata.Icon)
		assert.NotEmpty(t, metadata.Instructions)
		assert.Len(t, metadata.ConfigFields, 6)
		assert.Len(t, metadata.CredentialFields, 3)
	})

	t.Run("create_mode can't be overridden", func(t *testing.T) {
		tmpDir := t.TempDir()
		for provider, override := range map[string]string{
			"mcp":     `{"create_mode": "form", "label": "Agents"}`,
			"webhook": `{"create_mode": "external", "label": "Hooks"}`,
		} {
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, provider), 0755))
			writeTestFile(t, filepath.Join(tmpDir, provider, "metadata.json"), override)
		}
		loader := NewMetadataLoader(tmpDir)

		mcp, err := loader.Load("mcp")
		require.NoError(t, err)
		assert.Equal(t, CreateModeExternal, mcp.CreateMode)
		assert.Equal(t, "Agents", mcp.Label, "other fields still merge")

		webhook, err := loader.Load("webhook")
		require.NoError(t, err)
		assert.Empty(t, webhook.CreateMode)
		assert.Equal(t, "Hooks", webhook.Label)
	})

	// API v1 serves provider metadata as is: types without a create mode
	// must encode exactly as before the field existed.
	t.Run("form types encode without create_mode", func(t *testing.T) {
		loader := NewMetadataLoader("")
		for _, provider := range []string{"webhook", "hookdeck", "aws_sqs", "aws_kinesis", "aws_s3", "aws_eventbridge", "azure_servicebus", "gcp_pubsub", "rabbitmq", "kafka", "cloudflare_queues", "webhook_standard"} {
			metadata, err := loader.Load(provider)
			require.NoError(t, err, provider)
			assert.Empty(t, metadata.CreateMode, provider)
			b, err := json.Marshal(metadata)
			require.NoError(t, err)
			assert.NotContains(t, string(b), "create_mode", provider)
		}

		metadata, err := loader.Load("mcp")
		require.NoError(t, err)
		b, err := json.Marshal(metadata)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"create_mode":"external"`)
	})
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
