package models

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/clickhouse"
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

type Event struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id"`
	DestinationID    string            `json:"destination_id"`
	Topic            string            `json:"topic"`
	EligibleForRetry bool              `json:"eligible_for_retry"`
	Time             time.Time         `json:"time"`
	Metadata         map[string]string `json:"metadata"`
	Data             Data              `json:"data"`
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
	Metadata    map[string]string
	Event       Event
	Destination Destination
	Delivery    *Delivery
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
		Metadata:    map[string]string{},
		Event:       event,
		Destination: destination,
	}
}

// ============================== Model ==============================

type EventModel struct{}

func NewEventModel() *EventModel {
	return &EventModel{}
}

func (m *EventModel) InsertMany(ctx context.Context, conn clickhouse.DB, events []*Event) error {
	batch, err := conn.PrepareBatch(ctx,
		"INSERT INTO eventkit.events (id, tenant_id, destination_id, time, topic, data) VALUES (?, ?, ?, ?, ?)",
	)
	if err != nil {
		return err
	}

	for _, event := range events {
		if err := batch.Append(
			&event.ID,
			&event.TenantID,
			&event.DestinationID,
			&event.Time,
			&event.Topic,
			&event.Data,
		); err != nil {
			return err
		}
	}

	if err := batch.Send(); err != nil {
		return err
	}

	return nil
}

func (m *EventModel) List(ctx context.Context, conn clickhouse.DB) ([]*Event, error) {
	rows, err := conn.Query(ctx, `
		SELECT
			id,
			tenant_id,
			destination_id,
			time,
			topic,
			data
		FROM eventkit.events
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		event := &Event{}
		if err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.DestinationID,
			&event.Time,
			&event.Topic,
			&event.Data,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
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

type DeliveryModel struct{}

func NewDeliveryModel() *DeliveryModel {
	return &DeliveryModel{}
}

func (m *DeliveryModel) InsertMany(ctx context.Context, conn clickhouse.DB, delivery []*Delivery) error {
	batch, err := conn.PrepareBatch(ctx,
		"INSERT INTO eventkit.deliveries (id, delivery_event_id, event_id, destination_id, status, time) VALUES (?, ?, ?, ?, ?, ?)",
	)
	if err != nil {
		return err
	}

	for _, d := range delivery {
		if err := batch.Append(
			&d.ID,
			&d.DeliveryEventID,
			&d.EventID,
			&d.DestinationID,
			&d.Status,
			&d.Time,
		); err != nil {
			return err
		}
	}

	if err := batch.Send(); err != nil {
		return err
	}

	return nil
}
