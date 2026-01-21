package pglogstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	cursorResourceEvent    = "evt"
	cursorResourceDelivery = "dlv"
	cursorVersion          = 1
)

type logStore struct {
	db *pgxpool.Pool
}

func NewLogStore(db *pgxpool.Pool) driver.LogStore {
	return &logStore{
		db: db,
	}
}

// eventWithTimeID wraps an event with its time_id for cursor encoding.
type eventWithTimeID struct {
	*models.Event
	TimeID string
}

func (s *logStore) ListEvent(ctx context.Context, req driver.ListEventRequest) (driver.ListEventResponse, error) {
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	res, err := pagination.Run(ctx, pagination.Config[eventWithTimeID]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(ctx context.Context, q pagination.QueryInput) ([]eventWithTimeID, error) {
			query, args := buildEventQuery(req, q)
			rows, err := s.db.Query(ctx, query, args...)
			if err != nil {
				return nil, fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()
			return scanEvents(rows)
		},
		Cursor: pagination.Cursor[eventWithTimeID]{
			Encode: func(e eventWithTimeID) string {
				return cursor.Encode(cursorResourceEvent, cursorVersion, e.TimeID)
			},
			Decode: func(c string) (string, error) {
				return cursor.Decode(c, cursorResourceEvent, cursorVersion)
			},
		},
	})
	if err != nil {
		return driver.ListEventResponse{}, err
	}

	// Extract events from results
	data := make([]*models.Event, len(res.Items))
	for i, item := range res.Items {
		data[i] = item.Event
	}

	return driver.ListEventResponse{
		Data: data,
		Next: res.Next,
		Prev: res.Prev,
	}, nil
}

