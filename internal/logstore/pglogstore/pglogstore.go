package pglogstore

import (
	"context"
	"fmt"
	"strconv"
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

// eventWithPosition wraps an event with its cursor position data.
type eventWithPosition struct {
	*models.Event
	eventTime time.Time
}

// attemptRecordWithPosition wraps an attempt record with its cursor position data.
type attemptRecordWithPosition struct {
	*driver.AttemptRecord
	attemptTime time.Time
}

// parseTimestampMs parses a millisecond timestamp string.
func parseTimestampMs(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func (s *logStore) ListEvent(ctx context.Context, req driver.ListEventRequest) (driver.ListEventResponse, error) {
	// DestinationIDs filter is not supported for ListEvent.
	//
	// The current implementation is incorrect because it queries events.destination_id,
	// which represents the publish input (the destination specified when the event was
	// originally published), not the destinations that actually matched and received
	// the event.
	//
	// Events are destination-agnostic: a single event can be delivered to multiple
	// destinations based on routing rules. To filter events by destination, you need
	// to query via the attempts table, which records actual delivery attempts per
	// destination.
	//
	// For now, users should use ListAttempt with the DestinationIDs filter instead,
	// which correctly filters by the destinations that received delivery attempts.
	if len(req.DestinationIDs) > 0 {
		return driver.ListEventResponse{}, fmt.Errorf("ListEvent with DestinationIDs filter is not implemented: events are destination-agnostic, use ListAttempt instead")
	}

	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	res, err := pagination.Run(ctx, pagination.Config[eventWithPosition]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(ctx context.Context, q pagination.QueryInput) ([]eventWithPosition, error) {
			query, args := buildEventQuery(req, q)
			rows, err := s.db.Query(ctx, query, args...)
			if err != nil {
				return nil, fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()
			return scanEvents(rows)
		},
		Cursor: pagination.Cursor[eventWithPosition]{
			Encode: func(e eventWithPosition) string {
				position := fmt.Sprintf("%d::%s", e.eventTime.UnixMilli(), e.Event.ID)
				return cursor.Encode(cursorResourceEvent, cursorVersion, position)
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
	var conditions []string
	var args []any
	argNum := 1

	if req.TenantID != "" {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, req.TenantID)
		argNum++
	}

	if len(req.DestinationIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("destination_id = ANY($%d)", argNum))
		args = append(args, req.DestinationIDs)
		argNum++
	}

	if len(req.Topics) > 0 {
		conditions = append(conditions, fmt.Sprintf("topic = ANY($%d)", argNum))
		args = append(args, req.Topics)
		argNum++
	}

	if req.TimeFilter.GTE != nil {
		conditions = append(conditions, fmt.Sprintf("time >= $%d", argNum))
		args = append(args, *req.TimeFilter.GTE)
		argNum++
	}
	if req.TimeFilter.LTE != nil {
		conditions = append(conditions, fmt.Sprintf("time <= $%d", argNum))
		args = append(args, *req.TimeFilter.LTE)
		argNum++
	}
	if req.TimeFilter.GT != nil {
		conditions = append(conditions, fmt.Sprintf("time > $%d", argNum))
		args = append(args, *req.TimeFilter.GT)
		argNum++
	}
	if req.TimeFilter.LT != nil {
		conditions = append(conditions, fmt.Sprintf("time < $%d", argNum))
		args = append(args, *req.TimeFilter.LT)
		argNum++
	}

	if q.CursorPos != "" {
		cursorCond, cursorArgs := buildEventCursorCondition(q.Compare, q.CursorPos, argNum)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
		argNum += len(cursorArgs)
	}

	whereClause := strings.Join(conditions, " AND ")
	if whereClause == "" {
		whereClause = "1=1"
	}

	orderByClause := fmt.Sprintf("ORDER BY time %s, id %s",
		strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

	query := fmt.Sprintf(`
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
		WHERE %s
		%s
		LIMIT $%d
	`, whereClause, orderByClause, argNum)

	args = append(args, q.Limit)

	return query, args
}

func buildEventCursorCondition(compare, position string, argOffset int) (string, []any) {
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1", nil // invalid cursor, return always true
	}
	eventTimeMs, err := parseTimestampMs(parts[0])
	if err != nil {
		return "1=1", nil // invalid timestamp, return always true
	}
	eventID := parts[1]

	// Convert milliseconds to PostgreSQL timestamp
	condition := fmt.Sprintf(`(
		time %s to_timestamp($%d / 1000.0)
		OR (time = to_timestamp($%d / 1000.0) AND id %s $%d)
	)`, compare, argOffset, argOffset+1, compare, argOffset+2)

	return condition, []any{eventTimeMs, eventTimeMs, eventID}
}

func scanEvents(rows pgx.Rows) ([]eventWithPosition, error) {
	var results []eventWithPosition
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
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		results = append(results, eventWithPosition{
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
			eventTime: eventTime,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
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

	res, err := pagination.Run(ctx, pagination.Config[attemptRecordWithPosition]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(ctx context.Context, q pagination.QueryInput) ([]attemptRecordWithPosition, error) {
			query, args := buildAttemptQuery(req, q)
			rows, err := s.db.Query(ctx, query, args...)
			if err != nil {
				return nil, fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()
			return scanAttemptRecords(rows)
		},
		Cursor: pagination.Cursor[attemptRecordWithPosition]{
			Encode: func(ar attemptRecordWithPosition) string {
				position := fmt.Sprintf("%d::%s", ar.attemptTime.UnixMilli(), ar.Attempt.ID)
				return cursor.Encode(cursorResourceAttempt, cursorVersion, position)
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
	var conditions []string
	var args []any
	argNum := 1

	if req.TenantID != "" {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, req.TenantID)
		argNum++
	}

	if req.EventID != "" {
		conditions = append(conditions, fmt.Sprintf("event_id = $%d", argNum))
		args = append(args, req.EventID)
		argNum++
	}

	if len(req.DestinationIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("destination_id = ANY($%d)", argNum))
		args = append(args, req.DestinationIDs)
		argNum++
	}

	if req.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argNum))
		args = append(args, req.Status)
		argNum++
	}

	if len(req.Topics) > 0 {
		conditions = append(conditions, fmt.Sprintf("topic = ANY($%d)", argNum))
		args = append(args, req.Topics)
		argNum++
	}

	if req.TimeFilter.GTE != nil {
		conditions = append(conditions, fmt.Sprintf("time >= $%d", argNum))
		args = append(args, *req.TimeFilter.GTE)
		argNum++
	}
	if req.TimeFilter.LTE != nil {
		conditions = append(conditions, fmt.Sprintf("time <= $%d", argNum))
		args = append(args, *req.TimeFilter.LTE)
		argNum++
	}
	if req.TimeFilter.GT != nil {
		conditions = append(conditions, fmt.Sprintf("time > $%d", argNum))
		args = append(args, *req.TimeFilter.GT)
		argNum++
	}
	if req.TimeFilter.LT != nil {
		conditions = append(conditions, fmt.Sprintf("time < $%d", argNum))
		args = append(args, *req.TimeFilter.LT)
		argNum++
	}

	if q.CursorPos != "" {
		cursorCond, cursorArgs := buildAttemptCursorCondition(q.Compare, q.CursorPos, argNum)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
		argNum += len(cursorArgs)
	}

	whereClause := strings.Join(conditions, " AND ")
	if whereClause == "" {
		whereClause = "1=1"
	}

	orderByClause := fmt.Sprintf("ORDER BY time %s, id %s",
		strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

	query := fmt.Sprintf(`
		SELECT
			id,
			event_id,
			tenant_id,
			destination_id,
			topic,
			status,
			time,
			attempt_number,
			manual,
			code,
			response_data,
			event_time,
			eligible_for_retry,
			event_data,
			event_metadata
		FROM attempts
		WHERE %s
		%s
		LIMIT $%d
	`, whereClause, orderByClause, argNum)

	args = append(args, q.Limit)

	return query, args
}

func buildAttemptCursorCondition(compare, position string, argOffset int) (string, []any) {
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1", nil // invalid cursor, return always true
	}
	attemptTimeMs, err := parseTimestampMs(parts[0])
	if err != nil {
		return "1=1", nil // invalid timestamp, return always true
	}
	attemptID := parts[1]

	// Convert milliseconds to PostgreSQL timestamp
	condition := fmt.Sprintf(`(
		time %s to_timestamp($%d / 1000.0)
		OR (time = to_timestamp($%d / 1000.0) AND id %s $%d)
	)`, compare, argOffset, argOffset+1, compare, argOffset+2)

	return condition, []any{attemptTimeMs, attemptTimeMs, attemptID}
}

func scanAttemptRecords(rows pgx.Rows) ([]attemptRecordWithPosition, error) {
	var results []attemptRecordWithPosition
	for rows.Next() {
		var (
			id               string
			eventID          string
			tenantID         string
			destinationID    string
			topic            string
			status           string
			attemptTime      time.Time
			attemptNumber    int
			manual           bool
			code             string
			responseData     map[string]any
			eventTime        time.Time
			eligibleForRetry bool
			eventData        map[string]any
			eventMetadata    map[string]string
		)

		if err := rows.Scan(
			&id,
			&eventID,
			&tenantID,
			&destinationID,
			&topic,
			&status,
			&attemptTime,
			&attemptNumber,
			&manual,
			&code,
			&responseData,
			&eventTime,
			&eligibleForRetry,
			&eventData,
			&eventMetadata,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		results = append(results, attemptRecordWithPosition{
			AttemptRecord: &driver.AttemptRecord{
				Attempt: &models.Attempt{
					ID:            id,
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
					Data:             eventData,
					Metadata:         eventMetadata,
				},
			},
			attemptTime: attemptTime,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

func (s *logStore) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	var conditions []string
	var args []any
	argNum := 1

	if req.TenantID != "" {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, req.TenantID)
		argNum++
	}

	conditions = append(conditions, fmt.Sprintf("id = $%d", argNum))
	args = append(args, req.EventID)

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			id,
			tenant_id,
			destination_id,
			topic,
			eligible_for_retry,
			time,
			metadata,
			data
		FROM events
		WHERE %s
		LIMIT 1`, whereClause)

	row := s.db.QueryRow(ctx, query, args...)

	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Time,
		&event.Metadata,
		&event.Data,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return event, nil
}

func (s *logStore) RetrieveAttempt(ctx context.Context, req driver.RetrieveAttemptRequest) (*driver.AttemptRecord, error) {
	var conditions []string
	var args []any
	argNum := 1

	if req.TenantID != "" {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, req.TenantID)
		argNum++
	}

	conditions = append(conditions, fmt.Sprintf("id = $%d", argNum))
	args = append(args, req.AttemptID)

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			id,
			event_id,
			tenant_id,
			destination_id,
			topic,
			status,
			time,
			attempt_number,
			manual,
			code,
			response_data,
			event_time,
			eligible_for_retry,
			event_data,
			event_metadata
		FROM attempts
		WHERE %s
		LIMIT 1`, whereClause)

	row := s.db.QueryRow(ctx, query, args...)

	var (
		id               string
		eventID          string
		tenantID         string
		destinationID    string
		topic            string
		status           string
		attemptTime      time.Time
		attemptNumber    int
		manual           bool
		code             string
		responseData     map[string]any
		eventTime        time.Time
		eligibleForRetry bool
		eventData        map[string]any
		eventMetadata    map[string]string
	)

	err := row.Scan(
		&id,
		&eventID,
		&tenantID,
		&destinationID,
		&topic,
		&status,
		&attemptTime,
		&attemptNumber,
		&manual,
		&code,
		&responseData,
		&eventTime,
		&eligibleForRetry,
		&eventData,
		&eventMetadata,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return &driver.AttemptRecord{
		Attempt: &models.Attempt{
			ID:            id,
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
			Data:             eventData,
			Metadata:         eventMetadata,
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

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Insert events
	if len(events) > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[], $5::text[], $6::boolean[], $7::jsonb[], $8::jsonb[])
			ON CONFLICT (time, id) DO NOTHING
		`, eventArrays(events)...)
		if err != nil {
			return fmt.Errorf("insert events failed: %w", err)
		}
	}

	// Insert attempts
	if len(entries) > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO attempts (
				id, event_id, tenant_id, destination_id, topic, status,
				time, attempt_number, manual, code, response_data,
				event_time, eligible_for_retry, event_data, event_metadata
			)
			SELECT * FROM unnest(
				$1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[],
				$7::timestamptz[], $8::integer[], $9::boolean[], $10::text[], $11::jsonb[],
				$12::timestamptz[], $13::boolean[], $14::jsonb[], $15::jsonb[]
			)
			ON CONFLICT (time, id) DO UPDATE SET
				status = EXCLUDED.status,
				code = EXCLUDED.code,
				response_data = EXCLUDED.response_data
		`, attemptArrays(entries)...)
		if err != nil {
			return fmt.Errorf("insert attempts failed: %w", err)
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

// attemptArrays extracts arrays from log entries for bulk insert.
func attemptArrays(entries []*models.LogEntry) []any {
	n := len(entries)

	ids := make([]string, n)
	eventIDs := make([]string, n)
	tenantIDs := make([]string, n)
	destinationIDs := make([]string, n)
	topics := make([]string, n)
	statuses := make([]string, n)
	times := make([]time.Time, n)
	attemptNumbers := make([]int, n)
	manuals := make([]bool, n)
	codes := make([]string, n)
	responseDatas := make([]map[string]any, n)
	eventTimes := make([]time.Time, n)
	eligibleForRetries := make([]bool, n)
	eventDatas := make([]map[string]any, n)
	eventMetadatas := make([]map[string]string, n)

	for i, entry := range entries {
		a := entry.Attempt
		e := entry.Event

		ids[i] = a.ID
		eventIDs[i] = a.EventID
		tenantIDs[i] = e.TenantID
		destinationIDs[i] = a.DestinationID
		topics[i] = e.Topic
		statuses[i] = a.Status
		times[i] = a.Time
		attemptNumbers[i] = a.AttemptNumber
		manuals[i] = a.Manual
		codes[i] = a.Code
		responseDatas[i] = a.ResponseData
		eventTimes[i] = e.Time
		eligibleForRetries[i] = e.EligibleForRetry
		eventDatas[i] = e.Data
		eventMetadatas[i] = e.Metadata
	}

	return []any{
		ids,
		eventIDs,
		tenantIDs,
		destinationIDs,
		topics,
		statuses,
		times,
		attemptNumbers,
		manuals,
		codes,
		responseDatas,
		eventTimes,
		eligibleForRetries,
		eventDatas,
		eventMetadatas,
	}
}
