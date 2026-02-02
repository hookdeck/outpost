package models

import (
	"encoding/json"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/mqs"
)

type EventTelemetry struct {
	TraceID      string
	SpanID       string
	ReceivedTime string // format time.RFC3339Nano
}

type DeliveryTelemetry struct {
	TraceID string
	SpanID  string
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

// DeliveryTask represents a task to deliver an event to a destination.
// This is a message type (no ID) used by: publishmq -> deliverymq, retry -> deliverymq
type DeliveryTask struct {
	Event         Event              `json:"event"`
	DestinationID string             `json:"destination_id"`
	Attempt       int                `json:"attempt"`
	Manual        bool               `json:"manual"`
	Nonce         string             `json:"nonce,omitempty"`
	Telemetry     *DeliveryTelemetry `json:"telemetry,omitempty"`
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
// Manual retries include a nonce so each /retry request gets its own idempotency key,
// while MQ redeliveries of the same message (same nonce) are still deduplicated.
// Nonce was added to fix a regression from #653 where removing DeliveryEvent.ID
// made the manual retry idempotency key static per event+destination.
func (t *DeliveryTask) IdempotencyKey() string {
	if t.Manual {
		return t.Event.ID + ":" + t.DestinationID + ":manual:" + t.Nonce
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
// Each manual retry gets a unique nonce so separate /retry requests are not deduplicated.
func NewManualDeliveryTask(event Event, destinationID string) DeliveryTask {
	task := NewDeliveryTask(event, destinationID)
	task.Manual = true
	task.Nonce = idgen.String()
	return task
}

// LogEntry represents a message for the log queue.
//
// IMPORTANT: Both Event and Attempt are REQUIRED. The logstore requires both
// to exist for proper data consistency. The logmq consumer validates this
// requirement and rejects entries missing either field.
type LogEntry struct {
	Event   *Event   `json:"event"`
	Attempt *Attempt `json:"attempt"`
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
