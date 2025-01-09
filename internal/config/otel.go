package config

import (
	"fmt"

	"github.com/hookdeck/outpost/internal/otel"
	v "github.com/spf13/viper"
)

type OpenTelemetryTypeConfig struct {
	Exporter string `yaml:"exporter" env:"OTEL_EXPORTER"`
	Protocol string `yaml:"protocol" env:"OTEL_PROTOCOL"`
}

type OpenTelemetryConfig struct {
	ServiceName string                   `yaml:"service_name" env:"OTEL_SERVICE_NAME"`
	Traces      *OpenTelemetryTypeConfig `yaml:"traces"`
	Metrics     *OpenTelemetryTypeConfig `yaml:"metrics"`
	Logs        *OpenTelemetryTypeConfig `yaml:"logs"`
}

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

func (c *OpenTelemetryConfig) ToOTELConfig() *otel.OpenTelemetryConfig {
	if c == nil || c.ServiceName == "" {
		return nil
	}

	return &otel.OpenTelemetryConfig{
		Traces: &otel.OpenTelemetryTypeConfig{
			Exporter: c.Traces.Exporter,
			Protocol: c.Traces.Protocol,
		},
		Metrics: &otel.OpenTelemetryTypeConfig{
			Exporter: c.Metrics.Exporter,
			Protocol: c.Metrics.Protocol,
		},
		Logs: &otel.OpenTelemetryTypeConfig{
			Exporter: c.Logs.Exporter,
			Protocol: c.Logs.Protocol,
		},
	}
}
