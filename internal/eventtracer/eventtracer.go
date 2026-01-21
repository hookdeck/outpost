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
	StartDelivery(context.Context, *models.DeliveryTask) (context.Context, trace.Span)
	Deliver(context.Context, *models.DeliveryTask, *models.Destination) (context.Context, trace.Span)
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

func (t *eventTracerImpl) StartDelivery(_ context.Context, task *models.DeliveryTask) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(t.getRemoteEventSpanContext(&task.Event), "EventTracer.StartDelivery")

	task.Telemetry = &models.DeliveryEventTelemetry{
		TraceID: span.SpanContext().TraceID().String(),
		SpanID:  span.SpanContext().SpanID().String(),
	}

	return ctx, span
}

type DeliverSpan struct {
	trace.Span
	emeter      emetrics.OutpostMetrics
	task        *models.DeliveryTask
	destination *models.Destination
	err         error
}

func (d *DeliverSpan) RecordError(err error, options ...trace.EventOption) {
	d.err = err
	d.Span.RecordError(err, options...)
}

func (d *DeliverSpan) End(options ...trace.SpanEndOption) {
	if d.task.Event.Telemetry == nil {
		d.Span.End(options...)
		return
	}

	startTime, err := time.Parse(time.RFC3339Nano, d.task.Event.Telemetry.ReceivedTime)
	if err != nil {
		// TODO: handle error?
		d.Span.End(options...)
		return
	}

	d.emeter.DeliveryLatency(context.Background(),
		time.Since(startTime),
		emetrics.DeliveryLatencyOpts{Type: d.destination.Type})

	d.Span.End(options...)
}

func (t *eventTracerImpl) Deliver(_ context.Context, task *models.DeliveryTask, destination *models.Destination) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(t.getRemoteDeliveryTaskSpanContext(task), "EventTracer.Deliver")
	deliverySpan := &DeliverSpan{Span: span, emeter: t.emeter, task: task, destination: destination}
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

func (t *eventTracerImpl) getRemoteDeliveryTaskSpanContext(task *models.DeliveryTask) context.Context {
	if task.Telemetry == nil {
		return context.Background()
	}
	traceID, err := trace.TraceIDFromHex(task.Telemetry.TraceID)
	if err != nil {
		// TODO: handle error
		return context.Background()
	}

	spanID, err := trace.SpanIDFromHex(task.Telemetry.SpanID)
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
