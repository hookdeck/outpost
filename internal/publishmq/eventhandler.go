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
	ErrInvalidTopic = errors.New("invalid topic")
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
	redisClient *redis.Client,
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
	if len(h.topics) > 0 && event.Topic != "*" && !slices.Contains(h.topics, event.Topic) {
		return ErrInvalidTopic
	}
	return h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func(ctx context.Context) error {
		return h.doHandle(ctx, event)
	})
}

func (h *eventHandler) doHandle(ctx context.Context, event *models.Event) error {
	h.logger.Info("received event", zap.Any("event", event))

	_, span := h.eventTracer.Receive(ctx, event)
	defer span.End()

	matchedDestinations, err := h.entityStore.MatchEvent(ctx, *event)
	if err != nil {
		return err
	}
	if len(matchedDestinations) == 0 {
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
	err := h.deliveryMQ.Publish(ctx, deliveryEvent)
	if err != nil {
		deliverySpan.RecordError(err)
		deliverySpan.End()
		return err
	}
	deliverySpan.End()
	return nil
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "idempotency:publishmq:" + event.ID
}
