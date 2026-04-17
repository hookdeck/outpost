package chlogstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination"
)

const (
	cursorResourceEvent   = "evt"
	cursorResourceAttempt = "att"
	cursorVersion         = 1
)

type logStoreImpl struct {
	chDB          clickhouse.DB
	eventsTable   string
	attemptsTable string
}

var _ driver.LogStore = (*logStoreImpl)(nil)

func NewLogStore(chDB clickhouse.DB, deploymentID string) driver.LogStore {
	prefix := ""
	if deploymentID != "" {
		prefix = deploymentID + "_"
	}
	return &logStoreImpl{
		chDB:          chDB,
		eventsTable:   prefix + "events",
		attemptsTable: prefix + "attempts",
	}
}

// fetchAndDedup queries ClickHouse and deduplicates results by a key. If
// duplicates reduce the result count below the requested limit, it advances
// the cursor and fetches more rows until the limit is met or data is exhausted.
// This avoids LIMIT 1 BY / GROUP BY on large result sets while still hiding
// duplicates from unmerged ReplacingMergeTree parts.
func fetchAndDedup[T any](
	ctx context.Context,
	chDB clickhouse.DB,
	q pagination.QueryInput,
	buildQuery func(pagination.QueryInput) (string, []any),
	scan func(clickhouse.Rows) ([]T, error),
	getID func(T) string,
	getCursorPos func(T) string,
) ([]T, error) {
	seen := make(map[string]bool)
	var deduped []T
	cursorPos := q.CursorPos

	for len(deduped) < q.Limit {
		qi := pagination.QueryInput{
			Limit:     q.Limit,
			Compare:   q.Compare,
			SortDir:   q.SortDir,
			CursorPos: cursorPos,
		}
		query, args := buildQuery(qi)
		rows, err := chDB.Query(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w", err)
		}
		scanned, err := scan(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}

		for _, item := range scanned {
			id := getID(item)
			if !seen[id] {
				seen[id] = true
				deduped = append(deduped, item)
			}
		}

		// Fewer rows than requested means we've exhausted the data.
		if len(scanned) < q.Limit {
			break
		}

		// Advance cursor past last scanned row for next iteration.
		cursorPos = getCursorPos(scanned[len(scanned)-1])
	}

	// Truncate to the requested limit — the last batch may have added
	// more unique items than needed to reach the limit.
	if len(deduped) > q.Limit {
		deduped = deduped[:q.Limit]
	}

	return deduped, nil
}

// eventWithPosition wraps an event with its cursor position data.
type eventWithPosition struct {
	*models.Event
	eventTime time.Time
}

func (e eventWithPosition) cursorPosition() string {
	return fmt.Sprintf("%d::%s", e.eventTime.UnixMilli(), e.Event.ID)
}

