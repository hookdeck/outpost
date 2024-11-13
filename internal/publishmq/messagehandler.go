package publishmq

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
)

type messageHandler struct {
	eventHandler EventHandler
}

func NewMessageHandler(eventHandler EventHandler) consumer.MessageHandler {
	return &messageHandler{
		eventHandler: eventHandler,
	}
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	event := models.Event{}
	if err := event.FromMessage(msg); err != nil {
		msg.Nack()
		return err
	}
	// TODO: share logic with /publish flow
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if err := h.eventHandler.Handle(ctx, &event); err != nil {
		msg.Nack()
		return err
	}
	msg.Ack()
	return nil
}
