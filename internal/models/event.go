package models

import (
	"encoding"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/mqs"
)

type Data map[string]interface{}

var _ fmt.Stringer = &Data{}
var _ encoding.BinaryUnmarshaler = &Data{}

func (d *Data) String() string {
	data, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(data)
}

func (d *Data) UnmarshalBinary(data []byte) error {
	if string(data) == "" {
		return nil
	}
	return json.Unmarshal(data, d)
}

type EventTelemetry struct {
	TraceID      string
	SpanID       string
	ReceivedTime string // format time.RFC3339Nano
}

type Event struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id"`
	DestinationID    string            `json:"destination_id"`
	Topic            string            `json:"topic"`
	EligibleForRetry bool              `json:"eligible_for_retry"`
	Time             time.Time         `json:"time"`
	Metadata         map[string]string `json:"metadata"`
	Data             Data              `json:"data"`
	Telemetry        *EventTelemetry
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

type DeliveryEventTelemetry struct {
	TraceID string
	SpanID  string
}

type DeliveryEvent struct {
	ID            string
	DestinationID string
	Event         Event
	Delivery      *Delivery
	Telemetry     *DeliveryEventTelemetry
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

func NewDeliveryEvent(event Event, destinationID string) DeliveryEvent {
	return DeliveryEvent{
		ID:            uuid.New().String(),
		DestinationID: destinationID,
		Event:         event,
	}
}

const (
	DeliveryStatusOK     = "ok"
	DeliveryStatusFailed = "failed"
)

type Delivery struct {
	ID              string
	DeliveryEventID string
	EventID         string
	DestinationID   string
	Status          string
	Time            time.Time
}
