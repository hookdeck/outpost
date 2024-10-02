package logmq

import (
	"context"

	"github.com/hookdeck/EventKit/internal/clickhouse"
	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type messageHandler struct {
	logger        *otelzap.Logger
	eventModel    *models.EventModel
	deliveryModel *models.DeliveryModel
	chDB          clickhouse.DB
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(logger *otelzap.Logger, chDB clickhouse.DB) consumer.MessageHandler {
	return &messageHandler{
		logger:        logger,
		eventModel:    models.NewEventModel(),
		deliveryModel: models.NewDeliveryModel(),
		chDB:          chDB,
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
	err = h.deliveryModel.InsertMany(ctx, h.chDB, []*models.Delivery{deliveryEvent.Delivery})
	if err != nil {
		msg.Nack()
		return err
	}
	err = h.eventModel.InsertMany(ctx, h.chDB, []*models.Event{&deliveryEvent.Event})
	if err != nil {
		msg.Nack()
		return err
	}
	msg.Ack()
	return nil
}
