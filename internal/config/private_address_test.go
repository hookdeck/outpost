package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDestinationsPrivateAddresses(t *testing.T) {
	t.Run("off and empty by default", func(t *testing.T) {
		cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: map[string]string{}})
		require.NoError(t, err)
		opts := cfg.Destinations.ToConfig(cfg)
		assert.False(t, opts.Webhook.BlockPrivateAddresses)
		assert.Empty(t, opts.PrivateAddressAllowlist)
	})

	t.Run("env", func(t *testing.T) {
		for _, mode := range []string{"default", "standard"} {
			cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: map[string]string{
				"DESTINATIONS_WEBHOOK_MODE":                    mode,
				"DESTINATIONS_WEBHOOK_BLOCK_PRIVATE_ADDRESSES": "true",
				"DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST":       "10.0.0.0/8,localhost",
			}})
			require.NoError(t, err, mode)
			opts := cfg.Destinations.ToConfig(cfg)
			assert.True(t, opts.Webhook.BlockPrivateAddresses, mode)
			assert.Equal(t, []string{"10.0.0.0/8", "localhost"}, opts.PrivateAddressAllowlist, mode)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{
			files: map[string][]byte{"config.yaml": []byte(`
destinations:
  private_address_allowlist:
    - 10.0.0.0/8
    - 192.168.1.5
  webhook:
    block_private_addresses: true
`)},
			envVars: map[string]string{"CONFIG": "config.yaml"},
		})
		require.NoError(t, err)
		opts := cfg.Destinations.ToConfig(cfg)
		assert.True(t, opts.Webhook.BlockPrivateAddresses)
		assert.Equal(t, []string{"10.0.0.0/8", "192.168.1.5"}, opts.PrivateAddressAllowlist)
	})
}

func TestValidate_PrivateAddressAllowlist(t *testing.T) {
	for name, tc := range map[string]struct {
		entries []string
		block   bool
		wantErr bool
	}{
		"empty":                {},
		"valid":                {entries: []string{"10.0.0.0/8", "127.0.0.1", "localhost", "fd00::/8"}},
		"host name":            {entries: []string{"internal.example"}, wantErr: true},
		"whole internet":       {entries: []string{"0.0.0.0/0"}, wantErr: true},
		"host bits":            {entries: []string{"10.0.0.1/8"}, wantErr: true},
		"bad entry, block on":  {entries: []string{"nope"}, block: true, wantErr: true},
		"bad entry, block off": {entries: []string{"nope"}, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			c.Destinations.PrivateAddressAllowlist = tc.entries
			c.Destinations.Webhook.BlockPrivateAddresses = tc.block
			err := c.Validate(config.Flags{})
			if tc.wantErr {
				assert.ErrorIs(t, err, config.ErrInvalidPrivateAddressAllowlist)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
