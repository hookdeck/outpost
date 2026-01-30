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
	cursorResourceEvent   = "evt"
	cursorResourceAttempt = "att"
	cursorVersion         = 1
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

// attemptRecordWithTimeID wraps an attempt record with its time_attempt_id for cursor encoding.
type attemptRecordWithTimeID struct {
	*driver.AttemptRecord
	TimeAttemptID string
}

func (s *logStore) ListAttempt(ctx context.Context, req driver.ListAttemptRequest) (driver.ListAttemptResponse, error) {
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	res, err := pagination.Run(ctx, pagination.Config[attemptRecordWithTimeID]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(ctx context.Context, q pagination.QueryInput) ([]attemptRecordWithTimeID, error) {
			query, args := buildAttemptQuery(req, q)
			rows, err := s.db.Query(ctx, query, args...)
			if err != nil {
				return nil, fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()
			return scanAttemptRecords(rows)
		},
		Cursor: pagination.Cursor[attemptRecordWithTimeID]{
			Encode: func(ar attemptRecordWithTimeID) string {
				return cursor.Encode(cursorResourceAttempt, cursorVersion, ar.TimeAttemptID)
			},
			Decode: func(c string) (string, error) {
				return cursor.Decode(c, cursorResourceAttempt, cursorVersion)
			},
		},
	})
	if err != nil {
		return driver.ListAttemptResponse{}, err
	}

	// Extract attempt records from results
	data := make([]*driver.AttemptRecord, len(res.Items))
	for i, item := range res.Items {
		data[i] = item.AttemptRecord
	}

	return driver.ListAttemptResponse{
		Data: data,
		Next: res.Next,
		Prev: res.Prev,
	}, nil
}

