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
			name: "explicit exporter wins over endpoint inference",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://collector",
				"OTEL_TRACES_EXPORTER":                "otlp",
			},
			traces: "otlp", metrics: "", logs: "none",
		},
		{
			name: "OTEL_EXPORTER wins over endpoint inference",
			envVars: map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://collector",
				"OTEL_EXPORTER":                       "otlp",
			},
			traces: "otlp", metrics: "otlp", logs: "otlp",
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
