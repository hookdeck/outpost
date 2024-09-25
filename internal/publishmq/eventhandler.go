package publishmq

import (
	"context"
	"time"

	"github.com/hookdeck/EventKit/internal/deliverymq"
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
	logger           *otelzap.Logger
	redisClient      *redis.Client
	idempotence      idempotence.Idempotence
	deliveryMQ       *deliverymq.DeliveryMQ
	destinationModel *models.DestinationModel
}

func NewEventHandler(logger *otelzap.Logger, redisClient *redis.Client, deliveryMQ *deliverymq.DeliveryMQ, destinationModel *models.DestinationModel) EventHandler {
	return &eventHandler{
		logger:      logger,
		redisClient: redisClient,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
		deliveryMQ:       deliveryMQ,
		destinationModel: destinationModel,
	}
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) error {
	return h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func() error {
		return h.doHandle(ctx, event)
	})
}

func (h *eventHandler) doHandle(ctx context.Context, event *models.Event) error {
	h.logger.Info("received event", zap.Any("event", event))

	destinations, err := h.destinationModel.List(ctx, h.redisClient, event.TenantID)
	if err != nil {
		return err
	}
	destinations = models.FilterTopics(destinations, event.Topic)

	// TODO: Consider handling via goroutine for better performance? Maybe GoCloud PubSub already support this natively?
	// TODO: Consider how batch publishing work
	for _, destination := range destinations {
		deliveryEvent := models.NewDeliveryEvent(*event, destination)
		err := h.deliveryMQ.Publish(ctx, deliveryEvent)
		if err != nil {
			return err
		}
	}
	return nil
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "idempotency:publishmq:" + event.ID
}
