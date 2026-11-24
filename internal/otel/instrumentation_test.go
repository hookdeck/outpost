package otel

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/mempubsub"
)

type recorded struct {
	spans  *tracetest.SpanRecorder
	reader *sdkmetric.ManualReader
}

func (r recorded) spanScopes() map[string]sdktrace.ReadOnlySpan {
	out := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range r.spans.Ended() {
		out[s.InstrumentationScope().Name+" "+s.Name()] = s
	}
	return out
}

func (r recorded) meterScopes(t *testing.T) map[string]bool {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, r.reader.Collect(context.Background(), &rm))
	out := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		if len(sm.Metrics) > 0 {
			out[sm.Scope.Name] = true
		}
	}
	return out
}

// setGlobalProviders installs filtered SDK providers as the globals, the way
// SetupOTelSDK does, and restores the previous ones when the test ends.
func setGlobalProviders(t *testing.T, enabled []string) recorded {
	t.Helper()
	r := recorded{spans: tracetest.NewSpanRecorder(), reader: sdkmetric.NewManualReader()}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(r.spans))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r.reader))
	filter := newScopeFilter(enabled)

	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(&filteredTracerProvider{tp: tp, filter: filter})
	otel.SetMeterProvider(&filteredMeterProvider{mp: mp, filter: filter})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})
	return r
}

// exercise runs a Redis command and an MQ publish under an Outpost span.
func exercise(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	ctx, span := otel.Tracer("github.com/hookdeck/outpost/internal/mqs").Start(ctx, "Queue.Publish")

	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	require.NoError(t, redisotel.InstrumentTracing(client))
	require.NoError(t, client.Set(ctx, "k", "v", 0).Err())

	topic := mempubsub.NewTopic()
	t.Cleanup(func() { topic.Shutdown(context.Background()) })
	require.NoError(t, topic.Send(ctx, &pubsub.Message{Body: []byte("x")}))

	// A span started below the MQ client's span.
	childCtx, _ := otel.Tracer("gocloud.dev/pubsub").Start(ctx, "Topic.Send")
	_, child := otel.Tracer("github.com/hookdeck/outpost/internal/consumer").Start(childCtx, "Consumer.Handle")
	child.End()

	span.End()
}

func TestInstrumentationDefaultOff(t *testing.T) {
	r := setGlobalProviders(t, nil)
	exercise(t)

	spans := r.spanScopes()
	assert.Len(t, spans, 2, "only Outpost spans: %v", spans)
	parent := spans["github.com/hookdeck/outpost/internal/mqs Queue.Publish"]
	child := spans["github.com/hookdeck/outpost/internal/consumer Consumer.Handle"]
	require.NotNil(t, parent)
	require.NotNil(t, child)
	assert.Equal(t, parent.SpanContext().SpanID(), child.Parent().SpanID(), "span below a dropped span attaches to its parent")
	assert.Equal(t, parent.SpanContext().TraceID(), child.SpanContext().TraceID())

	assert.NotContains(t, r.meterScopes(t), "gocloud.dev/pubsub")
}

func TestInstrumentationOptIn(t *testing.T) {
	tests := []struct {
		name      string
		enabled   []string
		redis, mq bool
	}{
		{name: "redis", enabled: []string{InstrumentationRedis}, redis: true},
		{name: "mq", enabled: []string{InstrumentationMQ}, mq: true},
		{name: "redis and mq", enabled: []string{InstrumentationRedis, InstrumentationMQ}, redis: true, mq: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setGlobalProviders(t, tt.enabled)
			exercise(t)

			spans := r.spanScopes()
			_, hasRedis := spans["github.com/redis/go-redis/extra/redisotel set"]
			_, hasMQ := spans["gocloud.dev/pubsub gocloud.dev/pubsub.Topic.Send"]
			assert.Equal(t, tt.redis, hasRedis, "redis spans: %v", spans)
			assert.Equal(t, tt.mq, hasMQ, "mq spans: %v", spans)
			assert.Equal(t, tt.mq, r.meterScopes(t)["gocloud.dev/pubsub"], "mq metrics")

			assert.Contains(t, spans, "github.com/hookdeck/outpost/internal/mqs Queue.Publish")
			assert.Contains(t, spans, "github.com/hookdeck/outpost/internal/consumer Consumer.Handle")
		})
	}
}

func TestScopeFilter(t *testing.T) {
	t.Parallel()

	off := newScopeFilter(nil)
	assert.True(t, off.drops("gocloud.dev/pubsub"))
	assert.True(t, off.drops("github.com/redis/go-redis/extra/redisotel"))
	assert.True(t, off.drops("go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"))
	assert.False(t, off.drops("outpost"))
	assert.False(t, off.drops("github.com/hookdeck/outpost/internal/mqs"))
	assert.False(t, off.drops("go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"))
	assert.False(t, off.drops("github.com/uptrace/opentelemetry-go-extra/otelzap"))

	on := newScopeFilter(Instrumentations)
	assert.False(t, on.drops("gocloud.dev/pubsub"))
	assert.False(t, on.drops("github.com/redis/go-redis/extra/redisotel"))
	assert.False(t, on.drops("go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"))
}
