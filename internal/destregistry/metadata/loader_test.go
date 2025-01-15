package metadata

import (
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

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
