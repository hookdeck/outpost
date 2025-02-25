package emetrics

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type OutpostMetrics interface {
	DeliveryLatency(ctx context.Context, latency time.Duration, opts DeliveryLatencyOpts)
	EventDelivered(ctx context.Context, deliveryEvent *models.DeliveryEvent, ok bool, destinationType string)
	EventPublished(ctx context.Context, event *models.Event)
	EventEligbible(ctx context.Context, event *models.Event)
	APIResponseLatency(ctx context.Context, latency time.Duration, opts APIResponseLatencyOpts)
	APICalls(ctx context.Context, opts APICallsOpts)
}

type DeliveryLatencyOpts struct {
	Type string
}

type DeliveryErrorRate struct {
	Type string
}

type APIResponseLatencyOpts struct {
	Method string
	Path   string
}

type APICallsOpts struct {
	Method string
	Path   string
}

// ============================== Impl ==============================

var meter = otel.Meter("outpost")

type emetricsImpl struct {
	deliveryLatency       metric.Int64Histogram
	eventDeliveredCounter metric.Int64Counter
	eventPublishedCounter metric.Int64Counter
	eventEligibleCounter  metric.Int64Counter
	apiResponseLatency    metric.Int64Histogram
	apiCallsCounter       metric.Int64Counter
}

func New() (OutpostMetrics, error) {
	impl := emetricsImpl{}

	var err error
	if impl.deliveryLatency, err = meter.Int64Histogram("outpost.delivery_latency",
		metric.WithUnit("ms"),
		metric.WithDescription("Event delivery latency"),
	); err != nil {
		return nil, err
	}

	if impl.eventDeliveredCounter, err = meter.Int64Counter("outpost.delivered_events",
		metric.WithDescription("Number of delivered events"),
	); err != nil {
		return nil, err
	}

	if impl.eventPublishedCounter, err = meter.Int64Counter("outpost.published_events",
		metric.WithDescription("Number of published events"),
	); err != nil {
		return nil, err
	}

	if impl.eventEligibleCounter, err = meter.Int64Counter("outpost.eligible_events",
		metric.WithDescription("Number of eligible events"),
	); err != nil {
		return nil, err
	}

	if impl.apiResponseLatency, err = meter.Int64Histogram("outpost.api_response_latency",
		metric.WithUnit("ms"),
		metric.WithDescription("API response latency"),
	); err != nil {
		return nil, err
	}

	if impl.apiCallsCounter, err = meter.Int64Counter("outpost.api_calls",
		metric.WithDescription("Number of API calls"),
	); err != nil {
		return nil, err
	}

	return &impl, nil
}

func (e *emetricsImpl) DeliveryLatency(ctx context.Context, latency time.Duration, opts DeliveryLatencyOpts) {
	e.deliveryLatency.Record(ctx, latency.Milliseconds(), metric.WithAttributes(attribute.String("type", opts.Type)))
}

func (e *emetricsImpl) EventDelivered(ctx context.Context, deliveryEvent *models.DeliveryEvent, ok bool, destinationType string) {
	var status string
	if ok {
		status = models.DeliveryStatusSuccess
	} else {
		status = models.DeliveryStatusFailed
	}
	e.eventDeliveredCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", destinationType),
		attribute.String("status", status),
	))
}

func (e *emetricsImpl) EventPublished(ctx context.Context, event *models.Event) {
	e.eventPublishedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", event.Topic)))
}

func (e *emetricsImpl) EventEligbible(ctx context.Context, event *models.Event) {
	e.eventEligibleCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("topic", event.Topic)))
}

func (e *emetricsImpl) APIResponseLatency(ctx context.Context, latency time.Duration, opts APIResponseLatencyOpts) {
	e.apiResponseLatency.Record(ctx, latency.Milliseconds(), metric.WithAttributes(
		attribute.String("method", opts.Method),
		attribute.String("path", opts.Path),
	))
}

func (e *emetricsImpl) APICalls(ctx context.Context, opts APICallsOpts) {
	e.apiCallsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", opts.Method),
		attribute.String("path", opts.Path),
	))
}
