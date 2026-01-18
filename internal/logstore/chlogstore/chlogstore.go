package chlogstore

import (
	"context"
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
)

const (
	cursorResourceEvent    = "evt"
	cursorResourceDelivery = "dlv"
	cursorVersion          = 1
)

type logStoreImpl struct {
	chDB            clickhouse.DB
	eventsTable     string
	deliveriesTable string
}

var _ driver.LogStore = (*logStoreImpl)(nil)

func NewLogStore(chDB clickhouse.DB, deploymentID string) driver.LogStore {
	prefix := ""
	if deploymentID != "" {
		prefix = deploymentID + "_"
	}
	return &logStoreImpl{
		chDB:            chDB,
		eventsTable:     prefix + "events",
		deliveriesTable: prefix + "deliveries",
	}
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

	nextPosition, err := cursor.Decode(req.Next, cursorResourceEvent, cursorVersion)
	if err != nil {
		return driver.ListEventResponse{}, convertCursorError(err)
	}
	prevPosition, err := cursor.Decode(req.Prev, cursorResourceEvent, cursorVersion)
	if err != nil {
		return driver.ListEventResponse{}, convertCursorError(err)
	}

	goingBackward := prevPosition != ""

	// Multi-column ORDER BY for deterministic pagination
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

	var conditions []string
	var args []interface{}

	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	if len(req.DestinationIDs) > 0 {
		conditions = append(conditions, "destination_id IN ?")
		args = append(args, req.DestinationIDs)
	}

	if len(req.Topics) > 0 {
		conditions = append(conditions, "topic IN ?")
		args = append(args, req.Topics)
	}

	if req.EventStart != nil {
		conditions = append(conditions, "event_time >= ?")
		args = append(args, *req.EventStart)
	}
	if req.EventEnd != nil {
		conditions = append(conditions, "event_time <= ?")
		args = append(args, *req.EventEnd)
	}

	if nextPosition != "" {
		cursorCond, cursorArgs := buildEventCursorCondition(sortOrder, nextPosition, false)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
	} else if prevPosition != "" {
		cursorCond, cursorArgs := buildEventCursorCondition(sortOrder, prevPosition, true)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
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
		FROM %s
		WHERE %s
		%s
		LIMIT %d
	`, s.eventsTable, whereClause, orderByClause, limit+1)

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

	data := make([]*models.Event, len(results))
	for i, r := range results {
		data[i] = r.event
	}

	var nextEncoded, prevEncoded string
	if len(results) > 0 {
		getPosition := func(r rowData) string {
			return fmt.Sprintf("%d::%s", r.eventTime.UnixMilli(), r.event.ID)
		}

		encodeCursor := func(position string) string {
			return cursor.Encode(cursorResourceEvent, cursorVersion, position)
		}

		if prevPosition != "" {
			nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			if hasMore {
				prevEncoded = encodeCursor(getPosition(results[0]))
			}
		} else if nextPosition != "" {
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

func buildEventCursorCondition(sortOrder, position string, isBackward bool) (string, []interface{}) {
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1", nil // invalid cursor, return always true
	}
	eventTimeMs, err := parseTimestampMs(parts[0])
	if err != nil {
		return "1=1", nil // invalid timestamp, return always true
	}
	eventID := parts[1]

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

	condition := fmt.Sprintf(`(
		event_time %s fromUnixTimestamp64Milli(?)
		OR (event_time = fromUnixTimestamp64Milli(?) AND event_id %s ?)
	)`, cmp, cmp)

	return condition, []interface{}{eventTimeMs, eventTimeMs, eventID}
}

func (s *logStoreImpl) ListDeliveryEvent(ctx context.Context, req driver.ListDeliveryEventRequest) (driver.ListDeliveryEventResponse, error) {
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	nextPosition, err := cursor.Decode(req.Next, cursorResourceDelivery, cursorVersion)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, convertCursorError(err)
	}
	prevPosition, err := cursor.Decode(req.Prev, cursorResourceDelivery, cursorVersion)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, convertCursorError(err)
	}

	goingBackward := prevPosition != ""

	var orderByClause string
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

	var conditions []string
	var args []interface{}

	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

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

	if req.Start != nil {
		conditions = append(conditions, "delivery_time >= ?")
		args = append(args, *req.Start)
	}
	if req.End != nil {
		conditions = append(conditions, "delivery_time <= ?")
		args = append(args, *req.End)
	}

	if nextPosition != "" {
		cursorCond, cursorArgs := buildCursorCondition(sortOrder, nextPosition, false)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
	} else if prevPosition != "" {
		cursorCond, cursorArgs := buildCursorCondition(sortOrder, prevPosition, true)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
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
			response_data,
			manual,
			attempt
		FROM %s
		WHERE %s
		%s
		LIMIT %d
	`, s.deliveriesTable, whereClause, orderByClause, limit+1)

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
			manual           bool
			attempt          uint32
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
			&manual,
			&attempt,
		)
		if err != nil {
			return driver.ListDeliveryEventResponse{}, fmt.Errorf("scan failed: %w", err)
		}

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
			Manual:        manual,
			Attempt:       int(attempt),
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

	var hasMore bool
	if len(results) > limit {
		hasMore = true
		results = results[:limit]
	}

	if goingBackward {
		for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
			results[i], results[j] = results[j], results[i]
		}
	}

	data := make([]*models.DeliveryEvent, len(results))
	for i, r := range results {
		data[i] = r.de
	}

	var nextEncoded, prevEncoded string
	if len(results) > 0 {
		getPosition := func(r rowData) string {
			return fmt.Sprintf("%d::%s", r.deliveryTime.UnixMilli(), r.de.Delivery.ID)
		}

		encodeCursor := func(position string) string {
			return cursor.Encode(cursorResourceDelivery, cursorVersion, position)
		}

		if prevPosition != "" {
			nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			if hasMore {
				prevEncoded = encodeCursor(getPosition(results[0]))
			}
		} else if nextPosition != "" {
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
	var conditions []string
	var args []interface{}

	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

	conditions = append(conditions, "event_id = ?")
	args = append(args, req.EventID)

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
		FROM %s
		WHERE %s
		LIMIT 1`, s.deliveriesTable, whereClause)

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

func (s *logStoreImpl) RetrieveDeliveryEvent(ctx context.Context, req driver.RetrieveDeliveryEventRequest) (*models.DeliveryEvent, error) {
	var conditions []string
	var args []interface{}

	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}

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
			response_data,
			manual,
			attempt
		FROM %s
		WHERE %s
		LIMIT 1`, s.deliveriesTable, whereClause)

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
		manual           bool
		attempt          uint32
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
		&manual,
		&attempt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

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
		Manual:        manual,
		Attempt:       int(attempt),
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

	eventBatch, err := s.chDB.PrepareBatch(ctx,
		fmt.Sprintf(`INSERT INTO %s (
			event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data
		)`, s.eventsTable),
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

	deliveryBatch, err := s.chDB.PrepareBatch(ctx,
		fmt.Sprintf(`INSERT INTO %s (
			event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
			delivery_id, delivery_event_id, status, delivery_time, code, response_data, manual, attempt
		)`, s.deliveriesTable),
	)
	if err != nil {
		return fmt.Errorf("prepare deliveries batch failed: %w", err)
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
			de.Manual,
			uint32(de.Attempt),
		); err != nil {
			return fmt.Errorf("deliveries batch append failed: %w", err)
		}
	}

	if err := deliveryBatch.Send(); err != nil {
		return fmt.Errorf("deliveries batch send failed: %w", err)
	}

	return nil
}

func parseTimestampMs(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func buildCursorCondition(sortOrder, position string, isBackward bool) (string, []interface{}) {
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1", nil
	}
	deliveryTimeMs, err := parseTimestampMs(parts[0])
	if err != nil {
		return "1=1", nil // invalid timestamp, return always true
	}
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

	condition := fmt.Sprintf(`(
		delivery_time %s fromUnixTimestamp64Milli(?)
		OR (delivery_time = fromUnixTimestamp64Milli(?) AND delivery_id %s ?)
	)`, cmp, cmp)

	return condition, []interface{}{deliveryTimeMs, deliveryTimeMs, deliveryID}
}

// convertCursorError converts cursor package errors to driver errors.
func convertCursorError(err error) error {
	if errors.Is(err, cursor.ErrInvalidCursor) || errors.Is(err, cursor.ErrVersionMismatch) {
		return driver.ErrInvalidCursor
	}
	return err
}
