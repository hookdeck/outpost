package emetrics_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The package meter delegates to the global provider, which binds once per
// process: keep this the only test in the package that sets it.
func TestEventSchemaInvalid(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })
	otel.SetMeterProvider(provider)

	m, err := emetrics.New()
	require.NoError(t, err)

	m.EventSchemaInvalid(ctx, "order.created", "enforce", emetrics.SchemaInvalidReasonInvalid)
	m.EventSchemaInvalid(ctx, "order.created", "enforce", emetrics.SchemaInvalidReasonInvalid)
	m.EventSchemaInvalid(ctx, "order.created", "warn", emetrics.SchemaInvalidReasonTooLarge)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	var sum *metricdata.Sum[int64]
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			if md.Name == "outpost.events.schema_invalid" {
				s, ok := md.Data.(metricdata.Sum[int64])
				require.True(t, ok, "counter must be an int64 sum")
				sum = &s
			}
		}
	}
	require.NotNil(t, sum, "outpost.events.schema_invalid must be recorded")
	assert.True(t, sum.IsMonotonic)

	got := map[attribute.Distinct]int64{}
	for _, dp := range sum.DataPoints {
		assert.Equal(t, 3, dp.Attributes.Len(), "only topic, mode and reason are recorded")
		got[dp.Attributes.Equivalent()] = dp.Value
	}
	invalid := attribute.NewSet(
		attribute.String("topic", "order.created"),
		attribute.String("mode", "enforce"),
		attribute.String("reason", "invalid"),
	)
	tooLarge := attribute.NewSet(
		attribute.String("topic", "order.created"),
		attribute.String("mode", "warn"),
		attribute.String("reason", "too_large"),
	)
	assert.Equal(t, map[attribute.Distinct]int64{
		invalid.Equivalent():  2,
		tooLarge.Equivalent(): 1,
	}, got)
}