func (s *logStoreImpl) ListEvent(ctx context.Context, req driver.ListEventRequest) (driver.ListEventResponse, error) {
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
			return fetchAndDedup(ctx, s.chDB, q, func(qi pagination.QueryInput) (string, []any) {
				return buildEventQuery(s.eventsTable, req, qi)
			}, scanEvents, func(e eventWithPosition) string {
				return e.Event.ID
			}, eventWithPosition.cursorPosition)
		},
		Cursor: pagination.Cursor[eventWithPosition]{
			Encode: func(e eventWithPosition) string {
				return cursor.Encode(cursorResourceEvent, cursorVersion, e.cursorPosition())
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

func buildEventQuery(table string, req driver.ListEventRequest, q pagination.QueryInput) (string, []any) {
	var conditions []string
	var args []any

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, "tenant_id IN ?")
		args = append(args, req.TenantIDs)
	}

	if len(req.EventIDs) > 0 {
		conditions = append(conditions, "event_id IN ?")
		args = append(args, req.EventIDs)
	}

	if len(req.DestinationIDs) > 0 {
		conditions = append(conditions, "hasAny(matched_destination_ids, ?)")
		args = append(args, req.DestinationIDs)
	}

	if len(req.Topics) > 0 {
		conditions = append(conditions, "topic IN ?")
		args = append(args, req.Topics)
	}

	if req.TimeFilter.GTE != nil {
		conditions = append(conditions, "event_time >= ?")
		args = append(args, *req.TimeFilter.GTE)
	}
	if req.TimeFilter.LTE != nil {
		conditions = append(conditions, "event_time <= ?")
		args = append(args, *req.TimeFilter.LTE)
	}
	if req.TimeFilter.GT != nil {
		conditions = append(conditions, "event_time > ?")
		args = append(args, *req.TimeFilter.GT)
	}
	if req.TimeFilter.LT != nil {
		conditions = append(conditions, "event_time < ?")
		args = append(args, *req.TimeFilter.LT)
	}

	if q.CursorPos != "" {
		cursorCond, cursorArgs := buildEventCursorCondition(q.Compare, q.CursorPos)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
	}

	whereClause := strings.Join(conditions, " AND ")
	if whereClause == "" {
		whereClause = "1=1"
	}

	orderByClause := fmt.Sprintf("ORDER BY event_time %s, event_id %s",
		strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			matched_destination_ids,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data
		FROM %s
		WHERE %s
		%s
		LIMIT %d
	`, table, whereClause, orderByClause, q.Limit)

	return query, args
}

func scanEvents(rows clickhouse.Rows) ([]eventWithPosition, error) {
	var results []eventWithPosition
	for rows.Next() {
		var (
			eventID               string
			tenantID              string
			matchedDestinationIDs []string
			topic                 string
			eligibleForRetry      bool
			eventTime             time.Time
			metadataStr           string
			dataStr               string
		)

		err := rows.Scan(
			&eventID,
			&tenantID,
			&matchedDestinationIDs,
			&topic,
			&eligibleForRetry,
			&eventTime,
			&metadataStr,
			&dataStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		var metadata map[string]string

		if metadataStr != "" {
			if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}

		if matchedDestinationIDs == nil {
			matchedDestinationIDs = []string{}
		}

		results = append(results, eventWithPosition{
			Event: &models.Event{
				ID:                    eventID,
				TenantID:              tenantID,
				MatchedDestinationIDs: matchedDestinationIDs,
				Topic:                 topic,
				EligibleForRetry:      eligibleForRetry,
				Time:                  eventTime,
				Data:                  json.RawMessage(dataStr),
				Metadata:              metadata,
			},
			eventTime: eventTime,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

func buildEventCursorCondition(compare, position string) (string, []any) {
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1", nil // invalid cursor, return always true
	}
	eventTimeMs, err := parseTimestampMs(parts[0])
	if err != nil {
		return "1=1", nil // invalid timestamp, return always true
	}
	eventID := parts[1]

	condition := fmt.Sprintf(`(
		event_time %s fromUnixTimestamp64Milli(?)
		OR (event_time = fromUnixTimestamp64Milli(?) AND event_id %s ?)
	)`, compare, compare)

	return condition, []any{eventTimeMs, eventTimeMs, eventID}
}

// attemptRecordWithPosition wraps an attempt record with its cursor position data.
type attemptRecordWithPosition struct {
	*driver.AttemptRecord
	attemptTime time.Time
}

func (ar attemptRecordWithPosition) cursorPosition() string {
	return fmt.Sprintf("%d::%s", ar.attemptTime.UnixMilli(), ar.Attempt.ID)
}

func (s *logStoreImpl) ListAttempt(ctx context.Context, req driver.ListAttemptRequest) (driver.ListAttemptResponse, error) {
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
			return fetchAndDedup(ctx, s.chDB, q, func(qi pagination.QueryInput) (string, []any) {
				return buildAttemptQuery(s.attemptsTable, req, qi)
			}, scanAttemptRecords, func(ar attemptRecordWithPosition) string {
				return ar.Attempt.ID
			}, attemptRecordWithPosition.cursorPosition)
		},
		Cursor: pagination.Cursor[attemptRecordWithPosition]{
			Encode: func(ar attemptRecordWithPosition) string {
				return cursor.Encode(cursorResourceAttempt, cursorVersion, ar.cursorPosition())
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

func buildAttemptQuery(table string, req driver.ListAttemptRequest, q pagination.QueryInput) (string, []any) {
	var conditions []string
	var args []any

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, "tenant_id IN ?")
		args = append(args, req.TenantIDs)
	}

	if len(req.EventIDs) > 0 {
		conditions = append(conditions, "event_id IN ?")
		args = append(args, req.EventIDs)
	}

	if len(req.DestinationIDs) > 0 {
		conditions = append(conditions, "destination_id IN ?")
		args = append(args, req.DestinationIDs)
	}

	if len(req.DestinationTypes) > 0 {
		conditions = append(conditions, "destination_type IN ?")
		args = append(args, req.DestinationTypes)
	}

	if req.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, req.Status)
	}

	if len(req.Topics) > 0 {
		conditions = append(conditions, "topic IN ?")
		args = append(args, req.Topics)
	}

	if req.TimeFilter.GTE != nil {
		conditions = append(conditions, "attempt_time >= ?")
		args = append(args, *req.TimeFilter.GTE)
	}
	if req.TimeFilter.LTE != nil {
		conditions = append(conditions, "attempt_time <= ?")
		args = append(args, *req.TimeFilter.LTE)
	}
	if req.TimeFilter.GT != nil {
		conditions = append(conditions, "attempt_time > ?")
		args = append(args, *req.TimeFilter.GT)
	}
	if req.TimeFilter.LT != nil {
		conditions = append(conditions, "attempt_time < ?")
		args = append(args, *req.TimeFilter.LT)
	}

	if q.CursorPos != "" {
		cursorCond, cursorArgs := buildAttemptCursorCondition(q.Compare, q.CursorPos)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
	}

	whereClause := strings.Join(conditions, " AND ")
	if whereClause == "" {
		whereClause = "1=1"
	}

	orderByClause := fmt.Sprintf("ORDER BY attempt_time %s, attempt_id %s",
		strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			destination_id,
			destination_type,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data,
			attempt_id,
			status,
			attempt_time,
			code,
			response_data,
			manual,
			attempt_number
		FROM %s
		WHERE %s
		%s
		LIMIT %d
	`, table, whereClause, orderByClause, q.Limit)

	return query, args
}

