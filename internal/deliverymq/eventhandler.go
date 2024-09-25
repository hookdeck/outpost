package deliverymq

import (
	"context"
	"time"

	"github.com/hookdeck/EventKit/internal/idempotence"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type EventHandler interface {
	Handle(ctx context.Context, deliveryEvent models.DeliveryEvent) error
}

type eventHandler struct {
	logger           *otelzap.Logger
	redisClient      *redis.Client
	destinationModel *models.DestinationModel
	idempotence      idempotence.Idempotence
}

func NewEventHandler(logger *otelzap.Logger, redisClient *redis.Client, destinationModel *models.DestinationModel) EventHandler {
	return &eventHandler{
		logger:           logger,
		redisClient:      redisClient,
		destinationModel: destinationModel,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
	}
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	return h.idempotence.Exec(ctx, idempotencyKeyFromDeliveryEvent(deliveryEvent), func() error {
		return h.doHandle(ctx, deliveryEvent)
	})
}

func (h *eventHandler) doHandle(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	h.logger.Info("deliverymq handler", zap.String("delivery_event", deliveryEvent.ID))
	err := deliveryEvent.Destination.Publish(ctx, &deliveryEvent.Event)
	if err != nil {
		h.logger.Error("failed to publish event", zap.Error(err))
		return err
	}
	return nil
}

func idempotencyKeyFromDeliveryEvent(deliveryEvent models.DeliveryEvent) string {
	return "idempotency:deliverymq:" + deliveryEvent.ID
}
