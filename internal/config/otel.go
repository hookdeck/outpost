package config

import (
	"github.com/hookdeck/EventKit/internal/otel"
	v "github.com/spf13/viper"
)

// If the user has set OTEL_SERVICE_NAME, we assume they are managing their own OpenTelemetry configuration.
// When parsing config, we assume if the user has set OTEL_EXPORTER_OTLP_ENDPOINT, they will use all 3
// Traces, Metrics, and Logs.
// If the user doesn't want to use all 3, they will have to specify each one individually.
func parseOpenTelemetryConfig(viper *v.Viper) (*otel.OpenTelemetryConfig, error) {
	if viper.GetString("OTEL_SERVICE_NAME") == "" {
		return nil, nil
	}

	defaultEndpoint := viper.GetString("OTEL_EXPORTER_OTLP_ENDPOINT")
	defaultProtocol := viper.GetString("OTEL_EXPORTER_OTLP_PROTOCOL")
	if defaultProtocol == "" {
		defaultProtocol = "grpc"
	}

	tracesConfig, err := parseTracesConfig(viper, defaultEndpoint, defaultProtocol)
	if err != nil {
		return nil, err
	}
	metricsConfig, err := parseMetricsConfig(viper, defaultEndpoint, defaultProtocol)
	if err != nil {
		return nil, err
	}
	logsConfig, err := parseLogsConfig(viper, defaultEndpoint, defaultProtocol)
	if err != nil {
		return nil, err
	}

	return &otel.OpenTelemetryConfig{
		Traces:  tracesConfig,
		Metrics: metricsConfig,
		Logs:    logsConfig,
	}, nil
}

func parseTracesConfig(viper *v.Viper, defaultEndpoint, defaultProtocol string) (*otel.OpenTelemetryTypeConfig, error) {
	endpoint := getTypeSpecificWithDefault(viper, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", defaultEndpoint)
	if endpoint == "" {
		return nil, nil
	}

	protocol, err := otel.OpenTelemetryProtocolFromString(getTypeSpecificWithDefault(viper, "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", defaultProtocol))
	if err != nil {
		return nil, err
	}

	return &otel.OpenTelemetryTypeConfig{
		Endpoint: endpoint,
		Protocol: protocol,
	}, nil
}

func parseMetricsConfig(viper *v.Viper, defaultEndpoint, defaultProtocol string) (*otel.OpenTelemetryTypeConfig, error) {
	endpoint := getTypeSpecificWithDefault(viper, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", defaultEndpoint)
	if endpoint == "" {
		return nil, nil
	}

	protocol, err := otel.OpenTelemetryProtocolFromString(getTypeSpecificWithDefault(viper, "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", defaultProtocol))
	if err != nil {
		return nil, err
	}

	return &otel.OpenTelemetryTypeConfig{
		Endpoint: endpoint,
		Protocol: protocol,
	}, nil
}

func parseLogsConfig(viper *v.Viper, defaultEndpoint, defaultProtocol string) (*otel.OpenTelemetryTypeConfig, error) {
	endpoint := getTypeSpecificWithDefault(viper, "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", defaultEndpoint)
	if endpoint == "" {
		return nil, nil
	}

	protocol, err := otel.OpenTelemetryProtocolFromString(getTypeSpecificWithDefault(viper, "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", defaultProtocol))
	if err != nil {
		return nil, err
	}

	return &otel.OpenTelemetryTypeConfig{
		Endpoint: endpoint,
		Protocol: protocol,
	}, nil
}

func getTypeSpecificWithDefault(viper *v.Viper, otelTypeKey string, defaultValue string) string {
	value := viper.GetString(otelTypeKey)
	if value == "" {
		return defaultValue
	}
	return value
}
