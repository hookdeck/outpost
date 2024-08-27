package otel

import (
	"context"
	"time"

	"github.com/hookdeck/EventKit/internal/config"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

func newTraceProvider(ctx context.Context, c *config.Config) (*trace.TracerProvider, error) {
	if c.OpenTelemetry.Traces == nil {
		return nil, nil
	}

	var err error
	var traceExporter trace.SpanExporter
	if c.OpenTelemetry.Traces.Protocol == config.OpenTelemetryProtocolGRPC {
		traceExporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithInsecure(), // TODO: support TLS
			otlptracegrpc.WithEndpoint(c.OpenTelemetry.Traces.Endpoint),
		)
	} else {
		traceExporter, err = otlptracehttp.New(ctx,
			otlptracehttp.WithInsecure(), // TODO: support TLS
			otlptracehttp.WithEndpointURL(ensureHTTPEndpoint("traces", c.OpenTelemetry.Traces.Endpoint)),
		)
	}
	// traceExporter, err = stdouttrace.New()

	if err != nil {
		return nil, err
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter,
			// FIXME
			// Default is 5s. Set to 1s for demonstrative purposes.
			trace.WithBatchTimeout(time.Second)),
	)

	return traceProvider, nil
}

func newMeterProvider(ctx context.Context, c *config.Config) (*metric.MeterProvider, error) {
	if c.OpenTelemetry.Metrics == nil {
		return nil, nil
	}

	var err error
	var metricExporter metric.Exporter
	if c.OpenTelemetry.Metrics.Protocol == config.OpenTelemetryProtocolGRPC {
		metricExporter, err = otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithInsecure(), // TODO: support TLS
			otlpmetricgrpc.WithEndpoint(c.OpenTelemetry.Metrics.Endpoint),
		)
	} else {
		metricExporter, err = otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithInsecure(), // TODO: support TLS
			otlpmetrichttp.WithEndpointURL(ensureHTTPEndpoint("metrics", c.OpenTelemetry.Metrics.Endpoint)),
		)
	}

	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			// FIXME
			// Default is 1m. Set to 3s for demonstrative purposes.
			metric.WithInterval(3*time.Second))),
	)
	return meterProvider, nil
}

func newLoggerProvider(ctx context.Context, c *config.Config) (*log.LoggerProvider, error) {
	if c.OpenTelemetry.Logs == nil {
		return nil, nil
	}

	var err error
	var logExporter log.Exporter
	if c.OpenTelemetry.Logs.Protocol == config.OpenTelemetryProtocolGRPC {
		logExporter, err = otlploggrpc.New(ctx,
			otlploggrpc.WithInsecure(), // TODO: support TLS
			otlploggrpc.WithEndpoint(c.OpenTelemetry.Logs.Endpoint),
		)
	} else {
		logExporter, err = otlploghttp.New(ctx,
			otlploghttp.WithInsecure(), // TODO: support TLS
			otlploghttp.WithEndpointURL(ensureHTTPEndpoint("logs", c.OpenTelemetry.Logs.Endpoint)),
		)
	}

	if err != nil {
		return nil, err
	}

	loggerProvider := log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
	)
	return loggerProvider, nil
}

func ensureHTTPEndpoint(exporterType string, endpoint string) string {
	fullEndpoint := endpoint
	if endpoint[:4] != "http" {
		fullEndpoint = "http://" + endpoint
	}
	if endpoint[len(endpoint)-len("/v1/"+exporterType):] != "/v1/"+exporterType {
		fullEndpoint = fullEndpoint + "/v1/" + exporterType
	}
	return fullEndpoint
}
