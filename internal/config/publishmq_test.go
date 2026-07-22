package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishMQConfig_GetInfraType_AWSSQS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.PublishAWSSQSConfig
		want string
	}{
		{
			name: "region only (IAM role via default credential chain)",
			cfg:  config.PublishAWSSQSConfig{Region: "us-east-1"},
			want: "awssqs",
		},
		{
			name: "region with static keys",
			cfg:  config.PublishAWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET", Region: "us-east-1"},
			want: "awssqs",
		},
		{
			name: "keys without region",
			cfg:  config.PublishAWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET"},
			want: "",
		},
		{
			name: "empty",
			cfg:  config.PublishAWSSQSConfig{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.PublishMQConfig{AWSSQS: tt.cfg}
			assert.Equal(t, tt.want, cfg.GetInfraType())
		})
	}
}

func TestPublishMQConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.PublishMQConfig
		wantErr bool
	}{
		{
			name: "aws sqs both keys set",
			cfg:  config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET", Region: "us-east-1"}},
		},
		{
			name: "aws sqs both keys empty (default credential chain)",
			cfg:  config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{Region: "us-east-1"}},
		},
		{
			name:    "aws sqs only access key set",
			cfg:     config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{AccessKeyID: "AKID", Region: "us-east-1"}},
			wantErr: true,
		},
		{
			name:    "aws sqs only secret key set",
			cfg:     config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{SecretAccessKey: "SECRET", Region: "us-east-1"}},
			wantErr: true,
		},
		{
			name: "no publish provider configured",
			cfg:  config.PublishMQConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
