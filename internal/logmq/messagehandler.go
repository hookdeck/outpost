package logmq

import (
	"context"
	"log"

	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type messageHandler struct {
	logger *otelzap.Logger
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(logger *otelzap.Logger) consumer.MessageHandler {
	return &messageHandler{
		logger: logger,
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	logger := h.logger.Ctx(ctx)
	deliveryEvent := models.DeliveryEvent{}
	err := deliveryEvent.FromMessage(msg)
	if err != nil {
		msg.Nack()
		return err
	}
	logger.Info("logmq handler", zap.String("delivery_event", deliveryEvent.ID))
	log.Println(deliveryEvent)
	msg.Ack()
	return nil
}
