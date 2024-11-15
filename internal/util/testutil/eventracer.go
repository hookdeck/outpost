package testutil

import (
	"context"

	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/models"
	traceSDK "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type mockEventTracerImpl struct {
	tracer        trace.Tracer
	receive       func(context.Context, *models.Event) (context.Context, trace.Span)
	startDelivery func(context.Context, *models.DeliveryEvent) (context.Context, trace.Span)
	deliver       func(context.Context, *models.DeliveryEvent) (context.Context, trace.Span)
}

var _ eventtracer.EventTracer = (*mockEventTracerImpl)(nil)

func (m *mockEventTracerImpl) Receive(ctx context.Context, event *models.Event) (context.Context, trace.Span) {
	return m.receive(ctx, event)
}

func (m *mockEventTracerImpl) StartDelivery(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	return m.startDelivery(ctx, deliveryEvent)
}

func (m *mockEventTracerImpl) Deliver(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	return m.deliver(ctx, deliveryEvent)
}

func NewMockEventTracer(exporter traceSDK.SpanExporter) *mockEventTracerImpl {
	traceProvider := traceSDK.NewTracerProvider(traceSDK.WithBatcher(
		exporter,
		traceSDK.WithBatchTimeout(0),
	))

	mockEventTracer := &mockEventTracerImpl{
		tracer: traceProvider.Tracer("mockeventtracer"),
	}
	mockEventTracer.receive = func(ctx context.Context, event *models.Event) (context.Context, trace.Span) {
		return mockEventTracer.tracer.Start(ctx, "Receive")
	}
	mockEventTracer.startDelivery = func(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
		return mockEventTracer.tracer.Start(ctx, "StartDelivery")
	}
	mockEventTracer.deliver = func(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
		return mockEventTracer.tracer.Start(ctx, "Deliver")
	}

	return mockEventTracer
}
