package metadata

import (
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// embed.FS keys are always slash-separated, independent of the host OS. A key
// built with filepath.Join uses the OS separator, so on Windows it becomes
// `providers\webhook\metadata.json` and every embedded lookup misses — which
// aborts server startup, because the destination registry cannot initialise.
//
// Loading a provider is already covered by TestMetadataLoader, but that only
// fails on Windows. This test pins the invariant that breaks there, so it
// carries meaning on every platform.
func TestEmbeddedKeysUseSlashSeparator(t *testing.T) {
	for _, providerType := range []string{"webhook", "webhook_standard", "hookdeck", "mcp", "aws_sqs", "kafka"} {
		key := path.Join("providers", providerType, "metadata.json")

		bytes, err := defaultFS.ReadFile(key)
		require.NoError(t, err, "embedded key %q must resolve (GOOS=%s)", key, runtime.GOOS)
		assert.NotEmpty(t, bytes, "embedded file %q must not be empty", key)

		if runtime.GOOS == "windows" {
			// The separator filepath.Join produces on Windows is not a key
			// embed.FS can address. Assert the two differ so the regression is
			// described rather than assumed.
			assert.NotEqual(t,
				filepath.Join("providers", providerType, "metadata.json"),
				key,
				"filepath.Join must not be used to build an embed.FS key",
			)
		}
	}
}
