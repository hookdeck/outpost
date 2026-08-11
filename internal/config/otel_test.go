package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseOTelConfig(t *testing.T, envVars map[string]string) *config.OpenTelemetryConfig {
	t.Helper()
	if _, ok := envVars["OTEL_SERVICE_NAME"]; !ok {
		envVars["OTEL_SERVICE_NAME"] = "outpost"
	}
	cfg, err := config.ParseWithoutValidation(config.Flags{}, &mockOS{
		files:   make(map[string][]byte),
		envVars: envVars,
	})
	require.NoError(t, err)
	return &cfg.OpenTelemetry
}

func TestOTelExporterPerSignal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		envVars               map[string]string
		traces, metrics, logs string
	}{
		{
			name:    "no exporter config leaves every signal enabled",
			envVars: map[string]string{},
		},
		{
			name:    "OTEL_EXPORTER applies to every signal",
			envVars: map[string]string{"OTEL_EXPORTER": "console"},
			traces:  "console", metrics: "console", logs: "console",
		},
		{
			name: "per-signal exporter overrides OTEL_EXPORTER",
			envVars: map[string]string{
				"OTEL_EXPORTER":        "otlp",
				"OTEL_LOGS_EXPORTER":   "none",
				"OTEL_TRACES_EXPORTER": "console",
			},
			traces: "console", metrics: "otlp", logs: "none",
		},
		{
			name: "per-signal endpoint enables that signal and disables the rest",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://collector",
			},
			traces: "none", metrics: "", logs: "none",
		},
		{
			name: "generic endpoint enables every signal",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT":         "https://collector",
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://metrics-collector",
			},
		},
		{
			name: "per-signal exporter cannot enable a signal gated off by endpoints",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://collector",
				"OTEL_TRACES_EXPORTER":                "otlp",
			},
			traces: "none", metrics: "", logs: "none",
		},
		{
			name: "OTEL_EXPORTER cannot enable signals gated off by endpoints",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://collector",
				"OTEL_EXPORTER":                       "otlp",
			},
			traces: "none", metrics: "otlp", logs: "none",
		},
		{
			name: "exporter none still disables a signal enabled by the generic endpoint",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector",
				"OTEL_LOGS_EXPORTER":          "none",
			},
			traces: "", metrics: "", logs: "none",
		},
		{
			name: "an endpoint never forces a signal on over exporter none",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://collector",
				"OTEL_METRICS_EXPORTER":               "none",
			},
			traces: "none", metrics: "none", logs: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			otelCfg := parseOTelConfig(t, tt.envVars)
			assert.Equal(t, tt.traces, otelCfg.Traces.Exporter, "traces")
			assert.Equal(t, tt.metrics, otelCfg.Metrics.Exporter, "metrics")
			assert.Equal(t, tt.logs, otelCfg.Logs.Exporter, "logs")
		})
	}
}

func TestOTelProtocolPerSignal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		envVars               map[string]string
		traces, metrics, logs string
	}{
		{
			name:    "defaults to grpc",
			envVars: map[string]string{},
			traces:  "grpc", metrics: "grpc", logs: "grpc",
		},
		{
			name:    "OTEL_PROTOCOL applies to every signal",
			envVars: map[string]string{"OTEL_PROTOCOL": "http"},
			traces:  "http", metrics: "http", logs: "http",
		},
		{
			name:    "standard generic protocol overrides OTEL_PROTOCOL",
			envVars: map[string]string{"OTEL_PROTOCOL": "grpc", "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf"},
			traces:  "http", metrics: "http", logs: "http",
		},
		{
			name: "standard per-signal protocol wins",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_PROTOCOL":      "http/protobuf",
				"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL": "grpc",
			},
			traces: "http", metrics: "http", logs: "grpc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			otelCfg := parseOTelConfig(t, tt.envVars).ToConfig()
			require.NotNil(t, otelCfg)
			assert.Equal(t, tt.traces, otelCfg.Traces.Protocol, "traces")
			assert.Equal(t, tt.metrics, otelCfg.Metrics.Protocol, "metrics")
			assert.Equal(t, tt.logs, otelCfg.Logs.Protocol, "logs")
		})
	}
}

