package models

import (
	"encoding"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
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

type Metadata = MapStringString

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
//
// We use Event.ID + DestinationID (not DeliveryEvent.ID) because:
//  1. Multiple destinations: The same event can be delivered to multiple destinations.
//     Each needs its own retry, so we include DestinationID to avoid collisions.
//  2. Manual retry cancellation: When a manual retry succeeds, it must cancel any
//     pending automatic retry. Manual retries create a NEW DeliveryEvent with a NEW ID,
//     but share the same Event.ID + DestinationID. This allows Cancel() to find and
//     remove the pending automatic retry.
func (e *DeliveryEvent) GetRetryID() string {
	return e.Event.ID + ":" + e.DestinationID
}

func NewDeliveryEvent(event Event, destinationID string) DeliveryEvent {
	return DeliveryEvent{
		ID:            idgen.DeliveryEvent(),
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
	ID            string                 `json:"id"`
	TenantID      string                 `json:"tenant_id"`
	EventID       string                 `json:"event_id"`
	DestinationID string                 `json:"destination_id"`
	Attempt       int                    `json:"attempt"`
	Manual        bool                   `json:"manual"`
	Status        string                 `json:"status"`
	Time          time.Time              `json:"time"`
	Code          string                 `json:"code"`
	ResponseData  map[string]interface{} `json:"response_data"`
}
