package deliverymq

import (
	"context"
	"time"

	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/eventtracer"
	"github.com/hookdeck/EventKit/internal/idempotence"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type messageHandler struct {
	logger           *otelzap.Logger
	redisClient      *redis.Client
	destinationModel *models.DestinationModel
	idempotence      idempotence.Idempotence
	tracer           eventtracer.EventTracer
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(logger *otelzap.Logger, redisClient *redis.Client, destinationModel *models.DestinationModel) consumer.MessageHandler {
	return &messageHandler{
		tracer:           eventtracer.NewEventTracer(),
		logger:           logger,
		redisClient:      redisClient,
		destinationModel: destinationModel,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	deliveryEvent := models.DeliveryEvent{}
	err := deliveryEvent.FromMessage(msg)
	if err != nil {
		msg.Nack()
		return err
	}
	err = h.idempotence.Exec(ctx, idempotencyKeyFromDeliveryEvent(deliveryEvent), func(ctx context.Context) error {
		return h.doHandle(ctx, deliveryEvent)
	})
	if err != nil {
		msg.Nack()
		return err
	}
	msg.Ack()
	return nil
}

func (h *messageHandler) doHandle(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	_, span := h.tracer.Deliver(ctx, &deliveryEvent)
	defer span.End()
	logger := h.logger.Ctx(ctx)
	logger.Info("deliverymq handler", zap.String("delivery_event", deliveryEvent.ID))
	err := deliveryEvent.Destination.Publish(ctx, &deliveryEvent.Event)
	if err != nil {
		logger.Error("failed to publish event", zap.Error(err))
		span.RecordError(err)
		return err
	}
	return nil
}

func idempotencyKeyFromDeliveryEvent(deliveryEvent models.DeliveryEvent) string {
	return "idempotency:deliverymq:" + deliveryEvent.ID
}
