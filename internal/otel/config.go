package otel

type OpenTelemetryTypeConfig struct {
	// Exporter can be "otlp", "none", etc.
	Exporter string
	// Protocol can be "grpc" or "http"
	Protocol string
}

type OpenTelemetryConfig struct {
	Traces  *OpenTelemetryTypeConfig
	Metrics *OpenTelemetryTypeConfig
	Logs    *OpenTelemetryTypeConfig
}
