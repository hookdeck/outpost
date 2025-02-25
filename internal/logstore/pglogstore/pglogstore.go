package pglogstore

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type logStore struct {
	db           *pgxpool.Pool
	cursorParser eventCursorParser
}

func NewLogStore(db *pgxpool.Pool) driver.LogStore {
	return &logStore{
		db:           db,
		cursorParser: newEventCursorParser(),
	}
}

func (s *logStore) ListEvent(ctx context.Context, req driver.ListEventRequest) ([]*models.Event, string, error) {
	query := `
		SELECT 
			id,
			tenant_id,
			destination_id,
			time,
			topic,
			eligible_for_retry,
			data,
			metadata,
			time_id,
			COALESCE(
				NULLIF($3, ''),
				CASE 
					WHEN EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id AND d.status = 'success') THEN 'success'
					WHEN EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id) THEN 'failed'
					ELSE 'pending'
				END
			) as status
		FROM events e
		WHERE tenant_id = $1
		AND (array_length($2::text[], 1) IS NULL OR destination_id = ANY($2))
		AND ($4 = '' OR time_id < $4)
		AND ($3 = '' OR 
			CASE $3
				WHEN 'success' THEN EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id AND d.status = 'success')
				WHEN 'failed' THEN EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id) AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id AND d.status = 'success')
				WHEN 'pending' THEN NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id)
			END
		)
		ORDER BY time_id DESC
		LIMIT CASE WHEN $5 = 0 THEN NULL ELSE $5 END`

	var cursor string
	if req.Cursor != "" {
		decodedCursor, err := s.cursorParser.Parse(req.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %v", err)
		}
		cursor = decodedCursor
	}

	rows, err := s.db.Query(ctx, query,
		req.TenantID,
		req.DestinationIDs,
		req.Status,
		cursor,
		req.Limit,
	)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var events []*models.Event
	var lastTimeID string
	for rows.Next() {
		event := &models.Event{}
		var status string
		err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.DestinationID,
			&event.Time,
			&event.Topic,
			&event.EligibleForRetry,
			&event.Data,
			&event.Metadata,
			&lastTimeID,
			&status,
		)
		if err != nil {
			return nil, "", err
		}
		events = append(events, event)
	}

	var nextCursor string
	if len(events) > 0 {
		nextCursor = s.cursorParser.Format(lastTimeID)
	}

	return events, nextCursor, nil
}

func (s *logStore) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	query := `
		SELECT id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata
		FROM events
		WHERE tenant_id = $1 AND id = $2`

	row := s.db.QueryRow(ctx, query, tenantID, eventID)

	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Time,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Data,
		&event.Metadata,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return event, nil
}

func (s *logStore) ListDelivery(ctx context.Context, req driver.ListDeliveryRequest) ([]*models.Delivery, error) {
	query := `
		SELECT id, event_id, destination_id, status, time, code, response_data
		FROM deliveries
		WHERE event_id = $1
		ORDER BY time DESC`

	rows, err := s.db.Query(ctx, query, req.EventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []*models.Delivery
	for rows.Next() {
		delivery := &models.Delivery{}
		err := rows.Scan(
			&delivery.ID,
			&delivery.EventID,
			&delivery.DestinationID,
			&delivery.Status,
			&delivery.Time,
			&delivery.Code,
			&delivery.ResponseData,
		)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}

	return deliveries, nil
}

func (s *logStore) InsertManyEvent(ctx context.Context, events []*models.Event) error {
	_, err := s.db.CopyFrom(
		ctx,
		pgx.Identifier{"events"},
		[]string{"id", "tenant_id", "destination_id", "time", "topic", "eligible_for_retry", "data", "metadata"},
		pgx.CopyFromSlice(len(events), func(i int) ([]interface{}, error) {
			return []interface{}{
				events[i].ID,
				events[i].TenantID,
				events[i].DestinationID,
				events[i].Time,
				events[i].Topic,
				events[i].EligibleForRetry,
				events[i].Data,
				events[i].Metadata,
			}, nil
		}),
	)
	return err
}

func (s *logStore) InsertManyDelivery(ctx context.Context, deliveries []*models.Delivery) error {
	_, err := s.db.CopyFrom(
		ctx,
		pgx.Identifier{"deliveries"},
		[]string{"id", "event_id", "destination_id", "status", "time", "code", "response_data"},
		pgx.CopyFromSlice(len(deliveries), func(i int) ([]interface{}, error) {
			return []interface{}{
				deliveries[i].ID,
				deliveries[i].EventID,
				deliveries[i].DestinationID,
				deliveries[i].Status,
				deliveries[i].Time,
				deliveries[i].Code,
				deliveries[i].ResponseData,
			}, nil
		}),
	)
	return err
}
