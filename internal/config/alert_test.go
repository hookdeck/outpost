package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAlertConfig_ParsePaths verifies the three-state contract for an alert
// setting — unset means "use the default", empty means "disabled", and a value
// means "use that value" — and that it holds identically whether the setting
// comes from the YAML config file or an environment variable.
//
// It runs the full parse path (file/env -> config -> resolved settings) so that
// the two surfaces are exercised exactly as a deployment would configure them.
// The empty-string-disables case in particular must behave the same on both.
func TestAlertConfig_ParsePaths(t *testing.T) {
	parseYAML := func(t *testing.T, yamlBody string) alert.Settings {
		t.Helper()
		m := &mockOS{
			files:   map[string][]byte{"/c.yaml": []byte(yamlBody)},
			envVars: map[string]string{},
		}
		cfg, err := config.ParseWithoutValidation(config.Flags{Config: "/c.yaml"}, m)
		require.NoError(t, err)
		s, err := cfg.Alert.ToConfig()
		require.NoError(t, err)
		return s
	}
	parseEnv := func(t *testing.T, env map[string]string) alert.Settings {
		t.Helper()
		m := &mockOS{files: map[string][]byte{}, envVars: env}
		cfg, err := config.ParseWithoutValidation(config.Flags{}, m)
		require.NoError(t, err)
		s, err := cfg.Alert.ToConfig()
		require.NoError(t, err)
		return s
	}
	parseBoth := func(t *testing.T, yamlBody string, env map[string]string) alert.Settings {
		t.Helper()
		m := &mockOS{files: map[string][]byte{"/c.yaml": []byte(yamlBody)}, envVars: env}
		cfg, err := config.ParseWithoutValidation(config.Flags{Config: "/c.yaml"}, m)
		require.NoError(t, err)
		s, err := cfg.Alert.ToConfig()
		require.NoError(t, err)
		return s
	}
	cf := func(s alert.Settings) alert.ConsecutiveFailureSetting { return s.ConsecutiveFailure }

	t.Run("yaml: alert key absent -> default", func(t *testing.T) {
		got := cf(parseYAML(t, "log_level: debug\n"))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: true, Count: 100}, got)
	})
	t.Run("yaml: set to empty string -> disabled", func(t *testing.T) {
		got := cf(parseYAML(t, "alert:\n  consecutive_failure_count: \"\"\n"))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: false, Count: 0}, got)
	})
	t.Run("yaml: key present but no value -> default", func(t *testing.T) {
		// `key:` with nothing after it is not the same as an empty string; it
		// reads as "not provided", so it falls back to the default rather than
		// disabling. Only an explicit empty string disables.
		got := cf(parseYAML(t, "alert:\n  consecutive_failure_count:\n"))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: true, Count: 100}, got)
	})
	t.Run("yaml: value -> value", func(t *testing.T) {
		got := cf(parseYAML(t, "alert:\n  consecutive_failure_count: \"5\"\n"))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: true, Count: 5}, got)
	})

	t.Run("env: var absent -> default", func(t *testing.T) {
		got := cf(parseEnv(t, map[string]string{}))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: true, Count: 100}, got)
	})
	t.Run("env: set to empty string -> disabled", func(t *testing.T) {
		// Setting the env var to an empty string must disable the dimension, the
		// same way `key: ""` does in YAML — an empty env var is a deliberate
		// "off", distinct from leaving the var unset (which uses the default).
		got := cf(parseEnv(t, map[string]string{"ALERT_CONSECUTIVE_FAILURE_COUNT": ""}))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: false, Count: 0}, got)
	})
	t.Run("env: value -> value", func(t *testing.T) {
		got := cf(parseEnv(t, map[string]string{"ALERT_CONSECUTIVE_FAILURE_COUNT": "5"}))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: true, Count: 5}, got)
	})

	// When a setting is provided in both places, the env var wins over YAML.
	t.Run("both: env value overrides yaml value", func(t *testing.T) {
		got := cf(parseBoth(t,
			"alert:\n  consecutive_failure_count: \"5\"\n",
			map[string]string{"ALERT_CONSECUTIVE_FAILURE_COUNT": "9"}))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: true, Count: 9}, got)
	})
	t.Run("both: empty env overrides yaml value (disables)", func(t *testing.T) {
		got := cf(parseBoth(t,
			"alert:\n  consecutive_failure_count: \"5\"\n",
			map[string]string{"ALERT_CONSECUTIVE_FAILURE_COUNT": ""}))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: false, Count: 0}, got)
	})
	t.Run("both: env value overrides yaml disable", func(t *testing.T) {
		got := cf(parseBoth(t,
			"alert:\n  consecutive_failure_count: \"\"\n",
			map[string]string{"ALERT_CONSECUTIVE_FAILURE_COUNT": "9"}))
		assert.Equal(t, alert.ConsecutiveFailureSetting{Enabled: true, Count: 9}, got)
	})
}

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
				ConsecutiveFailureCount:       config.NewOptionalString(""),
				ExhaustedRetriesWindowSeconds: config.NewOptionalString(""),
			},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: false, Count: 0},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: false, WindowSeconds: 0},
			},
		},
		{
			name: "explicit values",
			cfg: config.AlertConfig{
				ConsecutiveFailureCount:       config.NewOptionalString("50"),
				ExhaustedRetriesWindowSeconds: config.NewOptionalString("120"),
			},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: true, Count: 50},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: true, WindowSeconds: 120},
			},
		},
		{
			name: "surrounding whitespace is trimmed",
			cfg: config.AlertConfig{
				ConsecutiveFailureCount: config.NewOptionalString("  50  "),
			},
			want: alert.Settings{
				ConsecutiveFailure: alert.ConsecutiveFailureSetting{Enabled: true, Count: 50},
				ExhaustedRetries:   alert.ExhaustedRetriesSetting{Enabled: true, WindowSeconds: 3600},
			},
		},
		{
			name: "exhausted window zero means enabled with no suppression",
			cfg:  config.AlertConfig{ExhaustedRetriesWindowSeconds: config.NewOptionalString("0")},
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
			cfg:     config.AlertConfig{ConsecutiveFailureCount: config.NewOptionalString("0")},
			wantErr: true,
		},
		{
			name:    "consecutive negative is invalid",
			cfg:     config.AlertConfig{ConsecutiveFailureCount: config.NewOptionalString("-1")},
			wantErr: true,
		},
		{
			name:    "consecutive non-numeric is invalid",
			cfg:     config.AlertConfig{ConsecutiveFailureCount: config.NewOptionalString("abc")},
			wantErr: true,
		},
		{
			name:    "exhausted negative is invalid",
			cfg:     config.AlertConfig{ExhaustedRetriesWindowSeconds: config.NewOptionalString("-5")},
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
