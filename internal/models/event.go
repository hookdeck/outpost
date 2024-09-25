package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/mqs"
)

type Event struct {
	ID               string                 `json:"id"`
	TenantID         string                 `json:"tenant_id"`
	DestinationID    string                 `json:"destination_id"`
	Topic            string                 `json:"topic"`
	EligibleForRetry bool                   `json:"eligible_for_retry"`
	Time             time.Time              `json:"time"`
	Metadata         map[string]string      `json:"metadata"`
	Data             map[string]interface{} `json:"data"`
}

var _ mqs.IncomingMessage = &Event{}

func (e *Event) FromMessage(msg *mqs.Message) error {
	return json.Unmarshal(msg.Body, e)
}

func (e *Event) ToMessage() (*mqs.Message, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &mqs.Message{Body: data}, nil
}

func (e *Event) ToAdapterEvent() *adapters.Event {
	return &adapters.Event{
		ID:               e.ID,
		TenantID:         e.TenantID,
		DestinationID:    e.DestinationID,
		Topic:            e.Topic,
		EligibleForRetry: e.EligibleForRetry,
		Time:             e.Time,
		Metadata:         e.Metadata,
		Data:             e.Data,
	}
}

type DeliveryEvent struct {
	ID          string
	Event       Event
	Destination Destination
}

var _ mqs.IncomingMessage = &DeliveryEvent{}

func (e *DeliveryEvent) FromMessage(msg *mqs.Message) error {
	return json.Unmarshal(msg.Body, e)
}

func (e *DeliveryEvent) ToMessage() (*mqs.Message, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &mqs.Message{Body: data}, nil
}

func NewDeliveryEvent(event Event, destination Destination) DeliveryEvent {
	return DeliveryEvent{
		ID:          uuid.New().String(),
		Event:       event,
		Destination: destination,
	}
}
