package chlogstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
)

type logStoreImpl struct {
	chDB clickhouse.DB
}

var _ driver.LogStore = (*logStoreImpl)(nil)

func NewLogStore(chDB clickhouse.DB) driver.LogStore {
	return &logStoreImpl{chDB: chDB}
}

func (s *logStoreImpl) ListEvent(ctx context.Context, req driver.ListEventRequest) (driver.ListEventResponse, error) {
	// Validate and set defaults
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Decode and validate cursors (using "event_time" as the sortBy for events)
	nextCursor, prevCursor, err := cursor.DecodeAndValidate(req.Next, req.Prev, "event_time", sortOrder)
	if err != nil {
		return driver.ListEventResponse{}, err
	}

	// Determine if we're going backward (using prev cursor)
	goingBackward := !prevCursor.IsEmpty()

	// Build ORDER BY clause with tiebreaker for deterministic pagination
	// ORDER BY event_time, event_id
	var orderByClause string
	if sortOrder == "desc" {
		if goingBackward {
			orderByClause = "ORDER BY event_time ASC, event_id ASC"
		} else {
			orderByClause = "ORDER BY event_time DESC, event_id DESC"
		}
	} else {
		if goingBackward {
			orderByClause = "ORDER BY event_time DESC, event_id DESC"
		} else {
			orderByClause = "ORDER BY event_time ASC, event_id ASC"
		}
	}

	// Build query with filters
	var conditions []string
	var args []interface{}

	// Optional: tenant_id filter (skip if empty to query across all tenants)
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	// Optional filters
	if len(req.DestinationIDs) > 0 {
		conditions = append(conditions, "destination_id IN ?")
		args = append(args, req.DestinationIDs)
	}

	if len(req.Topics) > 0 {
		conditions = append(conditions, "topic IN ?")
		args = append(args, req.Topics)
	}

	// Time filters
	if req.EventStart != nil {
		conditions = append(conditions, "event_time >= ?")
		args = append(args, *req.EventStart)
	}
	if req.EventEnd != nil {
		conditions = append(conditions, "event_time <= ?")
		args = append(args, *req.EventEnd)
	}

	// Cursor conditions
	if !nextCursor.IsEmpty() {
		cursorCond := buildEventCursorCondition(sortOrder, nextCursor.Position, false)
		conditions = append(conditions, cursorCond)
	} else if !prevCursor.IsEmpty() {
		cursorCond := buildEventCursorCondition(sortOrder, prevCursor.Position, true)
		conditions = append(conditions, cursorCond)
	}

	whereClause := strings.Join(conditions, " AND ")
	if whereClause == "" {
		whereClause = "1=1"
	}

	// Note: We intentionally omit FINAL to avoid forcing ClickHouse to merge all parts
	// before returning results. The events table uses ReplacingMergeTree, so duplicates
	// may briefly appear before background merges consolidate them. This is acceptable
	// for log viewing and maintains O(limit) query performance.
	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			destination_id,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data
		FROM events
		WHERE %s
		%s
		LIMIT %d
	`, whereClause, orderByClause, limit+1)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		event     *models.Event
		eventTime time.Time
	}
	var results []rowData

	for rows.Next() {
		var (
			eventID          string
			tenantID         string
			destinationID    string
			topic            string
			eligibleForRetry bool
			eventTime        time.Time
			metadataStr      string
			dataStr          string
		)

		err := rows.Scan(
			&eventID,
			&tenantID,
			&destinationID,
			&topic,
			&eligibleForRetry,
			&eventTime,
			&metadataStr,
			&dataStr,
		)
		if err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("scan failed: %w", err)
		}

		// Parse JSON fields
		var metadata map[string]string
		var data map[string]interface{}

		if metadataStr != "" {
			if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
				return driver.ListEventResponse{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}
		if dataStr != "" {
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				return driver.ListEventResponse{}, fmt.Errorf("failed to unmarshal data: %w", err)
			}
		}

		event := &models.Event{
			ID:               eventID,
			TenantID:         tenantID,
			DestinationID:    destinationID,
			Topic:            topic,
			EligibleForRetry: eligibleForRetry,
			Time:             eventTime,
			Data:             data,
			Metadata:         metadata,
		}

		results = append(results, rowData{event: event, eventTime: eventTime})
	}

	if err := rows.Err(); err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("rows error: %w", err)
	}

	// Handle pagination
	var hasMore bool
	if len(results) > limit {
		hasMore = true
		results = results[:limit] // Always keep the first `limit` items, remove the extra
	}

	// When going backward, we queried in reverse order, so reverse results back to normal order
	if goingBackward {
		for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
			results[i], results[j] = results[j], results[i]
		}
	}

	// Build response
	data := make([]*models.Event, len(results))
	for i, r := range results {
		data[i] = r.event
	}

	var nextEncoded, prevEncoded string
	if len(results) > 0 {
		getPosition := func(r rowData) string {
			// Cursor format: eventTimeMs::eventID
			return fmt.Sprintf("%d::%s", r.eventTime.UnixMilli(), r.event.ID)
		}

		encodeCursor := func(position string) string {
			return cursor.Encode(cursor.Cursor{
				SortBy:    "event_time",
				SortOrder: sortOrder,
				Position:  position,
			})
		}

		if !prevCursor.IsEmpty() {
			nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			if hasMore {
				prevEncoded = encodeCursor(getPosition(results[0]))
			}
		} else if !nextCursor.IsEmpty() {
			prevEncoded = encodeCursor(getPosition(results[0]))
			if hasMore {
				nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			}
		} else {
			if hasMore {
				nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			}
		}
	}

	return driver.ListEventResponse{
		Data: data,
		Next: nextEncoded,
		Prev: prevEncoded,
	}, nil
}

// buildEventCursorCondition builds a SQL condition for cursor-based pagination on events table
func buildEventCursorCondition(sortOrder, position string, isBackward bool) string {
	// Parse position: eventTimeMs::eventID
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1" // invalid cursor, return always true
	}
	eventTimeMs := parts[0]
	eventID := parts[1]

	// Determine comparison direction
	var cmp string
	if sortOrder == "desc" {
		if isBackward {
			cmp = ">"
		} else {
			cmp = "<"
		}
	} else {
		if isBackward {
			cmp = "<"
		} else {
			cmp = ">"
		}
	}

	// Build multi-column comparison for (event_time, event_id)
	return fmt.Sprintf(`(
		event_time %s fromUnixTimestamp64Milli(%s)
		OR (event_time = fromUnixTimestamp64Milli(%s) AND event_id %s '%s')
	)`, cmp, eventTimeMs, eventTimeMs, cmp, eventID)
}

func (s *logStoreImpl) ListDeliveryEvent(ctx context.Context, req driver.ListDeliveryEventRequest) (driver.ListDeliveryEventResponse, error) {
	// Validate and set defaults
	sortBy := req.SortBy
	if sortBy != "event_time" && sortBy != "delivery_time" {
		sortBy = "delivery_time"
	}
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Decode and validate cursors
	nextCursor, prevCursor, err := cursor.DecodeAndValidate(req.Next, req.Prev, sortBy, sortOrder)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, err
	}

	// Determine if we're going backward (using prev cursor)
	goingBackward := !prevCursor.IsEmpty()

	// Build ORDER BY clause
	// For delivery_time sort: ORDER BY delivery_time, delivery_id
	// For event_time sort: ORDER BY event_time, event_id, delivery_time, delivery_id
	var orderByClause string
	if sortBy == "event_time" {
		if sortOrder == "desc" {
			if goingBackward {
				orderByClause = "ORDER BY event_time ASC, event_id ASC, delivery_time ASC, delivery_id ASC"
			} else {
				orderByClause = "ORDER BY event_time DESC, event_id DESC, delivery_time DESC, delivery_id DESC"
			}
		} else {
			if goingBackward {
				orderByClause = "ORDER BY event_time DESC, event_id DESC, delivery_time DESC, delivery_id DESC"
			} else {
				orderByClause = "ORDER BY event_time ASC, event_id ASC, delivery_time ASC, delivery_id ASC"
			}
		}
	} else {
		if sortOrder == "desc" {
			if goingBackward {
				orderByClause = "ORDER BY delivery_time ASC, delivery_id ASC"
			} else {
				orderByClause = "ORDER BY delivery_time DESC, delivery_id DESC"
			}
		} else {
			if goingBackward {
				orderByClause = "ORDER BY delivery_time DESC, delivery_id DESC"
			} else {
				orderByClause = "ORDER BY delivery_time ASC, delivery_id ASC"
			}
		}
	}

	// Build query with filters
	var conditions []string
	var args []interface{}

	// Optional: tenant_id filter (skip if empty to query across all tenants)
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	// Optional filters
	if req.EventID != "" {
		conditions = append(conditions, "event_id = ?")
		args = append(args, req.EventID)
	}

	if len(req.DestinationIDs) > 0 {
		conditions = append(conditions, "destination_id IN ?")
		args = append(args, req.DestinationIDs)
	}

	if req.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, req.Status)
	}

	if len(req.Topics) > 0 {
		conditions = append(conditions, "topic IN ?")
		args = append(args, req.Topics)
	}

	// Time filters
	if req.EventStart != nil {
		conditions = append(conditions, "event_time >= ?")
		args = append(args, *req.EventStart)
	}
	if req.EventEnd != nil {
		conditions = append(conditions, "event_time <= ?")
		args = append(args, *req.EventEnd)
	}
	if req.DeliveryStart != nil {
		conditions = append(conditions, "delivery_time >= ?")
		args = append(args, *req.DeliveryStart)
	}
	if req.DeliveryEnd != nil {
		conditions = append(conditions, "delivery_time <= ?")
		args = append(args, *req.DeliveryEnd)
	}

	// Cursor conditions
	if !nextCursor.IsEmpty() {
		cursorCond := buildCursorCondition(sortBy, sortOrder, nextCursor.Position, false)
		conditions = append(conditions, cursorCond)
	} else if !prevCursor.IsEmpty() {
		cursorCond := buildCursorCondition(sortBy, sortOrder, prevCursor.Position, true)
		conditions = append(conditions, cursorCond)
	}

	whereClause := strings.Join(conditions, " AND ")
	if whereClause == "" {
		whereClause = "1=1"
	}

	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			destination_id,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data,
			delivery_id,
			delivery_event_id,
			status,
			delivery_time,
			code,
			response_data
		FROM event_log
		WHERE %s
		%s
		LIMIT %d
	`, whereClause, orderByClause, limit+1)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		de           *models.DeliveryEvent
		eventTime    time.Time
		deliveryTime time.Time
	}
	var results []rowData

	for rows.Next() {
		var (
			eventID          string
			tenantID         string
			destinationID    string
			topic            string
			eligibleForRetry bool
			eventTime        time.Time
			metadataStr      string
			dataStr          string
			deliveryID       string
			deliveryEventID  string
			status           string
			deliveryTime     time.Time
			code             string
			responseDataStr  string
		)

		err := rows.Scan(
			&eventID,
			&tenantID,
			&destinationID,
			&topic,
			&eligibleForRetry,
			&eventTime,
			&metadataStr,
			&dataStr,
			&deliveryID,
			&deliveryEventID,
			&status,
			&deliveryTime,
			&code,
			&responseDataStr,
		)
		if err != nil {
			return driver.ListDeliveryEventResponse{}, fmt.Errorf("scan failed: %w", err)
		}

		// Parse JSON fields
		var metadata map[string]string
		var data map[string]interface{}
		var responseData map[string]interface{}

		if metadataStr != "" {
			if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
				return driver.ListDeliveryEventResponse{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}
		if dataStr != "" {
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				return driver.ListDeliveryEventResponse{}, fmt.Errorf("failed to unmarshal data: %w", err)
			}
		}
		if responseDataStr != "" {
			if err := json.Unmarshal([]byte(responseDataStr), &responseData); err != nil {
				return driver.ListDeliveryEventResponse{}, fmt.Errorf("failed to unmarshal response_data: %w", err)
			}
		}

		de := &models.DeliveryEvent{
			ID:            deliveryEventID,
			DestinationID: destinationID,
			Event: models.Event{
				ID:               eventID,
				TenantID:         tenantID,
				DestinationID:    destinationID,
				Topic:            topic,
				EligibleForRetry: eligibleForRetry,
				Time:             eventTime,
				Data:             data,
				Metadata:         metadata,
			},
			Delivery: &models.Delivery{
				ID:            deliveryID,
				EventID:       eventID,
				DestinationID: destinationID,
				Status:        status,
				Time:          deliveryTime,
				Code:          code,
				ResponseData:  responseData,
			},
		}

		results = append(results, rowData{de: de, eventTime: eventTime, deliveryTime: deliveryTime})
	}

	if err := rows.Err(); err != nil {
		return driver.ListDeliveryEventResponse{}, fmt.Errorf("rows error: %w", err)
	}

	// Handle pagination
	var hasMore bool
	if len(results) > limit {
		hasMore = true
		results = results[:limit] // Always keep the first `limit` items, remove the extra
	}

	// When going backward, we queried in reverse order, so reverse results back to normal order
	if goingBackward {
		for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
			results[i], results[j] = results[j], results[i]
		}
	}

	// Build response
	data := make([]*models.DeliveryEvent, len(results))
	for i, r := range results {
		data[i] = r.de
	}

	var nextEncoded, prevEncoded string
	if len(results) > 0 {
		getPosition := func(r rowData) string {
			if sortBy == "event_time" {
				// Composite cursor: eventTime::eventID::deliveryTime::deliveryID
				return fmt.Sprintf("%d::%s::%d::%s",
					r.eventTime.UnixMilli(),
					r.de.Event.ID,
					r.deliveryTime.UnixMilli(),
					r.de.Delivery.ID,
				)
			}
			// delivery_time cursor: deliveryTime::deliveryID
			return fmt.Sprintf("%d::%s", r.deliveryTime.UnixMilli(), r.de.Delivery.ID)
		}

		encodeCursor := func(position string) string {
			return cursor.Encode(cursor.Cursor{
				SortBy:    sortBy,
				SortOrder: sortOrder,
				Position:  position,
			})
		}

		if !prevCursor.IsEmpty() {
			nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			if hasMore {
				prevEncoded = encodeCursor(getPosition(results[0]))
			}
		} else if !nextCursor.IsEmpty() {
			prevEncoded = encodeCursor(getPosition(results[0]))
			if hasMore {
				nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			}
		} else {
			if hasMore {
				nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			}
		}
	}

	return driver.ListDeliveryEventResponse{
		Data: data,
		Next: nextEncoded,
		Prev: prevEncoded,
	}, nil
}

