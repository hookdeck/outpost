package config

import (
	"fmt"

	"github.com/hookdeck/outpost/internal/otel"
	v "github.com/spf13/viper"
)

func getProtocol(viper *v.Viper, telemetryType string) string {
	// Check type-specific protocol first
	protocol := viper.GetString(fmt.Sprintf("OTEL_EXPORTER_OTLP_%s_PROTOCOL", telemetryType))
	if protocol == "" {
		// Fall back to generic protocol
		protocol = viper.GetString("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	if protocol == "" {
		// Default to gRPC if not specified
		protocol = "grpc"
	}
	return protocol
}

// If the user has set OTEL_SERVICE_NAME, we assume they are managing their own OpenTelemetry configuration.
// The SDK will automatically read the environment variables for configuration.
func parseOpenTelemetryConfig(viper *v.Viper) (*otel.OpenTelemetryConfig, error) {
	if viper.GetString("OTEL_SERVICE_NAME") == "" {
		return nil, nil
	}

	return &otel.OpenTelemetryConfig{
		Traces: &otel.OpenTelemetryTypeConfig{
			Exporter: viper.GetString("OTEL_TRACES_EXPORTER"),
			Protocol: getProtocol(viper, "TRACES"),
		},
		Metrics: &otel.OpenTelemetryTypeConfig{
			Exporter: viper.GetString("OTEL_METRICS_EXPORTER"),
			Protocol: getProtocol(viper, "METRICS"),
		},
		Logs: &otel.OpenTelemetryTypeConfig{
			Exporter: viper.GetString("OTEL_LOGS_EXPORTER"),
			Protocol: getProtocol(viper, "LOGS"),
		},
	}, nil
}
