package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestAlertConfig_ToConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.AlertConfig
		want    alert.Settings
		wantErr bool
	}{
		{
			name: "unset uses defaults",
			cfg:  config.AlertConfig{},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: true, Count: 100},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: true, WindowSeconds: 3600},
			},
		},
		{
			name: "empty string disables both dimensions",
			cfg: config.AlertConfig{
				ConsecutiveFailureCount:       strPtr(""),
				ExhaustedRetriesWindowSeconds: strPtr(""),
			},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: false, Count: 0},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: false, WindowSeconds: 0},
			},
		},
		{
			name: "explicit values",
			cfg: config.AlertConfig{
				ConsecutiveFailureCount:       strPtr("50"),
				ExhaustedRetriesWindowSeconds: strPtr("120"),
			},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: true, Count: 50},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: true, WindowSeconds: 120},
			},
		},
		{
			name: "surrounding whitespace is trimmed",
			cfg: config.AlertConfig{
				ConsecutiveFailureCount: strPtr("  50  "),
			},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: true, Count: 50},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: true, WindowSeconds: 3600},
			},
		},
		{
			name: "exhausted window zero means enabled with no suppression",
			cfg:  config.AlertConfig{ExhaustedRetriesWindowSeconds: strPtr("0")},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: true, Count: 100},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: true, WindowSeconds: 0},
			},
		},
		{
			name: "auto_disable_destination is carried through",
			cfg:  config.AlertConfig{AutoDisableDestination: true},
			want: alert.Settings{
				ConsecutiveFailure:     alert.ConsecutiveFailureSetting{Enabled: true, Count: 100},
				ExhaustedRetries:       alert.ExhaustedRetriesSetting{Enabled: true, WindowSeconds: 3600},
				AutoDisableDestination: true,
			},
		},
		{
			name:    "consecutive zero is invalid (min 1)",
			cfg:     config.AlertConfig{ConsecutiveFailureCount: strPtr("0")},
			wantErr: true,
		},
		{
			name:    "consecutive negative is invalid",
			cfg:     config.AlertConfig{ConsecutiveFailureCount: strPtr("-1")},
			wantErr: true,
		},
		{
			name:    "consecutive non-numeric is invalid",
			cfg:     config.AlertConfig{ConsecutiveFailureCount: strPtr("abc")},
			wantErr: true,
		},
		{
			name:    "exhausted negative is invalid",
			cfg:     config.AlertConfig{ExhaustedRetriesWindowSeconds: strPtr("-5")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.cfg.ToConfig()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