func (s *logStoreImpl) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	// Build conditions dynamically to support optional tenant_id
	var conditions []string
	var args []interface{}

	// Optional: tenant_id filter (skip if empty to search across all tenants)
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	// Required: event_id
	conditions = append(conditions, "event_id = ?")
	args = append(args, req.EventID)

	// Optional: destination_id filter
	if req.DestinationID != "" {
		conditions = append(conditions, "destination_id = ?")
		args = append(args, req.DestinationID)
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			destination_id,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data
		FROM event_log
		WHERE %s
		LIMIT 1`, whereClause)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	var metadataStr, dataStr string
	event := &models.Event{}

	if err := rows.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Time,
		&metadataStr,
		&dataStr,
	); err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	// Parse JSON fields
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

	return event, nil
}

// RetrieveDeliveryEvent retrieves a single delivery event by delivery ID.
func (s *logStoreImpl) RetrieveDeliveryEvent(ctx context.Context, req driver.RetrieveDeliveryEventRequest) (*models.DeliveryEvent, error) {
	// Build conditions dynamically to support optional tenant_id
	var conditions []string
	var args []interface{}

	// Optional: tenant_id filter (skip if empty to search across all tenants)
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	// Required: delivery_id
	conditions = append(conditions, "delivery_id = ?")
	args = append(args, req.DeliveryID)

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			event_id,
			tenant_id,
			destination_id,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data,
			delivery_id,
			delivery_event_id,
			status,
			delivery_time,
			code,
			response_data
		FROM event_log
		WHERE %s
		LIMIT 1`, whereClause)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	var (
		eventID          string
		tenantID         string
		destinationID    string
		topic            string
		eligibleForRetry bool
		eventTime        time.Time
		metadataStr      string
		dataStr          string
		deliveryID       string
		deliveryEventID  string
		status           string
		deliveryTime     time.Time
		code             string
		responseDataStr  string
	)

	err = rows.Scan(
		&eventID,
		&tenantID,
		&destinationID,
		&topic,
		&eligibleForRetry,
		&eventTime,
		&metadataStr,
		&dataStr,
		&deliveryID,
		&deliveryEventID,
		&status,
		&deliveryTime,
		&code,
		&responseDataStr,
	)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	// Parse JSON fields
	var metadata map[string]string
	var data map[string]interface{}
	var responseData map[string]interface{}

	if metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}
	if dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal data: %w", err)
		}
	}
	if responseDataStr != "" {
		if err := json.Unmarshal([]byte(responseDataStr), &responseData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response_data: %w", err)
		}
	}

	return &models.DeliveryEvent{
		ID:            deliveryEventID,
		DestinationID: destinationID,
		Event: models.Event{
			ID:               eventID,
			TenantID:         tenantID,
			DestinationID:    destinationID,
			Topic:            topic,
			EligibleForRetry: eligibleForRetry,
			Time:             eventTime,
			Data:             data,
			Metadata:         metadata,
		},
		Delivery: &models.Delivery{
			ID:            deliveryID,
			EventID:       eventID,
			DestinationID: destinationID,
			Status:        status,
			Time:          deliveryTime,
			Code:          code,
			ResponseData:  responseData,
		},
	}, nil
}

