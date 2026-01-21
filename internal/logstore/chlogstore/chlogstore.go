package chlogstore

import (
	"context"
	"encoding/json"
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

// eventWithPosition wraps an event with its cursor position data.
type eventWithPosition struct {
	*models.Event
	eventTime time.Time
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
			query, args := buildEventQuery(s.eventsTable, req, q)
			rows, err := s.chDB.Query(ctx, query, args...)
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

func buildEventQuery(table string, req driver.ListEventRequest, q pagination.QueryInput) (string, []any) {
	var conditions []string
	var args []any

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
	`, table, whereClause, orderByClause, q.Limit)

	return query, args
}

func scanEvents(rows clickhouse.Rows) ([]eventWithPosition, error) {
	var results []eventWithPosition
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
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		var metadata map[string]string
		var data map[string]any

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

		results = append(results, eventWithPosition{
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

// deliveryRecordWithPosition wraps a delivery record with its cursor position data.
type deliveryRecordWithPosition struct {
	*driver.DeliveryRecord
	deliveryTime time.Time
}

func (s *logStoreImpl) ListDelivery(ctx context.Context, req driver.ListDeliveryRequest) (driver.ListDeliveryResponse, error) {
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	res, err := pagination.Run(ctx, pagination.Config[deliveryRecordWithPosition]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(ctx context.Context, q pagination.QueryInput) ([]deliveryRecordWithPosition, error) {
			query, args := buildDeliveryQuery(s.deliveriesTable, req, q)
			rows, err := s.chDB.Query(ctx, query, args...)
			if err != nil {
				return nil, fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()
			return scanDeliveryRecords(rows)
		},
		Cursor: pagination.Cursor[deliveryRecordWithPosition]{
			Encode: func(dr deliveryRecordWithPosition) string {
				position := fmt.Sprintf("%d::%s", dr.deliveryTime.UnixMilli(), dr.Delivery.ID)
				return cursor.Encode(cursorResourceDelivery, cursorVersion, position)
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

func buildDeliveryQuery(table string, req driver.ListDeliveryRequest, q pagination.QueryInput) (string, []any) {
	var conditions []string
	var args []any

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

	if req.TimeFilter.GTE != nil {
		conditions = append(conditions, "delivery_time >= ?")
		args = append(args, *req.TimeFilter.GTE)
	}
	if req.TimeFilter.LTE != nil {
		conditions = append(conditions, "delivery_time <= ?")
		args = append(args, *req.TimeFilter.LTE)
	}
	if req.TimeFilter.GT != nil {
		conditions = append(conditions, "delivery_time > ?")
		args = append(args, *req.TimeFilter.GT)
	}
	if req.TimeFilter.LT != nil {
		conditions = append(conditions, "delivery_time < ?")
		args = append(args, *req.TimeFilter.LT)
	}

	if q.CursorPos != "" {
		cursorCond, cursorArgs := buildDeliveryCursorCondition(q.Compare, q.CursorPos)
		conditions = append(conditions, cursorCond)
		args = append(args, cursorArgs...)
	}

	whereClause := strings.Join(conditions, " AND ")
	if whereClause == "" {
		whereClause = "1=1"
	}

	orderByClause := fmt.Sprintf("ORDER BY delivery_time %s, delivery_id %s",
		strings.ToUpper(q.SortDir), strings.ToUpper(q.SortDir))

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
	`, table, whereClause, orderByClause, q.Limit)

	return query, args
}

func scanDeliveryRecords(rows clickhouse.Rows) ([]deliveryRecordWithPosition, error) {
	var results []deliveryRecordWithPosition
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
		var data map[string]any
		var responseData map[string]any

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

		results = append(results, deliveryRecordWithPosition{
			DeliveryRecord: &driver.DeliveryRecord{
				Delivery: &models.Delivery{
					ID:            deliveryID,
					TenantID:      tenantID,
					EventID:       eventID,
					DestinationID: destinationID,
					Attempt:       int(attempt),
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
			deliveryTime: deliveryTime,
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

func (s *logStoreImpl) RetrieveDelivery(ctx context.Context, req driver.RetrieveDeliveryRequest) (*driver.DeliveryRecord, error) {
	var conditions []string
	var args []any

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
	var data map[string]any
	var responseData map[string]any

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

	return &driver.DeliveryRecord{
		Delivery: &models.Delivery{
			ID:            deliveryID,
			TenantID:      tenantID,
			EventID:       eventID,
			DestinationID: destinationID,
			Attempt:       int(attempt),
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

func (s *logStoreImpl) InsertMany(ctx context.Context, events []*models.Event, deliveries []*models.Delivery) error {
	if len(events) == 0 && len(deliveries) == 0 {
		return nil
	}

	if len(events) > 0 {
		eventBatch, err := s.chDB.PrepareBatch(ctx,
			fmt.Sprintf(`INSERT INTO %s (
				event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data
			)`, s.eventsTable),
		)
		if err != nil {
			return fmt.Errorf("prepare events batch failed: %w", err)
		}

		for _, e := range events {
			metadataJSON, err := json.Marshal(e.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}
			dataJSON, err := json.Marshal(e.Data)
			if err != nil {
				return fmt.Errorf("failed to marshal data: %w", err)
			}

			if err := eventBatch.Append(
				e.ID,
				e.TenantID,
				e.DestinationID,
				e.Topic,
				e.EligibleForRetry,
				e.Time,
				string(metadataJSON),
				string(dataJSON),
			); err != nil {
				return fmt.Errorf("events batch append failed: %w", err)
			}
		}

		if err := eventBatch.Send(); err != nil {
			return fmt.Errorf("events batch send failed: %w", err)
		}
	}

	if len(deliveries) > 0 {
		// Build a map of events for looking up event data when inserting deliveries
		eventMap := make(map[string]*models.Event)
		for _, e := range events {
			eventMap[e.ID] = e
		}

		deliveryBatch, err := s.chDB.PrepareBatch(ctx,
			fmt.Sprintf(`INSERT INTO %s (
				event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
				delivery_id, status, delivery_time, code, response_data, manual, attempt
			)`, s.deliveriesTable),
		)
		if err != nil {
			return fmt.Errorf("prepare deliveries batch failed: %w", err)
		}

		for _, d := range deliveries {
			event := eventMap[d.EventID]
			if event == nil {
				// If event not in current batch, use delivery's data as fallback
				event = &models.Event{
					ID:            d.EventID,
					TenantID:      d.TenantID,
					DestinationID: d.DestinationID,
				}
			}

			metadataJSON, err := json.Marshal(event.Metadata)
			if err != nil {
				return fmt.Errorf("failed to marshal metadata: %w", err)
			}
			dataJSON, err := json.Marshal(event.Data)
			if err != nil {
				return fmt.Errorf("failed to marshal data: %w", err)
			}
			responseDataJSON, err := json.Marshal(d.ResponseData)
			if err != nil {
				return fmt.Errorf("failed to marshal response_data: %w", err)
			}

			// Use event.TenantID for tenant_id since it's denormalized from the event
			if err := deliveryBatch.Append(
				d.EventID,
				event.TenantID, // Use event's TenantID, not delivery's
				d.DestinationID,
				event.Topic,
				event.EligibleForRetry,
				event.Time,
				string(metadataJSON),
				string(dataJSON),
				d.ID,
				d.Status,
				d.Time,
				d.Code,
				string(responseDataJSON),
				d.Manual,
				uint32(d.Attempt),
			); err != nil {
				return fmt.Errorf("deliveries batch append failed: %w", err)
			}
		}

		if err := deliveryBatch.Send(); err != nil {
			return fmt.Errorf("deliveries batch send failed: %w", err)
		}
	}

	return nil
}

func parseTimestampMs(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func buildDeliveryCursorCondition(compare, position string) (string, []any) {
	parts := strings.SplitN(position, "::", 2)
	if len(parts) != 2 {
		return "1=1", nil
	}
	deliveryTimeMs, err := parseTimestampMs(parts[0])
	if err != nil {
		return "1=1", nil // invalid timestamp, return always true
	}
	deliveryID := parts[1]

	condition := fmt.Sprintf(`(
		delivery_time %s fromUnixTimestamp64Milli(?)
		OR (delivery_time = fromUnixTimestamp64Milli(?) AND delivery_id %s ?)
	)`, compare, compare)

	return condition, []any{deliveryTimeMs, deliveryTimeMs, deliveryID}
}
