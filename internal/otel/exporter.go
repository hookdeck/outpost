package otel

import (
	"context"

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

func newTraceProvider(ctx context.Context, config *OpenTelemetryConfig) (*trace.TracerProvider, error) {
	if config.Traces == nil {
		return nil, nil
	}

	switch config.Traces.Exporter {
	case "", "otlp":
		var traceExporter trace.SpanExporter
		var err error

		if config.Traces.Protocol == "grpc" {
			traceExporter, err = otlptracegrpc.New(ctx)
		} else {
			traceExporter, err = otlptracehttp.New(ctx)
		}

		if err != nil {
			return nil, err
		}

		return trace.NewTracerProvider(trace.WithBatcher(traceExporter)), nil
	default:
		// For future exporters like "console", "jaeger", etc.
		return trace.NewTracerProvider(), nil
	}
}

func newMeterProvider(ctx context.Context, config *OpenTelemetryConfig) (*metric.MeterProvider, error) {
	if config.Metrics == nil {
		return nil, nil
	}

	switch config.Metrics.Exporter {
	case "", "otlp":
		var metricExporter metric.Exporter
		var err error

		if config.Metrics.Protocol == "grpc" {
			metricExporter, err = otlpmetricgrpc.New(ctx)
		} else {
			metricExporter, err = otlpmetrichttp.New(ctx)
		}

		if err != nil {
			return nil, err
		}

		return metric.NewMeterProvider(metric.WithReader(metric.NewPeriodicReader(metricExporter))), nil
	default:
		// For future exporters like "prometheus", etc.
		return metric.NewMeterProvider(), nil
	}
}

func newLoggerProvider(ctx context.Context, config *OpenTelemetryConfig) (*log.LoggerProvider, error) {
	if config.Logs == nil {
		return nil, nil
	}

	switch config.Logs.Exporter {
	case "", "otlp":
		var logExporter log.Exporter
		var err error

		if config.Logs.Protocol == "grpc" {
			logExporter, err = otlploggrpc.New(ctx)
		} else {
			logExporter, err = otlploghttp.New(ctx)
		}

		if err != nil {
			return nil, err
		}

		return log.NewLoggerProvider(log.WithProcessor(log.NewBatchProcessor(logExporter))), nil
	default:
		// For future exporters like "console", etc.
		return log.NewLoggerProvider(), nil
	}
}
