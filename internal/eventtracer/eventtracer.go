package eventtracer

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type EventTracer interface {
	Receive(context.Context, *models.Event) (context.Context, trace.Span)
	StartDelivery(context.Context, *models.DeliveryEvent) (context.Context, trace.Span)
	Deliver(context.Context, *models.DeliveryEvent, *models.Destination) (context.Context, trace.Span)
}

type eventTracerImpl struct {
	emeter emetrics.OutpostMetrics
	tracer trace.Tracer
}

var _ EventTracer = &eventTracerImpl{}

func NewEventTracer() EventTracer {
	traceProvider := otel.GetTracerProvider()
	emeter, _ := emetrics.New()

	return &eventTracerImpl{
		emeter: emeter,
		tracer: traceProvider.Tracer("github.com/hookdeck/outpost/internal/eventtracer"),
	}
}

func (t *eventTracerImpl) Receive(ctx context.Context, event *models.Event) (context.Context, trace.Span) {
	t.emeter.EventPublished(ctx, event)

	ctx, span := t.tracer.Start(context.Background(), "EventTracer.Receive")

	event.Telemetry = &models.EventTelemetry{
		TraceID:      span.SpanContext().TraceID().String(),
		SpanID:       span.SpanContext().SpanID().String(),
		ReceivedTime: time.Now().Format(time.RFC3339Nano),
	}

	return ctx, span
}

func (t *eventTracerImpl) StartDelivery(_ context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(t.getRemoteEventSpanContext(&deliveryEvent.Event), "EventTracer.StartDelivery")

	deliveryEvent.Telemetry = &models.DeliveryEventTelemetry{
		TraceID: span.SpanContext().TraceID().String(),
		SpanID:  span.SpanContext().SpanID().String(),
	}

	return ctx, span
}

type DeliverSpan struct {
	trace.Span
	emeter        emetrics.OutpostMetrics
	deliveryEvent *models.DeliveryEvent
	destination   *models.Destination
	err           error
}

func (d *DeliverSpan) RecordError(err error, options ...trace.EventOption) {
	d.err = err
	d.Span.RecordError(err, options...)
}

func (d *DeliverSpan) End(options ...trace.SpanEndOption) {
	if d.deliveryEvent.Event.Telemetry == nil {
		d.Span.End(options...)
		return
	}
	if d.deliveryEvent.Delivery == nil {
		d.Span.End(options...)
		return
	}

	ok := d.deliveryEvent.Delivery.Status == models.DeliveryStatusOK
	startTime, err := time.Parse(time.RFC3339Nano, d.deliveryEvent.Event.Telemetry.ReceivedTime)
	if err != nil {
		// TODO: handle error?
		d.Span.End(options...)
		return
	}

	d.emeter.DeliveryLatency(context.Background(),
		time.Since(startTime),
		emetrics.DeliveryLatencyOpts{Type: d.destination.Type})
	d.emeter.EventDelivered(context.Background(), d.deliveryEvent, ok, d.destination.Type)

	d.Span.End(options...)
}

func (t *eventTracerImpl) Deliver(_ context.Context, deliveryEvent *models.DeliveryEvent, destination *models.Destination) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(t.getRemoteDeliveryEventSpanContext(deliveryEvent), "EventTracer.Deliver")
	deliverySpan := &DeliverSpan{Span: span, emeter: t.emeter, deliveryEvent: deliveryEvent, destination: destination}
	return ctx, deliverySpan
}

func (t *eventTracerImpl) getRemoteEventSpanContext(event *models.Event) context.Context {
	if event.Telemetry == nil {
		return context.Background()
	}
	traceID, err := trace.TraceIDFromHex(event.Telemetry.TraceID)
	if err != nil {
		// TODO: handle error
		return context.Background()
	}

	spanID, err := trace.SpanIDFromHex(event.Telemetry.SpanID)
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
	if deliveryEvent.Telemetry == nil {
		return context.Background()
	}
	traceID, err := trace.TraceIDFromHex(deliveryEvent.Telemetry.TraceID)
	if err != nil {
		// TODO: handle error
		return context.Background()
	}

	spanID, err := trace.SpanIDFromHex(deliveryEvent.Telemetry.SpanID)
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
