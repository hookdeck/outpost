package publishmq

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
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
	var publishedEvent PublishedEvent
	if err := json.Unmarshal(msg.Body, &publishedEvent); err != nil {
		msg.Nack()
		return err
	}
	event := publishedEvent.toEvent()
	_, err := h.eventHandler.Handle(ctx, &event)
	if err != nil {
		msg.Nack()
		return err
	}
	msg.Ack()
	return nil
}

type PublishedEvent struct {
	ID               string                 `json:"id"`
	TenantID         string                 `json:"tenant_id" binding:"required"`
	DestinationID    string                 `json:"destination_id"`
	Topic            string                 `json:"topic"`
	EligibleForRetry *bool                  `json:"eligible_for_retry"`
	Time             time.Time              `json:"time"`
	Metadata         map[string]string      `json:"metadata"`
	Data             map[string]interface{} `json:"data"`
}

func (p *PublishedEvent) toEvent() models.Event {
	id := p.ID
	if id == "" {
		id = idgen.Event()
	}
	eventTime := p.Time
	if eventTime.IsZero() {
		eventTime = time.Now()
	}
	eligibleForRetry := true
	if p.EligibleForRetry != nil {
		eligibleForRetry = *p.EligibleForRetry
	}
	return models.Event{
		ID:               id,
		TenantID:         p.TenantID,
		DestinationID:    p.DestinationID,
		Topic:            p.Topic,
		EligibleForRetry: eligibleForRetry,
		Time:             eventTime,
		Metadata:         p.Metadata,
		Data:             p.Data,
	}
}
