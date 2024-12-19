package models

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
)

type LogStore interface {
	ListEvent(context.Context, ListEventRequest) ([]*Event, string, error)
	RetrieveEvent(ctx context.Context, tenantID, eventID string) (*Event, error)
	ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*Delivery, error)
	InsertManyEvent(context.Context, []*Event) error
	InsertManyDelivery(context.Context, []*Delivery) error
}

type logStoreImpl struct {
	chDB clickhouse.DB
}

var _ LogStore = (*logStoreImpl)(nil)

func NewLogStore(chDB clickhouse.DB) LogStore {
	return &logStoreImpl{chDB: chDB}
}

type ListEventRequest struct {
	TenantID string
	Cursor   string
	Limit    int
}

func (s *logStoreImpl) ListEvent(ctx context.Context, request ListEventRequest) ([]*Event, string, error) {
	var (
		query     string
		queryOpts []any
	)

	var cursor string
	if cursorTime, err := time.Parse(time.RFC3339, request.Cursor); err == nil {
		cursor = cursorTime.Format("2006-01-02T15:04:05") // RFC3339 without timezone
	}

	if cursor == "" {
		query = `
			SELECT
				id,
				tenant_id,
				destination_id,
				time,
				topic,
				eligible_for_retry,
				data,
				metadata
			FROM events
			WHERE tenant_id = ?
			ORDER BY time DESC
			LIMIT ?
		`
		queryOpts = []any{request.TenantID, request.Limit}
	} else {
		query = `
			SELECT
				id,
				tenant_id,
				destination_id,
				time,
				topic,
				eligible_for_retry,
				data,
				metadata
			FROM events
			WHERE tenant_id = ? AND time < ?
			ORDER BY time DESC
			LIMIT ?
		`
		queryOpts = []any{request.TenantID, cursor, request.Limit}
	}
	rows, err := s.chDB.Query(ctx, query, queryOpts...)
	if err != nil {
		return nil, "", err
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
			&event.EligibleForRetry,
			&event.Data,
			&event.Metadata,
		); err != nil {
			return nil, "", err
		}
		events = append(events, event)
	}
	var nextCursor string
	if len(events) > 0 {
		nextCursor = events[len(events)-1].Time.Format(time.RFC3339)
	}

	return events, nextCursor, nil
}

func (s *logStoreImpl) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*Event, error) {
	rows, err := s.chDB.Query(ctx, `
		SELECT
			id,
			tenant_id,
			destination_id,
			time,
			topic,
			eligible_for_retry,
			data,
			metadata
		FROM events
		WHERE tenant_id = ? AND id = ?
		`, tenantID, eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	event := &Event{}
	if err := rows.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Time,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Data,
		&event.Metadata,
	); err != nil {
		return nil, err
	}

	return event, nil
}

type ListDeliveryRequest struct {
	EventID string
}

func (s *logStoreImpl) ListDelivery(ctx context.Context, request ListDeliveryRequest) ([]*Delivery, error) {
	query := `
		SELECT
			id,
			event_id,
			destination_id,
			status,
			time
		FROM deliveries
		WHERE event_id = ?
		ORDER BY time DESC
	`
	rows, err := s.chDB.Query(ctx, query, request.EventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []*Delivery
	for rows.Next() {
		delivery := &Delivery{}
		if err := rows.Scan(
			&delivery.ID,
			&delivery.EventID,
			&delivery.DestinationID,
			&delivery.Status,
			&delivery.Time,
		); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}

	return deliveries, nil
}

func (s *logStoreImpl) InsertManyEvent(ctx context.Context, events []*Event) error {
	batch, err := s.chDB.PrepareBatch(ctx,
		"INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, metadata, data) VALUES (?, ?, ?, ?, ?, ?)",
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
			&event.EligibleForRetry,
			&event.Metadata,
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

func (s *logStoreImpl) InsertManyDelivery(ctx context.Context, deliveries []*Delivery) error {
	batch, err := s.chDB.PrepareBatch(ctx,
		"INSERT INTO deliveries (id, delivery_event_id, event_id, destination_id, status, time) VALUES (?, ?, ?, ?, ?, ?)",
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