func scanAttemptRecords(rows clickhouse.Rows) ([]attemptRecordWithPosition, error) {
	var results []attemptRecordWithPosition
	for rows.Next() {
		var (
			eventID          string
			tenantID         string
			destinationID    string
			destinationType  string
			topic            string
			eligibleForRetry bool
			eventTime        time.Time
			metadataStr      string
			dataStr          string
			attemptID        string
			status           string
			attemptTime      time.Time
			code             string
			responseDataStr  string
			manual           bool
			attemptNumber    uint32
		)

		err := rows.Scan(
			&eventID,
			&tenantID,
			&destinationID,
			&destinationType,
			&topic,
			&eligibleForRetry,
			&eventTime,
			&metadataStr,
			&dataStr,
			&attemptID,
			&status,
			&attemptTime,
			&code,
			&responseDataStr,
			&manual,
			&attemptNumber,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		var metadata map[string]string
		var responseData map[string]any

		if metadataStr != "" {
			if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}
		if responseDataStr != "" {
			if err := json.Unmarshal([]byte(responseDataStr), &responseData); err != nil {
				return nil, fmt.Errorf("failed to unmarshal response_data: %w", err)
			}
		}

		results = append(results, attemptRecordWithPosition{
			AttemptRecord: &driver.AttemptRecord{
				Attempt: &models.Attempt{
					ID:              attemptID,
					TenantID:        tenantID,
					EventID:         eventID,
					DestinationID:   destinationID,
					DestinationType: destinationType,
					AttemptNumber:   int(attemptNumber),
					Manual:          manual,
					Status:          status,
					Time:            attemptTime,
					Code:            code,
					ResponseData:    responseData,
				},
				Event: &models.Event{
					ID:               eventID,
					TenantID:         tenantID,
					DestinationID:    destinationID,
					Topic:            topic,
					EligibleForRetry: eligibleForRetry,
					Time:             eventTime,
					Data:             json.RawMessage(dataStr),
					Metadata:         metadata,
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

func (s *logStoreImpl) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	var conditions []string
	var args []any

	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	conditions = append(conditions, "event_id = ?")
	args = append(args, req.EventID)

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			matched_destination_ids,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data
		FROM %s
		WHERE %s
		LIMIT 1`, s.eventsTable, whereClause)

	row := s.chDB.QueryRow(ctx, query, args...)

	var metadataStr, dataStr string
	event := &models.Event{}

	if err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.MatchedDestinationIDs,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Time,
		&metadataStr,
		&dataStr,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	if metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &event.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}
	if dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &event.Data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal data: %w", err)
		}
	}

	if event.MatchedDestinationIDs == nil {
		event.MatchedDestinationIDs = []string{}
	}

	return event, nil
}

func (s *logStoreImpl) RetrieveAttempt(ctx context.Context, req driver.RetrieveAttemptRequest) (*driver.AttemptRecord, error) {
	var conditions []string
	var args []any

	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	conditions = append(conditions, "attempt_id = ?")
	args = append(args, req.AttemptID)

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			destination_id,
			destination_type,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data,
			attempt_id,
			status,
			attempt_time,
			code,
			response_data,
			manual,
			attempt_number
		FROM %s
		WHERE %s
		LIMIT 1`, s.attemptsTable, whereClause)

	row := s.chDB.QueryRow(ctx, query, args...)

	var (
		eventID          string
		tenantID         string
		destinationID    string
		destinationType  string
		topic            string
		eligibleForRetry bool
		eventTime        time.Time
		metadataStr      string
		dataStr          string
		attemptID        string
		status           string
		attemptTime      time.Time
		code             string
		responseDataStr  string
		manual           bool
		attemptNumber    uint32
	)

	err := row.Scan(
		&eventID,
		&tenantID,
		&destinationID,
		&destinationType,
		&topic,
		&eligibleForRetry,
		&eventTime,
		&metadataStr,
		&dataStr,
		&attemptID,
		&status,
		&attemptTime,
		&code,
		&responseDataStr,
		&manual,
		&attemptNumber,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	var metadata map[string]string
	var responseData map[string]any

	if metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}
	if responseDataStr != "" {
		if err := json.Unmarshal([]byte(responseDataStr), &responseData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response_data: %w", err)
		}
	}

	return &driver.AttemptRecord{
		Attempt: &models.Attempt{
			ID:              attemptID,
			TenantID:        tenantID,
			EventID:         eventID,
			DestinationID:   destinationID,
			DestinationType: destinationType,
			AttemptNumber:   int(attemptNumber),
			Manual:          manual,
			Status:          status,
			Time:            attemptTime,
			Code:            code,
			ResponseData:    responseData,
		},
		Event: &models.Event{
			ID:               eventID,
			TenantID:         tenantID,
			DestinationID:    destinationID,
			Topic:            topic,
			EligibleForRetry: eligibleForRetry,
			Time:             eventTime,
			Data:             json.RawMessage(dataStr),
			Metadata:         metadata,
		},
	}, nil
}

func (s *logStoreImpl) InsertMany(ctx context.Context, entries []*models.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Extract and dedupe events by ID, skipping retry attempts.
	// Retries (AttemptNumber > 1) carry identical event data — the event row
	// already exists from the first attempt's batch.
	eventMap := make(map[string]*models.Event)
	for _, entry := range entries {
		if entry.Attempt.AttemptNumber <= 1 {
			eventMap[entry.Event.ID] = entry.Event
		}
	}

	if len(eventMap) > 0 {
		eventBatch, err := s.chDB.PrepareBatch(ctx,
			fmt.Sprintf(`INSERT INTO %s (
				event_id, tenant_id, matched_destination_ids, topic, eligible_for_retry, event_time, metadata, data
			)`, s.eventsTable),
		)
		if err != nil {
			return fmt.Errorf("prepare events batch failed: %w", err)
		}

		for _, e := range eventMap {
			metadataJSON, err := json.Marshal(e.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}
			matched := e.MatchedDestinationIDs
			if matched == nil {
				matched = []string{}
			}
			if err := eventBatch.Append(
				e.ID,
				e.TenantID,
				matched,
				e.Topic,
				e.EligibleForRetry,
				e.Time,
				string(metadataJSON),
				string(e.Data),
			); err != nil {
				return fmt.Errorf("events batch append failed: %w", err)
			}
		}

		if err := eventBatch.Send(); err != nil {
			return fmt.Errorf("events batch send failed: %w", err)
		}
	}

	// Insert attempts with their paired event data
	attemptBatch, err := s.chDB.PrepareBatch(ctx,
		fmt.Sprintf(`INSERT INTO %s (
			event_id, tenant_id, destination_id, destination_type, topic, eligible_for_retry, event_time, metadata, data,
			attempt_id, status, attempt_time, code, response_data, manual, attempt_number
		)`, s.attemptsTable),
	)
	if err != nil {
		return fmt.Errorf("prepare attempts batch failed: %w", err)
	}

	for _, entry := range entries {
		event := entry.Event
		a := entry.Attempt

		metadataJSON, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		responseDataJSON, err := json.Marshal(a.ResponseData)
		if err != nil {
			return fmt.Errorf("failed to marshal response_data: %w", err)
		}

		if err := attemptBatch.Append(
			a.EventID,
			event.TenantID,
			a.DestinationID,
			a.DestinationType,
			event.Topic,
			event.EligibleForRetry,
			event.Time,
			string(metadataJSON),
			string(event.Data),
			a.ID,
			a.Status,
			a.Time,
			a.Code,
			string(responseDataJSON),
			a.Manual,
			uint32(a.AttemptNumber),
		); err != nil {
			return fmt.Errorf("attempts batch append failed: %w", err)
		}
	}

	if err := attemptBatch.Send(); err != nil {
		return fmt.Errorf("attempts batch send failed: %w", err)
	}

	return nil
}

func parseTimestampMs(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func buildAttemptCursorCondition(compare, position string) (string, []any) {
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1", nil
	}
	attemptTimeMs, err := parseTimestampMs(parts[0])
	if err != nil {
		return "1=1", nil // invalid timestamp, return always true
	}
	attemptID := parts[1]

	condition := fmt.Sprintf(`(
		attempt_time %s fromUnixTimestamp64Milli(?)
		OR (attempt_time = fromUnixTimestamp64Milli(?) AND attempt_id %s ?)
	)`, compare, compare)

	return condition, []any{attemptTimeMs, attemptTimeMs, attemptID}
}
