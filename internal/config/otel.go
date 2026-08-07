package config

import (
	"errors"
	"fmt"

	"github.com/hookdeck/outpost/internal/otel"
)

type OpenTelemetryTypeConfig struct {
	Exporter string `yaml:"exporter" env:"OTEL_EXPORTER" desc:"Exporter for this signal ('otlp', 'console' or 'none'). Applies to all three signals; override a single signal with OTEL_TRACES_EXPORTER, OTEL_METRICS_EXPORTER or OTEL_LOGS_EXPORTER. If unset, a signal is enabled when its endpoint (or the generic OTEL_EXPORTER_OTLP_ENDPOINT) is set, or when no endpoint is set at all." required:"C"`
	Protocol string `yaml:"protocol" env:"OTEL_PROTOCOL" desc:"OTLP protocol ('grpc' or 'http') for this signal. Applies to all three signals; the standard OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_PROTOCOL and OTEL_EXPORTER_OTLP_PROTOCOL take precedence. Defaults to 'grpc'." required:"C"`
}

type OpenTelemetryConfig struct {
	ServiceName string                  `yaml:"service_name" env:"OTEL_SERVICE_NAME" desc:"The service name reported to OpenTelemetry. If set, OpenTelemetry will be enabled." required:"N"`
	Traces      OpenTelemetryTypeConfig `yaml:"traces" desc:"OpenTelemetry configuration specific to traces."`
	Metrics     OpenTelemetryTypeConfig `yaml:"metrics" desc:"OpenTelemetry configuration specific to metrics."`
	Logs        OpenTelemetryTypeConfig `yaml:"logs" desc:"OpenTelemetry configuration specific to logs."`
}

const (
	OTelProtocolGRPC = "grpc"
	OTelProtocolHTTP = "http"
	// OTelProtocolHTTPProtobuf is the OpenTelemetry specification's name for
	// what Outpost calls "http". Accepted as an alias.
	OTelProtocolHTTPProtobuf = "http/protobuf"

	// OTelExporterNone disables a signal. It is the specification's value and
	// falls through to each provider constructor's no-exporter branch.
	OTelExporterNone = "none"
)

// otelSignals are the signal names as they appear in the specification's
// environment variables (OTEL_<SIGNAL>_EXPORTER, OTEL_EXPORTER_OTLP_<SIGNAL>_*).
var otelSignals = []string{"TRACES", "METRICS", "LOGS"}

var validOTelProtocols = map[string]bool{
	OTelProtocolGRPC:         true,
	OTelProtocolHTTP:         true,
	OTelProtocolHTTPProtobuf: true,
}

var ErrInvalidOTelProtocol = errors.New("config validation error: invalid OpenTelemetry protocol, must be 'grpc' or 'http'")

func validateOTelProtocol(protocol string) error {
	if protocol == "" {
		return nil // Empty protocol will use default
	}
	if !validOTelProtocols[protocol] {
		return ErrInvalidOTelProtocol
	}
	return nil
}

// normalizeOTelProtocol maps the specification's protocol name onto the value
// the otel package switches on.
func normalizeOTelProtocol(protocol string) string {
	if protocol == OTelProtocolHTTPProtobuf {
		return OTelProtocolHTTP
	}
	return protocol
}

// getProtocol returns the protocol for a signal from the standard OpenTelemetry
// variables, most specific first. Empty means neither is set, and the caller
// falls back to OTEL_PROTOCOL and then to gRPC.
func getProtocol(osInterface OSInterface, signal string) string {
	if protocol := osInterface.Getenv(fmt.Sprintf("OTEL_EXPORTER_OTLP_%s_PROTOCOL", signal)); protocol != "" {
		return protocol
	}
	return osInterface.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
}

// resolveOTelEnv applies the OpenTelemetry environment variables that
// caarlos0/env cannot express: the per-signal exporter and protocol variables
// (one struct tag would bind the same variable to all three signals), and the
// endpoint variables, which decide which signals are enabled.
//
// It runs after env parsing, so c.<Signal>.Exporter/Protocol already hold the
// YAML value overridden by OTEL_EXPORTER/OTEL_PROTOCOL. Anything resolved here
// is more specific and wins.
func (c *OpenTelemetryConfig) resolveOTelEnv(osInterface OSInterface) {
	signals := map[string]*OpenTelemetryTypeConfig{
		"TRACES":  &c.Traces,
		"METRICS": &c.Metrics,
		"LOGS":    &c.Logs,
	}

	for _, signal := range otelSignals {
		cfg := signals[signal]
		if exporter := osInterface.Getenv(fmt.Sprintf("OTEL_%s_EXPORTER", signal)); exporter != "" {
			cfg.Exporter = exporter
		}
		if protocol := getProtocol(osInterface, signal); protocol != "" {
			cfg.Protocol = protocol
		}
	}

	// Endpoint inference, for signals left without an explicit exporter: a
	// per-signal endpoint enables that signal and disables the others, while
	// the generic endpoint enables all three. With no endpoint set at all,
	// every signal stays enabled against the SDK's default endpoint.
	if osInterface.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		return
	}
	endpoints := make(map[string]bool, len(otelSignals))
	anyEndpoint := false
	for _, signal := range otelSignals {
		if osInterface.Getenv(fmt.Sprintf("OTEL_EXPORTER_OTLP_%s_ENDPOINT", signal)) != "" {
			endpoints[signal] = true
			anyEndpoint = true
		}
	}
	if !anyEndpoint {
		return
	}
	for _, signal := range otelSignals {
		if cfg := signals[signal]; cfg.Exporter == "" && !endpoints[signal] {
			cfg.Exporter = OTelExporterNone
		}
	}
}

func (c *OpenTelemetryConfig) Validate() error {
	if c.ServiceName == "" {
		return nil // OpenTelemetry is optional
	}

	if err := validateOTelProtocol(c.Traces.Protocol); err != nil {
		return err
	}
	if err := validateOTelProtocol(c.Metrics.Protocol); err != nil {
		return err
	}
	if err := validateOTelProtocol(c.Logs.Protocol); err != nil {
		return err
	}

	return nil
}

func (c *OpenTelemetryConfig) ToConfig() *otel.OpenTelemetryConfig {
	if c.ServiceName == "" {
		return nil
	}

	// Set default protocol if not specified
	getProtocolWithDefault := func(p string) string {
		if p == "" {
			return OTelProtocolGRPC
		}
		return normalizeOTelProtocol(p)
	}

	return &otel.OpenTelemetryConfig{
		Traces: &otel.OpenTelemetryTypeConfig{
			Exporter: c.Traces.Exporter,
			Protocol: getProtocolWithDefault(c.Traces.Protocol),
		},
		Metrics: &otel.OpenTelemetryTypeConfig{
			Exporter: c.Metrics.Exporter,
			Protocol: getProtocolWithDefault(c.Metrics.Protocol),
		},
		Logs: &otel.OpenTelemetryTypeConfig{
			Exporter: c.Logs.Exporter,
			Protocol: getProtocolWithDefault(c.Logs.Protocol),
		},
	}
}

func (c *OpenTelemetryConfig) GetServiceName() string {
	if c == nil {
		return ""
	}
	return c.ServiceName
}
