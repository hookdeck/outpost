package eventtracer

import (
	"context"

	"github.com/hookdeck/EventKit/internal/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type EventTracer interface {
	Receive(context.Context, *models.Event) (context.Context, trace.Span)
	StartDelivery(context.Context, *models.DeliveryEvent) (context.Context, trace.Span)
	Deliver(context.Context, *models.DeliveryEvent) (context.Context, trace.Span)
}

type eventTracerImpl struct {
	tracer trace.Tracer
}

var _ EventTracer = &eventTracerImpl{}

func NewEventTracer() EventTracer {
	traceProvider := otel.GetTracerProvider()

	return &eventTracerImpl{
		tracer: traceProvider.Tracer("github.com/hookdeck/EventKit/internal/eventtracer"),
	}
}

func (t *eventTracerImpl) Receive(_ context.Context, event *models.Event) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(context.Background(), "EventTracer.Receive")
	span.SetAttributes(attribute.String("eventkit.event_id", event.ID))

	event.Metadata["trace_id"] = span.SpanContext().TraceID().String()
	event.Metadata["span_id"] = span.SpanContext().SpanID().String()

	return ctx, span
}

func (t *eventTracerImpl) StartDelivery(_ context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(t.getRemoteEventSpanContext(&deliveryEvent.Event), "EventTracer.StartDelivery")
	span.SetAttributes(attribute.String("eventkit.delivery_event_id", deliveryEvent.ID))
	span.SetAttributes(attribute.String("eventkit.event_id", deliveryEvent.Event.ID))
	span.SetAttributes(attribute.String("eventkit.destination_id", deliveryEvent.DestinationID))

	deliveryEvent.Metadata["trace_id"] = span.SpanContext().TraceID().String()
	deliveryEvent.Metadata["span_id"] = span.SpanContext().SpanID().String()

	return ctx, span
}

func (t *eventTracerImpl) Deliver(_ context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(t.getRemoteDeliveryEventSpanContext(deliveryEvent), "EventTracer.Deliver")
	span.SetAttributes(attribute.String("eventkit.delivery_event_id", deliveryEvent.ID))
	span.SetAttributes(attribute.String("eventkit.event_id", deliveryEvent.Event.ID))
	span.SetAttributes(attribute.String("eventkit.destination_id", deliveryEvent.DestinationID))
	return ctx, span
}

func (t *eventTracerImpl) getRemoteEventSpanContext(event *models.Event) context.Context {
	traceID, err := trace.TraceIDFromHex(event.Metadata["trace_id"])
	if err != nil {
		// TODO: handle error
		return context.Background()
	}

	spanID, err := trace.SpanIDFromHex(event.Metadata["span_id"])
	if err != nil {
		// TODO: handle error
		return context.Background()
	}

	remoteCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: 01,
		Remote:     true,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), remoteCtx)
}

func (t *eventTracerImpl) getRemoteDeliveryEventSpanContext(deliveryEvent *models.DeliveryEvent) context.Context {
	traceID, err := trace.TraceIDFromHex(deliveryEvent.Metadata["trace_id"])
	if err != nil {
		// TODO: handle error
		return context.Background()
	}

	spanID, err := trace.SpanIDFromHex(deliveryEvent.Metadata["span_id"])
	if err != nil {
		// TODO: handle error
		return context.Background()
	}

	remoteCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: 01,
		Remote:     true,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), remoteCtx)
}