func buildEventQuery(req driver.ListEventRequest, q pagination.QueryInput) (string, []any) {
	cursorCondition := fmt.Sprintf("AND ($8::text = '' OR time_id %s $8::text)", q.Compare)
	orderByClause := fmt.Sprintf("time %s, id %s", strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

	query := fmt.Sprintf(`
		SELECT
			id,
			tenant_id,
			destination_id,
			time,
			topic,
			eligible_for_retry,
			data,
			metadata,
			time_id
		FROM events
		WHERE ($1::text = '' OR tenant_id = $1)
		AND (array_length($2::text[], 1) IS NULL OR destination_id = ANY($2))
		AND (array_length($3::text[], 1) IS NULL OR topic = ANY($3))
		AND ($4::timestamptz IS NULL OR time >= $4)
		AND ($5::timestamptz IS NULL OR time <= $5)
		AND ($6::timestamptz IS NULL OR time > $6)
		AND ($7::timestamptz IS NULL OR time < $7)
		%s
		ORDER BY %s
		LIMIT $9
	`, cursorCondition, orderByClause)

	args := []any{
		req.TenantID,       // $1
		req.DestinationIDs, // $2
		req.Topics,         // $3
		req.TimeFilter.GTE, // $4
		req.TimeFilter.LTE, // $5
		req.TimeFilter.GT,  // $6
		req.TimeFilter.LT,  // $7
		q.CursorPos,        // $8
		q.Limit,            // $9
	}

	return query, args
}

func scanEvents(rows pgx.Rows) ([]eventWithTimeID, error) {
	var results []eventWithTimeID
	for rows.Next() {
		var (
			id               string
			tenantID         string
			destinationID    string
			eventTime        time.Time
			topic            string
			eligibleForRetry bool
			data             map[string]any
			metadata         map[string]string
			timeID           string
		)

		if err := rows.Scan(
			&id,
			&tenantID,
			&destinationID,
			&eventTime,
			&topic,
			&eligibleForRetry,
			&data,
			&metadata,
			&timeID,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		results = append(results, eventWithTimeID{
			Event: &models.Event{
				ID:               id,
				TenantID:         tenantID,
				DestinationID:    destinationID,
				Topic:            topic,
				EligibleForRetry: eligibleForRetry,
				Time:             eventTime,
				Data:             data,
				Metadata:         metadata,
			},
			TimeID: timeID,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

// deliveryRecordWithTimeID wraps a delivery record with its time_delivery_id for cursor encoding.
type deliveryRecordWithTimeID struct {
	*driver.DeliveryRecord
	TimeDeliveryID string
}

func (s *logStore) ListDelivery(ctx context.Context, req driver.ListDeliveryRequest) (driver.ListDeliveryResponse, error) {
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	res, err := pagination.Run(ctx, pagination.Config[deliveryRecordWithTimeID]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(ctx context.Context, q pagination.QueryInput) ([]deliveryRecordWithTimeID, error) {
			query, args := buildDeliveryQuery(req, q)
			rows, err := s.db.Query(ctx, query, args...)
			if err != nil {
				return nil, fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()
			return scanDeliveryRecords(rows)
		},
		Cursor: pagination.Cursor[deliveryRecordWithTimeID]{
			Encode: func(dr deliveryRecordWithTimeID) string {
				return cursor.Encode(cursorResourceDelivery, cursorVersion, dr.TimeDeliveryID)
			},
			Decode: func(c string) (string, error) {
				return cursor.Decode(c, cursorResourceDelivery, cursorVersion)
			},
		},
	})
	if err != nil {
		return driver.ListDeliveryResponse{}, err
	}

	// Extract delivery records from results
	data := make([]*driver.DeliveryRecord, len(res.Items))
	for i, item := range res.Items {
		data[i] = item.DeliveryRecord
	}

	return driver.ListDeliveryResponse{
		Data: data,
		Next: res.Next,
		Prev: res.Prev,
	}, nil
}

func buildDeliveryQuery(req driver.ListDeliveryRequest, q pagination.QueryInput) (string, []any) {
	cursorCondition := fmt.Sprintf("AND ($10::text = '' OR idx.time_delivery_id %s $10::text)", q.Compare)
	orderByClause := fmt.Sprintf("idx.delivery_time %s, idx.delivery_id %s", strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

	query := fmt.Sprintf(`
		SELECT
			idx.event_id,
			idx.delivery_id,
			idx.destination_id,
			idx.event_time,
			idx.delivery_time,
			idx.topic,
			idx.status,
			idx.time_delivery_id,
			e.tenant_id,
			e.eligible_for_retry,
			e.data,
			e.metadata,
			d.code,
			d.response_data,
			idx.manual,
			idx.attempt
		FROM event_delivery_index idx
		JOIN events e ON e.id = idx.event_id AND e.time = idx.event_time
		JOIN deliveries d ON d.id = idx.delivery_id AND d.time = idx.delivery_time
		WHERE ($1::text = '' OR idx.tenant_id = $1)
		AND ($2::text = '' OR idx.event_id = $2)
		AND (array_length($3::text[], 1) IS NULL OR idx.destination_id = ANY($3))
		AND ($4::text = '' OR idx.status = $4)
		AND (array_length($5::text[], 1) IS NULL OR idx.topic = ANY($5))
		AND ($6::timestamptz IS NULL OR idx.delivery_time >= $6)
		AND ($7::timestamptz IS NULL OR idx.delivery_time <= $7)
		AND ($8::timestamptz IS NULL OR idx.delivery_time > $8)
		AND ($9::timestamptz IS NULL OR idx.delivery_time < $9)
		%s
		ORDER BY %s
		LIMIT $11
	`, cursorCondition, orderByClause)

	args := []any{
		req.TenantID,       // $1
		req.EventID,        // $2
		req.DestinationIDs, // $3
		req.Status,         // $4
		req.Topics,         // $5
		req.TimeFilter.GTE, // $6
		req.TimeFilter.LTE, // $7
		req.TimeFilter.GT,  // $8
		req.TimeFilter.LT,  // $9
		q.CursorPos,        // $10
		q.Limit,            // $11
	}

	return query, args
}

func scanDeliveryRecords(rows pgx.Rows) ([]deliveryRecordWithTimeID, error) {
	var results []deliveryRecordWithTimeID
	for rows.Next() {
		var (
			eventID          string
			deliveryID       string
			destinationID    string
			eventTime        time.Time
			deliveryTime     time.Time
			topic            string
			status           string
			timeDeliveryID   string
			tenantID         string
			eligibleForRetry bool
			data             map[string]any
			metadata         map[string]string
			code             string
			responseData     map[string]any
			manual           bool
			attempt          int
		)

		if err := rows.Scan(
			&eventID,
			&deliveryID,
			&destinationID,
			&eventTime,
			&deliveryTime,
			&topic,
			&status,
			&timeDeliveryID,
			&tenantID,
			&eligibleForRetry,
			&data,
			&metadata,
			&code,
			&responseData,
			&manual,
			&attempt,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		results = append(results, deliveryRecordWithTimeID{
			DeliveryRecord: &driver.DeliveryRecord{
				Delivery: &models.Delivery{
					ID:            deliveryID,
					TenantID:      tenantID,
					EventID:       eventID,
					DestinationID: destinationID,
					Attempt:       attempt,
					Manual:        manual,
					Status:        status,
					Time:          deliveryTime,
					Code:          code,
					ResponseData:  responseData,
				},
				Event: &models.Event{
					ID:               eventID,
					TenantID:         tenantID,
					DestinationID:    destinationID,
					Topic:            topic,
					EligibleForRetry: eligibleForRetry,
					Time:             eventTime,
					Data:             data,
					Metadata:         metadata,
				},
			},
			TimeDeliveryID: timeDeliveryID,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

func (s *logStore) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	var query string
	var args []any

	if req.DestinationID != "" {
		query = `
			SELECT
				e.id,
				e.tenant_id,
				$3 as destination_id,
				e.time,
				e.topic,
				e.eligible_for_retry,
				e.data,
				e.metadata
			FROM events e
			WHERE ($1::text = '' OR e.tenant_id = $1) AND e.id = $2
			AND EXISTS (
				SELECT 1 FROM event_delivery_index idx
				WHERE ($1::text = '' OR idx.tenant_id = $1) AND idx.event_id = $2 AND idx.destination_id = $3
			)`
		args = []any{req.TenantID, req.EventID, req.DestinationID}
	} else {
		query = `
			SELECT
				e.id,
				e.tenant_id,
				e.destination_id,
				e.time,
				e.topic,
				e.eligible_for_retry,
				e.data,
				e.metadata
			FROM events e
			WHERE ($1::text = '' OR e.tenant_id = $1) AND e.id = $2`
		args = []any{req.TenantID, req.EventID}
	}

	row := s.db.QueryRow(ctx, query, args...)

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

func (s *logStore) RetrieveDelivery(ctx context.Context, req driver.RetrieveDeliveryRequest) (*driver.DeliveryRecord, error) {
	query := `
		SELECT
			idx.event_id,
			idx.delivery_id,
			idx.destination_id,
			idx.event_time,
			idx.delivery_time,
			idx.topic,
			idx.status,
			e.tenant_id,
			e.eligible_for_retry,
			e.data,
			e.metadata,
			d.code,
			d.response_data,
			idx.manual,
			idx.attempt
		FROM event_delivery_index idx
		JOIN events e ON e.id = idx.event_id AND e.time = idx.event_time
		JOIN deliveries d ON d.id = idx.delivery_id AND d.time = idx.delivery_time
		WHERE ($1::text = '' OR idx.tenant_id = $1) AND idx.delivery_id = $2
		LIMIT 1`

	row := s.db.QueryRow(ctx, query, req.TenantID, req.DeliveryID)

	var (
		eventID          string
		deliveryID       string
		destinationID    string
		eventTime        time.Time
		deliveryTime     time.Time
		topic            string
		status           string
		tenantID         string
		eligibleForRetry bool
		data             map[string]any
		metadata         map[string]string
		code             string
		responseData     map[string]any
		manual           bool
		attempt          int
	)

	err := row.Scan(
		&eventID,
		&deliveryID,
		&destinationID,
		&eventTime,
		&deliveryTime,
		&topic,
		&status,
		&tenantID,
		&eligibleForRetry,
		&data,
		&metadata,
		&code,
		&responseData,
		&manual,
		&attempt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return &driver.DeliveryRecord{
		Delivery: &models.Delivery{
			ID:            deliveryID,
			TenantID:      tenantID,
			EventID:       eventID,
			DestinationID: destinationID,
			Attempt:       attempt,
			Manual:        manual,
			Status:        status,
			Time:          deliveryTime,
			Code:          code,
			ResponseData:  responseData,
		},
		Event: &models.Event{
			ID:               eventID,
			TenantID:         tenantID,
			DestinationID:    destinationID,
			Topic:            topic,
			EligibleForRetry: eligibleForRetry,
			Time:             eventTime,
			Data:             data,
			Metadata:         metadata,
		},
	}, nil
}

func (s *logStore) InsertMany(ctx context.Context, entries []*models.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Extract and dedupe events by ID
	eventMap := make(map[string]*models.Event)
	for _, entry := range entries {
		eventMap[entry.Event.ID] = entry.Event
	}
	events := make([]*models.Event, 0, len(eventMap))
	for _, e := range eventMap {
		events = append(events, e)
	}

	// Extract deliveries
	deliveries := make([]*models.Delivery, 0, len(entries))
	for _, entry := range entries {
		deliveries = append(deliveries, entry.Delivery)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if len(events) > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[], $5::text[], $6::boolean[], $7::jsonb[], $8::jsonb[])
			ON CONFLICT (time, id) DO NOTHING
		`, eventArrays(events)...)
		if err != nil {
			return err
		}
	}

	if len(deliveries) > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO deliveries (id, event_id, destination_id, status, time, code, response_data, manual, attempt)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::text[], $7::jsonb[], $8::boolean[], $9::integer[])
			ON CONFLICT (time, id) DO UPDATE SET
				status = EXCLUDED.status,
				code = EXCLUDED.code,
				response_data = EXCLUDED.response_data
		`, deliveryArrays(deliveries)...)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO event_delivery_index (
				event_id, delivery_id, tenant_id, destination_id,
				event_time, delivery_time, topic, status, manual, attempt
			)
			SELECT
				d.event_id,
				d.id,
				e.tenant_id,
				d.destination_id,
				e.time,
				d.time,
				e.topic,
				d.status,
				d.manual,
				d.attempt
			FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::text[], $7::jsonb[], $8::boolean[], $9::integer[])
				AS d(id, event_id, destination_id, status, time, code, response_data, manual, attempt)
			JOIN events e ON e.id = d.event_id
			ON CONFLICT (delivery_time, event_id, delivery_id) DO UPDATE SET
				status = EXCLUDED.status
		`, deliveryArrays(deliveries)...)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func eventArrays(events []*models.Event) []any {
	ids := make([]string, len(events))
	tenantIDs := make([]string, len(events))
	destinationIDs := make([]string, len(events))
	times := make([]time.Time, len(events))
	topics := make([]string, len(events))
	eligibleForRetries := make([]bool, len(events))
	datas := make([]map[string]any, len(events))
	metadatas := make([]map[string]string, len(events))

	for i, e := range events {
		ids[i] = e.ID
		tenantIDs[i] = e.TenantID
		destinationIDs[i] = e.DestinationID
		times[i] = e.Time
		topics[i] = e.Topic
		eligibleForRetries[i] = e.EligibleForRetry
		datas[i] = e.Data
		metadatas[i] = e.Metadata
	}

	return []any{
		ids,
		tenantIDs,
		destinationIDs,
		times,
		topics,
		eligibleForRetries,
		datas,
		metadatas,
	}
}

func deliveryArrays(deliveries []*models.Delivery) []any {
	ids := make([]string, len(deliveries))
	eventIDs := make([]string, len(deliveries))
	destinationIDs := make([]string, len(deliveries))
	statuses := make([]string, len(deliveries))
	times := make([]time.Time, len(deliveries))
	codes := make([]string, len(deliveries))
	responseDatas := make([]map[string]any, len(deliveries))
	manuals := make([]bool, len(deliveries))
	attempts := make([]int, len(deliveries))

	for i, d := range deliveries {
		ids[i] = d.ID
		eventIDs[i] = d.EventID
		destinationIDs[i] = d.DestinationID
		statuses[i] = d.Status
		times[i] = d.Time
		codes[i] = d.Code
		responseDatas[i] = d.ResponseData
		manuals[i] = d.Manual
		attempts[i] = d.Attempt
	}

	return []any{
		ids,
		eventIDs,
		destinationIDs,
		statuses,
		times,
		codes,
		responseDatas,
		manuals,
		attempts,
	}
}
