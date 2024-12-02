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

	t.Run("loads from embedded files when basePath is empty", func(t *testing.T) {
		loader := NewMetadataLoader("")
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)
		assert.Equal(t, "webhook", metadata.Type)
	})

	t.Run("loads from filesystem when file exists", func(t *testing.T) {
		providerDir := filepath.Join(tmpDir, "webhook")
		require.NoError(t, os.MkdirAll(providerDir, 0755))

		// Create custom core.json
		writeTestFile(t, filepath.Join(providerDir, "core.json"), `{
			"type": "webhook",
			"config_fields": [{"type": "text", "label": "Custom", "key": "custom", "required": true}],
			"credential_fields": []
		}`)

		loader := NewMetadataLoader(tmpDir)
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)
		assert.Equal(t, "Custom", metadata.ConfigFields[0].Label)
	})

	t.Run("falls back to embedded when file doesn't exist in filesystem", func(t *testing.T) {
		// Create directory but don't create any files
		providerDir := filepath.Join(tmpDir, "webhook")
		require.NoError(t, os.MkdirAll(providerDir, 0755))

		loader := NewMetadataLoader(tmpDir)
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)
		assert.Equal(t, "webhook", metadata.Type) // Should get this from embedded
	})

	t.Run("returns error when provider doesn't exist anywhere", func(t *testing.T) {
		loader := NewMetadataLoader(tmpDir)
		_, err := loader.Load("nonexistent")
		assert.Error(t, err)
	})

	t.Run("loads mixed sources (filesystem + embedded)", func(t *testing.T) {
		providerDir := filepath.Join(tmpDir, "webhook")
		require.NoError(t, os.MkdirAll(providerDir, 0755))

		// Only override instructions.md
		customInstructions := "# Custom Instructions"
		writeTestFile(t, filepath.Join(providerDir, "instructions.md"), customInstructions)

		loader := NewMetadataLoader(tmpDir)
		metadata, err := loader.Load("webhook")
		require.NoError(t, err)

		// Should use custom instructions
		assert.Equal(t, customInstructions, metadata.Instructions)
		// But embedded core.json
		assert.Equal(t, "webhook", metadata.Type)
	})
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
