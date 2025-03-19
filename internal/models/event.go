package models

import (
	"encoding"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
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

type Metadata map[string]string

var _ fmt.Stringer = &Metadata{}
var _ encoding.BinaryUnmarshaler = &Metadata{}

func (m *Metadata) String() string {
	metadata, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(metadata)
}

func (m *Metadata) UnmarshalBinary(metadata []byte) error {
	if string(metadata) == "" {
		return nil
	}
	return json.Unmarshal(metadata, m)
}

type EventTelemetry struct {
	TraceID      string
	SpanID       string
	ReceivedTime string // format time.RFC3339Nano
}

type Event struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	DestinationID    string    `json:"destination_id"`
	Topic            string    `json:"topic"`
	EligibleForRetry bool      `json:"eligible_for_retry"`
	Time             time.Time `json:"time"`
	Metadata         Metadata  `json:"metadata"`
	Data             Data      `json:"data"`
	Status           string    `json:"status,omitempty"`

	// Telemetry data, must exist to properly trace events between publish receiver & delivery handler
	Telemetry *EventTelemetry `json:"telemetry,omitempty"`
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

type DeliveryEventTelemetry struct {
	TraceID string
	SpanID  string
}

type DeliveryEvent struct {
	ID            string
	Attempt       int
	DestinationID string
	Event         Event
	Delivery      *Delivery
	Telemetry     *DeliveryEventTelemetry
	Manual        bool // Indicates if this is a manual retry
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

// GetRetryID returns the ID used for scheduling retries.
// We use Event.ID instead of DeliveryEvent.ID because:
// 1. Each event should only have one scheduled retry at a time
// 2. Event.ID is always accessible, while DeliveryEvent.ID may require additional queries in retry scenarios
func (e *DeliveryEvent) GetRetryID() string {
	return e.Event.ID
}

func NewDeliveryEvent(event Event, destinationID string) DeliveryEvent {
	return DeliveryEvent{
		ID:            uuid.New().String(),
		DestinationID: destinationID,
		Event:         event,
		Attempt:       0,
	}
}

func NewManualDeliveryEvent(event Event, destinationID string) DeliveryEvent {
	deliveryEvent := NewDeliveryEvent(event, destinationID)
	deliveryEvent.Manual = true
	return deliveryEvent
}

const (
	DeliveryStatusSuccess = "success"
	DeliveryStatusFailed  = "failed"
)

type Delivery struct {
	ID              string                 `json:"id"`
	DeliveryEventID string                 `json:"delivery_event_id"`
	EventID         string                 `json:"event_id"`
	DestinationID   string                 `json:"destination_id"`
	Status          string                 `json:"status"`
	Time            time.Time              `json:"time"`
	Code            string                 `json:"code"`
	ResponseData    map[string]interface{} `json:"response_data"`
}
