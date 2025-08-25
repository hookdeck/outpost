package publishmq

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	ErrInvalidTopic  = errors.New("invalid topic")
	ErrRequiredTopic = errors.New("topic is required")
)

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) error
}

type eventHandler struct {
	emeter      emetrics.OutpostMetrics
	eventTracer eventtracer.EventTracer
	logger      *logging.Logger
	idempotence idempotence.Idempotence
	deliveryMQ  *deliverymq.DeliveryMQ
	entityStore models.EntityStore
	topics      []string
}

func NewEventHandler(
	logger *logging.Logger,
	redisClient redis.Cmdable,
	deliveryMQ *deliverymq.DeliveryMQ,
	entityStore models.EntityStore,
	eventTracer eventtracer.EventTracer,
	topics []string,
) EventHandler {
	emeter, _ := emetrics.New()
	eventHandler := &eventHandler{
		logger: logger,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
		deliveryMQ:  deliveryMQ,
		entityStore: entityStore,
		eventTracer: eventTracer,
		topics:      topics,
		emeter:      emeter,
	}
	return eventHandler
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) error {
	if len(h.topics) > 0 && event.Topic == "" {
		return ErrRequiredTopic
	}
	if len(h.topics) > 0 && event.Topic != "*" && !slices.Contains(h.topics, event.Topic) {
		return ErrInvalidTopic
	}
	return h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func(ctx context.Context) error {
		return h.doHandle(ctx, event)
	})
}

func (h *eventHandler) doHandle(ctx context.Context, event *models.Event) error {
	logger := h.logger.Ctx(ctx)
	logger.Audit("processing event",
		zap.String("event_id", event.ID),
		zap.String("tenant_id", event.TenantID),
		zap.String("topic", event.Topic))

	_, span := h.eventTracer.Receive(ctx, event)
	defer span.End()

	matchedDestinations, err := h.entityStore.MatchEvent(ctx, *event)
	if err != nil {
		logger.Error("failed to match event destinations",
			zap.Error(err),
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID))
		return err
	}
	if len(matchedDestinations) == 0 {
		logger.Info("no matching destinations",
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID))
		return nil
	}

	h.emeter.EventEligbible(ctx, event)

	var g errgroup.Group
	for _, destinationSummary := range matchedDestinations {
		destID := destinationSummary.ID
		g.Go(func() error {
			return h.enqueueDeliveryEvent(ctx, models.NewDeliveryEvent(*event, destID))
		})
	}
	if err := g.Wait(); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

func (h *eventHandler) enqueueDeliveryEvent(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	_, deliverySpan := h.eventTracer.StartDelivery(ctx, &deliveryEvent)
	if err := h.deliveryMQ.Publish(ctx, deliveryEvent); err != nil {
		h.logger.Ctx(ctx).Error("failed to enqueue delivery event",
			zap.Error(err),
			zap.String("delivery_event_id", deliveryEvent.ID),
			zap.String("event_id", deliveryEvent.Event.ID),
			zap.String("destination_id", deliveryEvent.DestinationID))
		deliverySpan.RecordError(err)
		deliverySpan.End()
		return err
	}

	h.logger.Ctx(ctx).Audit("delivery event enqueued",
		zap.String("delivery_event_id", deliveryEvent.ID),
		zap.String("event_id", deliveryEvent.Event.ID),
		zap.String("destination_id", deliveryEvent.DestinationID))
	deliverySpan.End()
	return nil
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "idempotency:publishmq:" + event.ID
}