func (s *logStoreImpl) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	if len(deliveryEvents) == 0 {
		return nil
	}

	// Write to events table (ReplacingMergeTree deduplicates by ORDER BY)
	eventBatch, err := s.chDB.PrepareBatch(ctx,
		`INSERT INTO events (
			event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data
		)`,
	)
	if err != nil {
		return fmt.Errorf("prepare events batch failed: %w", err)
	}

	for _, de := range deliveryEvents {
		metadataJSON, err := json.Marshal(de.Event.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		dataJSON, err := json.Marshal(de.Event.Data)
		if err != nil {
			return fmt.Errorf("failed to marshal data: %w", err)
		}

		if err := eventBatch.Append(
			de.Event.ID,
			de.Event.TenantID,
			de.DestinationID,
			de.Event.Topic,
			de.Event.EligibleForRetry,
			de.Event.Time,
			string(metadataJSON),
			string(dataJSON),
		); err != nil {
			return fmt.Errorf("events batch append failed: %w", err)
		}
	}

	if err := eventBatch.Send(); err != nil {
		return fmt.Errorf("events batch send failed: %w", err)
	}

	// Write to event_log table (deliveries)
	deliveryBatch, err := s.chDB.PrepareBatch(ctx,
		`INSERT INTO event_log (
			event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
			delivery_id, delivery_event_id, status, delivery_time, code, response_data
		)`,
	)
	if err != nil {
		return fmt.Errorf("prepare event_log batch failed: %w", err)
	}

	for _, de := range deliveryEvents {
		metadataJSON, err := json.Marshal(de.Event.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		dataJSON, err := json.Marshal(de.Event.Data)
		if err != nil {
			return fmt.Errorf("failed to marshal data: %w", err)
		}

		var deliveryID, status, code string
		var deliveryTime time.Time
		var responseDataJSON []byte

		if de.Delivery != nil {
			deliveryID = de.Delivery.ID
			status = de.Delivery.Status
			deliveryTime = de.Delivery.Time
			code = de.Delivery.Code
			responseDataJSON, err = json.Marshal(de.Delivery.ResponseData)
			if err != nil {
				return fmt.Errorf("failed to marshal response_data: %w", err)
			}
		} else {
			deliveryID = de.ID
			status = "pending"
			deliveryTime = de.Event.Time
			code = ""
			responseDataJSON = []byte("{}")
		}

		if err := deliveryBatch.Append(
			de.Event.ID,
			de.Event.TenantID,
			de.DestinationID,
			de.Event.Topic,
			de.Event.EligibleForRetry,
			de.Event.Time,
			string(metadataJSON),
			string(dataJSON),
			deliveryID,
			de.ID,
			status,
			deliveryTime,
			code,
			string(responseDataJSON),
		); err != nil {
			return fmt.Errorf("event_log batch append failed: %w", err)
		}
	}

	if err := deliveryBatch.Send(); err != nil {
		return fmt.Errorf("event_log batch send failed: %w", err)
	}

	return nil
}

// buildCursorCondition builds a SQL condition for cursor-based pagination
func buildCursorCondition(sortBy, sortOrder, position string, isBackward bool) string {
	// Parse position based on sortBy
	// For delivery_time: "timestamp::deliveryID"
	// For event_time: "timestamp::eventID::timestamp::deliveryID"

	if sortBy == "event_time" {
		// Parse: eventTimeMs::eventID::deliveryTimeMs::deliveryID
		parts := strings.SplitN(position, "::", 4)
		if len(parts) != 4 {
			return "1=1" // invalid cursor, return always true
		}
		eventTimeMs := parts[0]
		eventID := parts[1]
		deliveryTimeMs := parts[2]
		deliveryID := parts[3]

		// Determine comparison direction
		var cmp string
		if sortOrder == "desc" {
			if isBackward {
				cmp = ">"
			} else {
				cmp = "<"
			}
		} else {
			if isBackward {
				cmp = "<"
			} else {
				cmp = ">"
			}
		}

		// Build multi-column comparison
		// (event_time, event_id, delivery_time, delivery_id) < (cursor_values)
		return fmt.Sprintf(`(
			event_time %s fromUnixTimestamp64Milli(%s)
			OR (event_time = fromUnixTimestamp64Milli(%s) AND event_id %s '%s')
			OR (event_time = fromUnixTimestamp64Milli(%s) AND event_id = '%s' AND delivery_time %s fromUnixTimestamp64Milli(%s))
			OR (event_time = fromUnixTimestamp64Milli(%s) AND event_id = '%s' AND delivery_time = fromUnixTimestamp64Milli(%s) AND delivery_id %s '%s')
		)`,
			cmp, eventTimeMs,
			eventTimeMs, cmp, eventID,
			eventTimeMs, eventID, cmp, deliveryTimeMs,
			eventTimeMs, eventID, deliveryTimeMs, cmp, deliveryID,
		)
	}

	// delivery_time sort: "timestamp::deliveryID"
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1"
	}
	deliveryTimeMs := parts[0]
	deliveryID := parts[1]

	var cmp string
	if sortOrder == "desc" {
		if isBackward {
			cmp = ">"
		} else {
			cmp = "<"
		}
	} else {
		if isBackward {
			cmp = "<"
		} else {
			cmp = ">"
		}
	}

	return fmt.Sprintf(`(
		delivery_time %s fromUnixTimestamp64Milli(%s)
		OR (delivery_time = fromUnixTimestamp64Milli(%s) AND delivery_id %s '%s')
	)`, cmp, deliveryTimeMs, deliveryTimeMs, cmp, deliveryID)
}
