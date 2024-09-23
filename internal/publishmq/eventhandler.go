package publishmq

import (
	"context"
	"errors"
	"log"

	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) error
}

type eventHandler struct {
	logger      *otelzap.Logger
	redisClient *redis.Client
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewEventHandler(logger *otelzap.Logger, redisClient *redis.Client, deliveryMQ *deliverymq.DeliveryMQ) EventHandler {
	return &eventHandler{
		logger:      logger,
		redisClient: redisClient,
		deliveryMQ:  deliveryMQ,
	}
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) error {
	// Check idempotency
	isIdempotent, err := h.checkIdempotency(ctx, event)
	if err != nil {
		h.logger.Info("error checking idempotency", zap.Error(err))
		return err
	}
	if !isIdempotent {
		h.logger.Info("message is not idempotent")
		return errors.New("idempotent hit")
	}

	// Message handling logic
	h.logger.Info("received event", zap.Any("event", event))
	err = h.deliveryMQ.Publish(ctx, *event)
	if err != nil {
		h.logger.Info("error publishing message to deliverymq", zap.Error(err))
		return err
	}

	return nil
}

func (h *eventHandler) checkIdempotency(ctx context.Context, event *models.Event) (bool, error) {
	idempotencyKey := idempotencyKeyFromEvent(event)
	log.Println("check PublishMQ Message Idempotency", idempotencyKey)

	return true, nil
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "event:" + event.ID
}
