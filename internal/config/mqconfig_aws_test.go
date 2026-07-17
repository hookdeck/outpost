package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
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
