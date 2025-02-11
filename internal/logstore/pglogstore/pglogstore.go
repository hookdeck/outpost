package pglogstore

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type logStore struct {
	db *pgxpool.Pool
}

func NewLogStore(db *pgxpool.Pool) driver.LogStore {
	return &logStore{db: db}
}

func (s *logStore) ListEvent(ctx context.Context, req driver.ListEventRequest) ([]*models.Event, string, error) {
	query := `
		SELECT id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata
		FROM events 
		WHERE tenant_id = $1 
		AND ($2 = '' OR time < $2::timestamptz)
		ORDER BY time DESC
		LIMIT $3`

	rows, err := s.db.Query(ctx, query, req.TenantID, req.Cursor, req.Limit)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var events []*models.Event
	for rows.Next() {
		event := &models.Event{}
		err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.DestinationID,
			&event.Time,
			&event.Topic,
			&event.EligibleForRetry,
			&event.Data,
			&event.Metadata,
		)
		if err != nil {
			return nil, "", err
		}
		events = append(events, event)
	}

	var nextCursor string
	if len(events) > 0 {
		nextCursor = events[len(events)-1].Time.UTC().Format(time.RFC3339)
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
		SELECT id, event_id, destination_id, status, time
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
		[]string{"id", "event_id", "destination_id", "status", "time"},
		pgx.CopyFromSlice(len(deliveries), func(i int) ([]interface{}, error) {
			return []interface{}{
				deliveries[i].ID,
				deliveries[i].EventID,
				deliveries[i].DestinationID,
				deliveries[i].Status,
				deliveries[i].Time,
			}, nil
		}),
	)
	return err
}
