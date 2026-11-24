package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDestinationsAllowCustomEndpoints(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: map[string]string{}})
		require.NoError(t, err)
		assert.False(t, cfg.Destinations.AllowCustomEndpoints)
		assert.False(t, cfg.Destinations.ToConfig(cfg).AllowCustomEndpoints)
	})

	t.Run("env", func(t *testing.T) {
		cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: map[string]string{
			"DESTINATIONS_ALLOW_CUSTOM_ENDPOINTS": "true",
		}})
		require.NoError(t, err)
		assert.True(t, cfg.Destinations.AllowCustomEndpoints)
		assert.True(t, cfg.Destinations.ToConfig(cfg).AllowCustomEndpoints)
	})

	t.Run("yaml", func(t *testing.T) {
		cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{
			files:   map[string][]byte{"config.yaml": []byte("destinations:\n  allow_custom_endpoints: true\n")},
			envVars: map[string]string{"CONFIG": "config.yaml"},
		})
		require.NoError(t, err)
		assert.True(t, cfg.Destinations.ToConfig(cfg).AllowCustomEndpoints)
	})
}