func TestOTelValidateProtocol(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{"", "grpc", "http", "http/protobuf"} {
		otelCfg := config.OpenTelemetryConfig{
			ServiceName: "outpost",
			Traces:      config.OpenTelemetryTypeConfig{Protocol: protocol},
		}
		assert.NoError(t, otelCfg.Validate(), protocol)
	}

	otelCfg := config.OpenTelemetryConfig{
		ServiceName: "outpost",
		Logs:        config.OpenTelemetryTypeConfig{Protocol: "http/json"},
	}
	assert.ErrorIs(t, otelCfg.Validate(), config.ErrInvalidOTelProtocol)
}

func TestOTelValidateExporter(t *testing.T) {
	t.Parallel()

	for _, exporter := range []string{"", "otlp", "console", "stdout", "none"} {
		otelCfg := config.OpenTelemetryConfig{
			ServiceName: "outpost",
			Traces:      config.OpenTelemetryTypeConfig{Exporter: exporter},
		}
		assert.NoError(t, otelCfg.Validate(), exporter)
	}

	tests := []struct {
		name string
		cfg  config.OpenTelemetryConfig
	}{
		{
			name: "typoed traces exporter",
			cfg: config.OpenTelemetryConfig{
				ServiceName: "outpost",
				Traces:      config.OpenTelemetryTypeConfig{Exporter: "otpl"},
			},
		},
		{
			name: "invalid metrics exporter",
			cfg: config.OpenTelemetryConfig{
				ServiceName: "outpost",
				Metrics:     config.OpenTelemetryTypeConfig{Exporter: "prometheus"},
			},
		},
		{
			name: "invalid logs exporter",
			cfg: config.OpenTelemetryConfig{
				ServiceName: "outpost",
				Logs:        config.OpenTelemetryTypeConfig{Exporter: "invalid"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			assert.ErrorIs(t, err, config.ErrInvalidOTelExporter)
			assert.ErrorContains(t, err, "'otlp', 'console', 'stdout' or 'none'")
		})
	}

	t.Run("empty service name skips exporter validation", func(t *testing.T) {
		t.Parallel()
		otelCfg := config.OpenTelemetryConfig{
			Traces: config.OpenTelemetryTypeConfig{Exporter: "otpl"},
		}
		assert.NoError(t, otelCfg.Validate())
	})
}

// Config file values stay in effect; environment variables refine them.
func TestOTelConfigFileWithEnvOverride(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
otel:
  service_name: outpost
  traces:
    exporter: console
    protocol: http
  metrics:
    exporter: otlp
  logs:
    exporter: none
`)
	cfg, err := config.ParseWithoutValidation(config.Flags{Config: "config.yaml"}, &mockOS{
		files:   map[string][]byte{"config.yaml": yaml},
		envVars: map[string]string{"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "grpc"},
	})
	require.NoError(t, err)

	otelCfg := cfg.OpenTelemetry.ToConfig()
	require.NotNil(t, otelCfg)
	assert.Equal(t, "console", otelCfg.Traces.Exporter)
	assert.Equal(t, "grpc", otelCfg.Traces.Protocol)
	assert.Equal(t, "otlp", otelCfg.Metrics.Exporter)
	assert.Equal(t, "none", otelCfg.Logs.Exporter)
}

// Endpoint gating applies to YAML-configured exporters too: a per-signal
// endpoint in the environment disables the signals without one, regardless of
// what the config file asks for.
func TestOTelEndpointGatingOverridesConfigFile(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
otel:
  service_name: outpost
  traces:
    exporter: otlp
`)
	cfg, err := config.ParseWithoutValidation(config.Flags{Config: "config.yaml"}, &mockOS{
		files:   map[string][]byte{"config.yaml": yaml},
		envVars: map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://collector"},
	})
	require.NoError(t, err)

	assert.Equal(t, "none", cfg.OpenTelemetry.Traces.Exporter)
	assert.Equal(t, "", cfg.OpenTelemetry.Metrics.Exporter)
	assert.Equal(t, "none", cfg.OpenTelemetry.Logs.Exporter)
}