func buildAttemptQuery(req driver.ListAttemptRequest, q pagination.QueryInput) (string, []any) {
	cursorCondition := fmt.Sprintf("AND ($10::text = '' OR idx.time_attempt_id %s $10::text)", q.Compare)
	orderByClause := fmt.Sprintf("idx.attempt_time %s, idx.attempt_id %s", strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

	query := fmt.Sprintf(`
		SELECT
			idx.event_id,
			idx.attempt_id,
			idx.destination_id,
			idx.event_time,
			idx.attempt_time,
			idx.topic,
			idx.status,
			idx.time_attempt_id,
			e.tenant_id,
			e.eligible_for_retry,
			e.data,
			e.metadata,
			a.code,
			a.response_data,
			idx.manual,
			idx.attempt_number
		FROM event_attempt_index idx
		JOIN events e ON e.id = idx.event_id AND e.time = idx.event_time
		JOIN attempts a ON a.id = idx.attempt_id AND a.time = idx.attempt_time
		WHERE ($1::text = '' OR idx.tenant_id = $1)
		AND ($2::text = '' OR idx.event_id = $2)
		AND (array_length($3::text[], 1) IS NULL OR idx.destination_id = ANY($3))
		AND ($4::text = '' OR idx.status = $4)
		AND (array_length($5::text[], 1) IS NULL OR idx.topic = ANY($5))
		AND ($6::timestamptz IS NULL OR idx.attempt_time >= $6)
		AND ($7::timestamptz IS NULL OR idx.attempt_time <= $7)
		AND ($8::timestamptz IS NULL OR idx.attempt_time > $8)
		AND ($9::timestamptz IS NULL OR idx.attempt_time < $9)
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

func scanAttemptRecords(rows pgx.Rows) ([]attemptRecordWithTimeID, error) {
	var results []attemptRecordWithTimeID
	for rows.Next() {
		var (
			eventID          string
			attemptID        string
			destinationID    string
			eventTime        time.Time
			attemptTime      time.Time
			topic            string
			status           string
			timeAttemptID    string
			tenantID         string
			eligibleForRetry bool
			data             map[string]any
			metadata         map[string]string
			code             string
			responseData     map[string]any
			manual           bool
			attemptNumber    int
		)

		if err := rows.Scan(
			&eventID,
			&attemptID,
			&destinationID,
			&eventTime,
			&attemptTime,
			&topic,
			&status,
			&timeAttemptID,
			&tenantID,
			&eligibleForRetry,
			&data,
			&metadata,
			&code,
			&responseData,
			&manual,
			&attemptNumber,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		results = append(results, attemptRecordWithTimeID{
			AttemptRecord: &driver.AttemptRecord{
				Attempt: &models.Attempt{
					ID:            attemptID,
					TenantID:      tenantID,
					EventID:       eventID,
					DestinationID: destinationID,
					AttemptNumber: attemptNumber,
					Manual:        manual,
					Status:        status,
					Time:          attemptTime,
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
			TimeAttemptID: timeAttemptID,
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
				SELECT 1 FROM event_attempt_index idx
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

func (s *logStore) RetrieveAttempt(ctx context.Context, req driver.RetrieveAttemptRequest) (*driver.AttemptRecord, error) {
	query := `
		SELECT
			idx.event_id,
			idx.attempt_id,
			idx.destination_id,
			idx.event_time,
			idx.attempt_time,
			idx.topic,
			idx.status,
			e.tenant_id,
			e.eligible_for_retry,
			e.data,
			e.metadata,
			a.code,
			a.response_data,
			idx.manual,
			idx.attempt_number
		FROM event_attempt_index idx
		JOIN events e ON e.id = idx.event_id AND e.time = idx.event_time
		JOIN attempts a ON a.id = idx.attempt_id AND a.time = idx.attempt_time
		WHERE ($1::text = '' OR idx.tenant_id = $1) AND idx.attempt_id = $2
		LIMIT 1`

	row := s.db.QueryRow(ctx, query, req.TenantID, req.AttemptID)

	var (
		eventID          string
		attemptID        string
		destinationID    string
		eventTime        time.Time
		attemptTime      time.Time
		topic            string
		status           string
		tenantID         string
		eligibleForRetry bool
		data             map[string]any
		metadata         map[string]string
		code             string
		responseData     map[string]any
		manual           bool
		attemptNumber    int
	)

	err := row.Scan(
		&eventID,
		&attemptID,
		&destinationID,
		&eventTime,
		&attemptTime,
		&topic,
		&status,
		&tenantID,
		&eligibleForRetry,
		&data,
		&metadata,
		&code,
		&responseData,
		&manual,
		&attemptNumber,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return &driver.AttemptRecord{
		Attempt: &models.Attempt{
			ID:            attemptID,
			TenantID:      tenantID,
			EventID:       eventID,
			DestinationID: destinationID,
			AttemptNumber: attemptNumber,
			Manual:        manual,
			Status:        status,
			Time:          attemptTime,
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

	// Extract attempts
	attempts := make([]*models.Attempt, 0, len(entries))
	for _, entry := range entries {
		attempts = append(attempts, entry.Attempt)
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

	if len(attempts) > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO attempts (id, event_id, destination_id, status, time, code, response_data, manual, attempt_number)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::text[], $7::jsonb[], $8::boolean[], $9::integer[])
			ON CONFLICT (time, id) DO UPDATE SET
				status = EXCLUDED.status,
				code = EXCLUDED.code,
				response_data = EXCLUDED.response_data
		`, attemptArrays(attempts)...)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO event_attempt_index (
				event_id, attempt_id, tenant_id, destination_id,
				event_time, attempt_time, topic, status, manual, attempt_number
			)
			SELECT
				a.event_id,
				a.id,
				e.tenant_id,
				a.destination_id,
				e.time,
				a.time,
				e.topic,
				a.status,
				a.manual,
				a.attempt_number
			FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::text[], $7::jsonb[], $8::boolean[], $9::integer[])
				AS a(id, event_id, destination_id, status, time, code, response_data, manual, attempt_number)
			JOIN events e ON e.id = a.event_id
			ON CONFLICT (attempt_time, event_id, attempt_id) DO UPDATE SET
				status = EXCLUDED.status
		`, attemptArrays(attempts)...)
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

func attemptArrays(attempts []*models.Attempt) []any {
	ids := make([]string, len(attempts))
	eventIDs := make([]string, len(attempts))
	destinationIDs := make([]string, len(attempts))
	statuses := make([]string, len(attempts))
	times := make([]time.Time, len(attempts))
	codes := make([]string, len(attempts))
	responseDatas := make([]map[string]any, len(attempts))
	manuals := make([]bool, len(attempts))
	attemptNumbers := make([]int, len(attempts))

	for i, a := range attempts {
		ids[i] = a.ID
		eventIDs[i] = a.EventID
		destinationIDs[i] = a.DestinationID
		statuses[i] = a.Status
		times[i] = a.Time
		codes[i] = a.Code
		responseDatas[i] = a.ResponseData
		manuals[i] = a.Manual
		attemptNumbers[i] = a.AttemptNumber
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
		attemptNumbers,
	}
}
