package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type OpenTelemetryProtocol string

const (
	OpenTelemetryProtocolGRPC         OpenTelemetryProtocol = "grpc"
	OpenTelemetryProtocolHTTPProtobuf OpenTelemetryProtocol = "http/protobuf"
	OpenTelemetryProtocolHTTPJSON     OpenTelemetryProtocol = "http/json"
)

type OpenTelemetryTypeConfig struct {
	Endpoint string
	Protocol OpenTelemetryProtocol
}

type OpenTelemetryConfig struct {
	Traces  *OpenTelemetryTypeConfig
	Metrics *OpenTelemetryTypeConfig
	Logs    *OpenTelemetryTypeConfig
}

// If the user has set OTEL_SERVICE_NAME, we assume they are managing their own OpenTelemetry configuration.
// When parsing config, we assume if the user has set OTEL_EXPORTER_OTLP_ENDPOINT, they will use all 3
// Traces, Metrics, and Logs.
// If the user doesn't want to use all 3, they will have to specify each one individually.
func parseOpenTelemetryConfig() (*OpenTelemetryConfig, error) {
	if viper.GetString("OTEL_SERVICE_NAME") == "" {
		return nil, nil
	}

	defaultEndpoint := viper.GetString("OTEL_EXPORTER_OTLP_ENDPOINT")
	defaultProtocol := viper.GetString("OTEL_EXPORTER_OTLP_PROTOCOL")
	if defaultProtocol == "" {
		defaultProtocol = "grpc"
	}

	tracesConfig, err := parseTracesConfig(defaultEndpoint, defaultProtocol)
	if err != nil {
		return nil, err
	}
	metricsConfig, err := parseMetricsConfig(defaultEndpoint, defaultProtocol)
	if err != nil {
		return nil, err
	}
	logsConfig, err := parseLogsConfig(defaultEndpoint, defaultProtocol)
	if err != nil {
		return nil, err
	}

	return &OpenTelemetryConfig{
		Traces:  tracesConfig,
		Metrics: metricsConfig,
		Logs:    logsConfig,
	}, nil
}

func parseTracesConfig(defaultEndpoint, defaultProtocol string) (*OpenTelemetryTypeConfig, error) {
	endpoint := getTypeSpecificWithDefault("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", defaultEndpoint)
	if endpoint == "" {
		return nil, nil
	}

	protocol, err := OpenTelemetryProtocolFromString(getTypeSpecificWithDefault("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", defaultProtocol))
	if err != nil {
		return nil, err
	}

	return &OpenTelemetryTypeConfig{
		Endpoint: endpoint,
		Protocol: protocol,
	}, nil
}

func parseMetricsConfig(defaultEndpoint, defaultProtocol string) (*OpenTelemetryTypeConfig, error) {
	endpoint := getTypeSpecificWithDefault("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", defaultEndpoint)
	if endpoint == "" {
		return nil, nil
	}

	protocol, err := OpenTelemetryProtocolFromString(getTypeSpecificWithDefault("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", defaultProtocol))
	if err != nil {
		return nil, err
	}

	return &OpenTelemetryTypeConfig{
		Endpoint: endpoint,
		Protocol: protocol,
	}, nil
}

func parseLogsConfig(defaultEndpoint, defaultProtocol string) (*OpenTelemetryTypeConfig, error) {
	endpoint := getTypeSpecificWithDefault("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", defaultEndpoint)
	if endpoint == "" {
		return nil, nil
	}

	protocol, err := OpenTelemetryProtocolFromString(getTypeSpecificWithDefault("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", defaultProtocol))
	if err != nil {
		return nil, err
	}

	return &OpenTelemetryTypeConfig{
		Endpoint: endpoint,
		Protocol: protocol,
	}, nil
}

func getTypeSpecificWithDefault(otelTypeKey string, defaultValue string) string {
	value := viper.GetString(otelTypeKey)
	if value == "" {
		return defaultValue
	}
	return value
}

func OpenTelemetryProtocolFromString(s string) (OpenTelemetryProtocol, error) {
	switch s {
	case "grpc":
		return OpenTelemetryProtocolGRPC, nil
	case "http/protobuf":
		return OpenTelemetryProtocolHTTPProtobuf, nil
	case "http/json":
		return OpenTelemetryProtocolHTTPJSON, nil
	}
	return OpenTelemetryProtocol(""), fmt.Errorf("unknown OpenTelemetry protocol: %s", s)
}
