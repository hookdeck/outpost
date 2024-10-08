package eventtracer

import (
	"context"

	"github.com/hookdeck/EventKit/internal/models"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type noopEventTracer struct {
	tracer trace.Tracer
}

func NewNoopEventTracer() EventTracer {
	traceProvider := noop.NewTracerProvider()
	return &noopEventTracer{
		tracer: traceProvider.Tracer("eventtracer"),
	}
}

func (t *noopEventTracer) Receive(ctx context.Context, _ *models.Event) (context.Context, trace.Span) {
	_, span := t.tracer.Start(ctx, "EventTracer.Receive")
	return ctx, span
}

func (t *noopEventTracer) StartDelivery(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	_, span := t.tracer.Start(ctx, "EventTracer.StartDelivery")
	return ctx, span
}

func (t *noopEventTracer) Deliver(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	_, span := t.tracer.Start(ctx, "EventTracer.Deliver")
	return ctx, span
}
