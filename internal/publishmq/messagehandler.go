package publishmq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
)

type messageHandler struct {
	eventHandler      EventHandler
	maxRedeliveries   int
	redeliveryCounter RedeliveryCounter
}

type MessageHandlerOption func(*messageHandler)

// WithMaxRedeliveries rejects a message once it has failed with a transient
// error maxRedeliveries+1 times, on brokers that support Reject. 0 redelivers
// without limit.
func WithMaxRedeliveries(maxRedeliveries int, counter RedeliveryCounter) MessageHandlerOption {
	return func(h *messageHandler) {
		h.maxRedeliveries = maxRedeliveries
		h.redeliveryCounter = counter
	}
}

func NewMessageHandler(eventHandler EventHandler, opts ...MessageHandlerOption) consumer.MessageHandler {
	h := &messageHandler{
		eventHandler: eventHandler,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	var publishedEvent PublishedEvent
	if err := json.Unmarshal(msg.Body, &publishedEvent); err != nil {
		return reject(msg, err)
	}
	// json.RawMessage is []byte, so null, invalid JSON, and non-object types
	// slip past unmarshaling. Reject since data must be a JSON object.
	if !json.Valid(publishedEvent.Data) || publishedEvent.Data[0] != '{' {
		return reject(msg, ErrInvalidData)
	}
	event := publishedEvent.toEvent()
	_, err := h.eventHandler.Handle(ctx, &event)
	if err != nil {
		// ErrInvalidTopic goes back on the queue: adding the topic to TOPICS
		// fixes it.
		if errors.Is(err, ErrRequiredTopic) {
			return reject(msg, err)
		}
		return h.nack(ctx, msg, err)
	}
	msg.Ack()
	return nil
}

func (h *messageHandler) nack(ctx context.Context, msg *mqs.Message, err error) error {
	if h.maxRedeliveries <= 0 || !msg.Rejectable() {
		msg.Nack()
		return err
	}
	// Without a count, redeliver rather than risk dropping the message.
	failures, countErr := h.redeliveryCounter.Incr(ctx, messageKey(msg))
	if countErr != nil {
		msg.Nack()
		return errors.Join(err, fmt.Errorf("count redeliveries: %w", countErr))
	}
	if failures <= int64(h.maxRedeliveries) {
		msg.Nack()
		return err
	}
	msg.Reject()
	return fmt.Errorf("rejected message %s after %d redeliveries: %w", msg.LoggableID, h.maxRedeliveries, err)
}

func reject(msg *mqs.Message, err error) error {
	if !msg.Rejectable() {
		msg.Nack()
		return err
	}
	msg.Reject()
	return fmt.Errorf("rejected message %s: %w", msg.LoggableID, err)
}

// messageKey falls back to a body hash for brokers or publishers that set no
// message ID.
func messageKey(msg *mqs.Message) string {
	if msg.ID != "" {
		return msg.ID
	}
	sum := sha256.Sum256(msg.Body)
	return hex.EncodeToString(sum[:])
}

type PublishedEvent struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id" binding:"required"`
	DestinationID    string            `json:"destination_id"`
	Topic            string            `json:"topic"`
	EligibleForRetry *bool             `json:"eligible_for_retry"`
	Time             time.Time         `json:"time"`
	Metadata         map[string]string `json:"metadata"`
	Data             json.RawMessage   `json:"data"`
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
