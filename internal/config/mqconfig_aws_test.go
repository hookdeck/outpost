package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSSQSConfig_IsConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.AWSSQSConfig
		want bool
	}{
		{
			name: "region only (IAM role via default credential chain)",
			cfg:  config.AWSSQSConfig{Region: "us-east-1"},
			want: true,
		},
		{
			name: "region with static keys",
			cfg:  config.AWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET", Region: "us-east-1"},
			want: true,
		},
		{
			name: "keys without region",
			cfg:  config.AWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET"},
			want: false,
		},
		{
			name: "empty",
			cfg:  config.AWSSQSConfig{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.cfg.IsConfigured())
		})
	}
}

func TestAWSSQSConfig_ToQueueConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.AWSSQSConfig
		wantErr bool
	}{
		{
			name: "both keys set (static credentials)",
			cfg:  config.AWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET", Region: "us-east-1"},
		},
		{
			name: "both keys empty (default credential chain)",
			cfg:  config.AWSSQSConfig{Region: "us-east-1"},
		},
		{
			name:    "only access key set",
			cfg:     config.AWSSQSConfig{AccessKeyID: "AKID", Region: "us-east-1"},
			wantErr: true,
		},
		{
			name:    "only secret key set",
			cfg:     config.AWSSQSConfig{SecretAccessKey: "SECRET", Region: "us-east-1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := tt.cfg.ToQueueConfig(t.Context(), "deliverymq")
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, cfg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cfg)
		})
	}
}
