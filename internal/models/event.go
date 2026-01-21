package models

import (
	"encoding"
	"encoding/json"
	"fmt"
	"time"

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

// DeliveryTask represents a task to deliver an event to a destination.
// This is a message type (no ID) used by: publishmq -> deliverymq, retry -> deliverymq
type DeliveryTask struct {
	Event         Event                   `json:"event"`
	DestinationID string                  `json:"destination_id"`
	Attempt       int                     `json:"attempt"`
	Manual        bool                    `json:"manual"`
	Telemetry     *DeliveryEventTelemetry `json:"telemetry,omitempty"`
}

var _ mqs.IncomingMessage = &DeliveryTask{}

func (t *DeliveryTask) FromMessage(msg *mqs.Message) error {
	return json.Unmarshal(msg.Body, t)
}

func (t *DeliveryTask) ToMessage() (*mqs.Message, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return &mqs.Message{Body: data}, nil
}

// IdempotencyKey returns the key used for idempotency checks.
// Uses Event.ID + DestinationID + Manual flag.
// Manual retries get a different key so they can bypass idempotency of failed automatic deliveries.
func (t *DeliveryTask) IdempotencyKey() string {
	if t.Manual {
		return t.Event.ID + ":" + t.DestinationID + ":manual"
	}
	return t.Event.ID + ":" + t.DestinationID
}

// RetryID returns the ID used for scheduling and canceling retries.
// Uses event_id:destination_id to allow manual retries to cancel pending automatic retries.
func RetryID(eventID, destinationID string) string {
	return eventID + ":" + destinationID
}

// NewDeliveryTask creates a new DeliveryTask for an event and destination.
func NewDeliveryTask(event Event, destinationID string) DeliveryTask {
	return DeliveryTask{
		Event:         event,
		DestinationID: destinationID,
		Attempt:       0,
	}
}

// NewManualDeliveryTask creates a new DeliveryTask for a manual retry.
func NewManualDeliveryTask(event Event, destinationID string) DeliveryTask {
	task := NewDeliveryTask(event, destinationID)
	task.Manual = true
	return task
}

const (
	DeliveryStatusSuccess = "success"
	DeliveryStatusFailed  = "failed"
)

// LogEntry represents a message for the log queue.
//
// IMPORTANT: Both Event and Delivery are REQUIRED. The logstore requires both
// to exist for proper data consistency. The logmq consumer validates this
// requirement and rejects entries missing either field.
type LogEntry struct {
	Event    *Event    `json:"event"`
	Delivery *Delivery `json:"delivery"`
}

var _ mqs.IncomingMessage = &LogEntry{}

func (e *LogEntry) FromMessage(msg *mqs.Message) error {
	return json.Unmarshal(msg.Body, e)
}

func (e *LogEntry) ToMessage() (*mqs.Message, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &mqs.Message{Body: data}, nil
}

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
