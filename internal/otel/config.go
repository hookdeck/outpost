package otel

import "fmt"

type OpenTelemetryProtocol string

type OpenTelemetryTypeConfig struct {
	Endpoint string
	Protocol OpenTelemetryProtocol
}

type OpenTelemetryConfig struct {
	Traces  *OpenTelemetryTypeConfig
	Metrics *OpenTelemetryTypeConfig
	Logs    *OpenTelemetryTypeConfig
}

const (
	OpenTelemetryProtocolGRPC         OpenTelemetryProtocol = "grpc"
	OpenTelemetryProtocolHTTPProtobuf OpenTelemetryProtocol = "http/protobuf"
	OpenTelemetryProtocolHTTPJSON     OpenTelemetryProtocol = "http/json"
)

func OpenTelemetryProtocolFromString(s string) (OpenTelemetryProtocol, error) {
	switch s {
	case "grpc":
		return OpenTelemetryProtocolGRPC, nil
	case "http/protobuf":
		return OpenTelemetryProtocolHTTPProtobuf, nil
	case "http/json":
		return OpenTelemetryProtocolHTTPJSON, nil
	default:
		return OpenTelemetryProtocol(""), fmt.Errorf("unknown OpenTelemetry protocol: %s", s)
	}
}
