package logmq

import (
	"context"

	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type eventBatcher interface {
	Add(ctx context.Context, event *models.Event) error
}

type deliveryBatcher interface {
	Add(ctx context.Context, delivery *models.Delivery) error
}

type messageHandler struct {
	logger          *otelzap.Logger
	eventBatcher    eventBatcher
	deliveryBatcher deliveryBatcher
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(logger *otelzap.Logger, eventBatcher eventBatcher, deliveryBatcher deliveryBatcher) consumer.MessageHandler {
	return &messageHandler{
		logger:          logger,
		eventBatcher:    eventBatcher,
		deliveryBatcher: deliveryBatcher,
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	logger := h.logger.Ctx(ctx)
	// Parse data from message.
	deliveryEvent := models.DeliveryEvent{}
	err := deliveryEvent.FromMessage(msg)
	if err != nil {
		msg.Nack()
		return err
	}
	// Handler logic
	logger.Info("logmq handler", zap.String("delivery_event", deliveryEvent.ID))
	err = h.deliveryBatcher.Add(ctx, deliveryEvent.Delivery)
	if err != nil {
		msg.Nack()
		return err
	}
	err = h.eventBatcher.Add(ctx, &deliveryEvent.Event)
	if err != nil {
		msg.Nack()
		return err
	}
	msg.Ack()
	return nil
}
