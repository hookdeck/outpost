package otel

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/metric"
	metricembedded "go.opentelemetry.io/otel/metric/embedded"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	traceembedded "go.opentelemetry.io/otel/trace/embedded"
)

// Third-party instrumentation that is off unless listed in
// OpenTelemetryConfig.EnabledInstrumentations.
const (
	InstrumentationRedis = "redis"
	InstrumentationMQ    = "mq"
)

// Instrumentations lists the valid EnabledInstrumentations values.
var Instrumentations = []string{InstrumentationRedis, InstrumentationMQ}

// instrumentationScopes maps each key to the instrumentation scope name
// prefixes it covers.
var instrumentationScopes = map[string][]string{
	InstrumentationRedis: {"github.com/redis/go-redis/extra/redisotel"},
	InstrumentationMQ: {
		"gocloud.dev/",
		"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc",
	},
}

type scopeFilter []string

func newScopeFilter(enabled []string) scopeFilter {
	on := make(map[string]bool, len(enabled))
	for _, key := range enabled {
		on[key] = true
	}
	var dropped scopeFilter
	for _, key := range Instrumentations {
		if !on[key] {
			dropped = append(dropped, instrumentationScopes[key]...)
		}
	}
	return dropped
}

func (f scopeFilter) drops(scope string) bool {
	for _, prefix := range f {
		if strings.HasPrefix(scope, prefix) {
			return true
		}
	}
	return false
}

type filteredTracerProvider struct {
	traceembedded.TracerProvider
	tp     trace.TracerProvider
	filter scopeFilter
}

func (p *filteredTracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	if p.filter.drops(name) {
		return passThroughTracer{}
	}
	return p.tp.Tracer(name, opts...)
}

// passThroughTracer records nothing and keeps the caller's span context, so
// spans started below a dropped span attach to its parent.
type passThroughTracer struct {
	traceembedded.Tracer
}

func (passThroughTracer) Start(ctx context.Context, _ string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx = trace.ContextWithSpanContext(ctx, trace.SpanContextFromContext(ctx))
	return ctx, trace.SpanFromContext(ctx)
}

type filteredMeterProvider struct {
	metricembedded.MeterProvider
	mp     metric.MeterProvider
	filter scopeFilter
}

func (p *filteredMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	if p.filter.drops(name) {
		return metricnoop.Meter{}
	}
	return p.mp.Meter(name, opts...)
}
