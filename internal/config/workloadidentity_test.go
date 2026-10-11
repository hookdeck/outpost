package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkloadIdentityConfig(t *testing.T) {
	const issuer = "https://outpost.example.com/workload-identity"
	key := testutil.SigningKeyPEM(t)
	keyFile := filepath.Join(t.TempDir(), "key.pem")
	require.NoError(t, os.WriteFile(keyFile, []byte(key), 0o600))

	tests := []struct {
		name        string
		wi          config.WorkloadIdentityConfig
		wantEnabled bool
		wantErr     string
	}{
		{name: "not configured", wi: config.WorkloadIdentityConfig{}},
		{name: "key value", wi: config.WorkloadIdentityConfig{Issuer: issuer, SigningKey: key}, wantEnabled: true},
		{name: "key file", wi: config.WorkloadIdentityConfig{Issuer: issuer, SigningKeyFile: keyFile}, wantEnabled: true},
		{name: "verification keys file", wi: config.WorkloadIdentityConfig{Issuer: issuer, SigningKey: key, VerificationKeysFile: keyFile}, wantEnabled: true},
		{name: "issuer without key", wi: config.WorkloadIdentityConfig{Issuer: issuer}, wantErr: "signing key"},
		{name: "key without issuer", wi: config.WorkloadIdentityConfig{SigningKey: key}, wantErr: "issuer is required"},
		{name: "only verification keys", wi: config.WorkloadIdentityConfig{VerificationKeys: key}, wantErr: "issuer is required"},
		{name: "value and file", wi: config.WorkloadIdentityConfig{Issuer: issuer, SigningKey: key, SigningKeyFile: keyFile}, wantErr: "not both"},
		{name: "missing file", wi: config.WorkloadIdentityConfig{Issuer: issuer, SigningKeyFile: keyFile + ".missing"}, wantErr: "signing_key_file"},
		{name: "http issuer", wi: config.WorkloadIdentityConfig{Issuer: "http://outpost.example.com", SigningKey: key}, wantErr: "https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.WorkloadIdentity = tt.wi
			err := c.Validate(config.Flags{})
			if tt.wantErr != "" {
				require.ErrorIs(t, err, config.ErrInvalidWorkloadIdentity)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			issuerObj, err := c.WorkloadIdentity.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnabled, issuerObj != nil)
		})
	}
}

func TestWorkloadIdentityConfig_Env(t *testing.T) {
	key := testutil.SigningKeyPEM(t)
	m := &mockOS{files: map[string][]byte{}, envVars: map[string]string{
		"WORKLOAD_IDENTITY_ISSUER":      "https://outpost.example.com/workload-identity",
		"WORKLOAD_IDENTITY_SIGNING_KEY": strings.ReplaceAll(key, "\n", `\n`),
	}}
	cfg, err := config.ParseWithoutValidation(config.Flags{}, m)
	require.NoError(t, err)
	issuer, err := cfg.WorkloadIdentity.Build()
	require.NoError(t, err)
	require.NotNil(t, issuer)
	assert.Equal(t, "https://outpost.example.com/workload-identity", issuer.URL())
}

func TestWorkloadIdentityConfig_YAML(t *testing.T) {
	key := testutil.SigningKeyPEM(t)
	indented := "    " + strings.ReplaceAll(strings.TrimSpace(key), "\n", "\n    ")
	m := &mockOS{files: map[string][]byte{"/c.yaml": []byte(
		"workload_identity:\n  issuer: https://outpost.example.com/workload-identity\n  signing_key: |\n" + indented + "\n",
	)}, envVars: map[string]string{}}
	cfg, err := config.ParseWithoutValidation(config.Flags{Config: "/c.yaml"}, m)
	require.NoError(t, err)
	issuer, err := cfg.WorkloadIdentity.Build()
	require.NoError(t, err)
	require.NotNil(t, issuer)
}
