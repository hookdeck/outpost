package publishmq

import (
	"context"
	"time"

	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/eventtracer"
	"github.com/hookdeck/EventKit/internal/idempotence"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) error
}

type eventHandler struct {
	tracer       eventtracer.EventTracer
	logger       *otelzap.Logger
	idempotence  idempotence.Idempotence
	deliveryMQ   *deliverymq.DeliveryMQ
	metadataRepo models.MetadataRepo
}

func NewEventHandler(
	logger *otelzap.Logger,
	redisClient *redis.Client,
	deliveryMQ *deliverymq.DeliveryMQ,
	metadataRepo models.MetadataRepo,
) EventHandler {
	return &eventHandler{
		tracer: eventtracer.NewEventTracer(),
		logger: logger,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
		deliveryMQ:   deliveryMQ,
		metadataRepo: metadataRepo,
	}
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) error {
	return h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func(ctx context.Context) error {
		return h.doHandle(ctx, event)
	})
}

func (h *eventHandler) doHandle(ctx context.Context, event *models.Event) error {
	h.logger.Info("received event", zap.Any("event", event))

	_, span := h.tracer.Receive(ctx, event)
	defer span.End()

	matchedDestinations, err := h.metadataRepo.MatchEvent(ctx, *event)
	if err != nil {
		return err
	}

	// TODO: Consider handling via goroutine for better performance? Maybe GoCloud PubSub already support this natively?
	// TODO: Consider how batch publishing work
	for _, destinationSummary := range matchedDestinations {
		deliveryEvent := models.NewDeliveryEvent(*event, destinationSummary.ID)
		_, deliverySpan := h.tracer.StartDelivery(ctx, &deliveryEvent)
		err := h.deliveryMQ.Publish(ctx, deliveryEvent)
		if err != nil {
			span.RecordError(err)
			deliverySpan.RecordError(err)
			deliverySpan.End()
			return err
		}
		deliverySpan.End()
	}
	return nil
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "idempotency:publishmq:" + event.ID
}
