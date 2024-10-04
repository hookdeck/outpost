package models

import (
	"context"

	"github.com/hookdeck/EventKit/internal/clickhouse"
)

type LogRepo interface {
	ListEvent(ctx context.Context) ([]*Event, error)
	InsertManyEvent(ctx context.Context, events []*Event) error
	InsertManyDelivery(ctx context.Context, deliveries []*Delivery) error
}

type logImpl struct {
	chDB clickhouse.DB
}

var _ LogRepo = (*logImpl)(nil)

func NewLogRepo(chDB clickhouse.DB) LogRepo {
	return &logImpl{chDB: chDB}
}

func (l *logImpl) ListEvent(ctx context.Context) ([]*Event, error) {
	rows, err := l.chDB.Query(ctx, `
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

func (l *logImpl) InsertManyEvent(ctx context.Context, events []*Event) error {
	batch, err := l.chDB.PrepareBatch(ctx,
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

func (l *logImpl) InsertManyDelivery(ctx context.Context, deliveries []*Delivery) error {
	batch, err := l.chDB.PrepareBatch(ctx,
		"INSERT INTO eventkit.deliveries (id, delivery_event_id, event_id, destination_id, status, time) VALUES (?, ?, ?, ?, ?, ?)",
	)
	if err != nil {
		return err
	}

	for _, delivery := range deliveries {
		if err := batch.Append(
			&delivery.ID,
			&delivery.DeliveryEventID,
			&delivery.EventID,
			&delivery.DestinationID,
			&delivery.Status,
			&delivery.Time,
		); err != nil {
			return err
		}
	}

	if err := batch.Send(); err != nil {
		return err
	}

	return nil
}
