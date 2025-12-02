package chlogstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
)

// ErrDeploymentIDNotSupported is returned when deployment_id is provided but not supported.
var ErrDeploymentIDNotSupported = errors.New("clickhouse logstore does not support deployment_id")

type logStoreImpl struct {
	chDB clickhouse.DB
}

var _ driver.LogStore = (*logStoreImpl)(nil)

func NewLogStore(chDB clickhouse.DB, deploymentID string) (driver.LogStore, error) {
	if deploymentID != "" {
		return nil, ErrDeploymentIDNotSupported
	}
	return &logStoreImpl{chDB: chDB}, nil
}

func (s *logStoreImpl) ListEvent(ctx context.Context, request driver.ListEventRequest) (driver.ListEventResponse, error) {
	// Set default time range
	start := request.Start
	end := request.End
	if start == nil && end == nil {
		now := time.Now()
		oneHourAgo := now.Add(-1 * time.Hour)
		start = &oneHourAgo
		end = &now
	} else if start == nil && end != nil {
		oneHourBefore := end.Add(-1 * time.Hour)
		start = &oneHourBefore
	} else if start != nil && end == nil {
		now := time.Now()
		end = &now
	}

	limit := request.Limit
	if limit == 0 {
		limit = 100
	}

	// Build query - single table, no JOINs needed
	// We get the latest row per event_id (max delivery_time)
	// Use Unix milliseconds for DateTime64(3) columns to preserve precision
	var args []interface{}
	args = append(args, request.TenantID, start.UnixMilli(), end.UnixMilli())

	// Build filter clauses for WHERE (on raw table columns with alias)
	var destFilter, topicFilter string

	if len(request.DestinationIDs) > 0 {
		args = append(args, request.DestinationIDs)
		destFilter = " AND e.destination_id IN (?)"
	}

	if len(request.Topics) > 0 {
		args = append(args, request.Topics)
		topicFilter = " AND e.topic IN (?)"
	}

	// Handle cursor pagination and status filter
	// Since status is computed via argMax in SELECT, we filter in HAVING clause
	var havingClauses []string
	var havingArgs []interface{}
	isBackward := false // true when using Prev cursor (going backward)

	// Status filter - uses HAVING since status is an aggregate
	// Reference the SELECT alias 'status' which is argMax(e.status, e.delivery_time)
	if request.Status != "" {
		havingClauses = append(havingClauses, "status = ?")
		havingArgs = append(havingArgs, request.Status)
	}

	if request.Next != "" {
		cursorTime, cursorID, err := parseCursor(request.Next)
		if err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("invalid next cursor: %w", err)
		}
		// For next page (DESC order): get records with time < cursor OR (time == cursor AND id < cursor_id)
		havingClauses = append(havingClauses, "(event_time < fromUnixTimestamp64Milli(?) OR (event_time = fromUnixTimestamp64Milli(?) AND e.event_id < ?))")
		havingArgs = append(havingArgs, cursorTime.UnixMilli(), cursorTime.UnixMilli(), cursorID)
	} else if request.Prev != "" {
		cursorTime, cursorID, err := parseCursor(request.Prev)
		if err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("invalid prev cursor: %w", err)
		}
		// For prev page: get records with time > cursor OR (time == cursor AND id > cursor_id)
		// We'll query in ASC order and reverse the results
		havingClauses = append(havingClauses, "(event_time > fromUnixTimestamp64Milli(?) OR (event_time = fromUnixTimestamp64Milli(?) AND e.event_id > ?))")
		havingArgs = append(havingArgs, cursorTime.UnixMilli(), cursorTime.UnixMilli(), cursorID)
		isBackward = true
	}

	// Build HAVING clause
	var havingClause string
	if len(havingClauses) > 0 {
		havingClause = " HAVING " + strings.Join(havingClauses, " AND ")
	}

	orderBy := "ORDER BY event_time DESC, event_id DESC"
	if isBackward {
		orderBy = "ORDER BY event_time ASC, event_id ASC"
	}

	// Query to get latest status per event
	// Use table alias to avoid confusion between raw columns and aggregated output
	query := fmt.Sprintf(`
		SELECT
			e.event_id,
			any(e.tenant_id) as tenant_id,
			any(e.destination_id) as destination_id,
			any(e.topic) as topic,
			any(e.eligible_for_retry) as eligible_for_retry,
			max(e.event_time) as event_time,
			any(e.metadata) as metadata,
			any(e.data) as data,
			argMax(e.status, e.delivery_time) as status
		FROM event_log AS e
		WHERE e.tenant_id = ?
			AND e.event_time >= fromUnixTimestamp64Milli(?)
			AND e.event_time <= fromUnixTimestamp64Milli(?)
			%s
			%s
		GROUP BY e.event_id
		%s
		%s
		LIMIT %d
	`, destFilter, topicFilter, havingClause, orderBy, limit+1)

	// Append having args after the main args
	args = append(args, havingArgs...)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var events []*models.Event
	for rows.Next() {
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
			&event.Status,
		); err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("scan failed: %w", err)
		}

		if metadataStr != "" {
			if err := json.Unmarshal([]byte(metadataStr), &event.Metadata); err != nil {
				return driver.ListEventResponse{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}
		if dataStr != "" {
			if err := json.Unmarshal([]byte(dataStr), &event.Data); err != nil {
				return driver.ListEventResponse{}, fmt.Errorf("failed to unmarshal data: %w", err)
			}
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("rows iteration error: %w", err)
	}

	// Handle pagination
	hasMore := len(events) > int(limit)
	if hasMore {
		events = events[:limit]
	}

	// For backward pagination, reverse the results to maintain DESC order
	if isBackward {
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
	}

	// Determine hasNext/hasPrev based on pagination direction
	var hasNext, hasPrev bool
	if isBackward {
		// Going backward: hasMore means there are more prev pages, and we came from a next cursor so hasNext is true
		hasPrev = hasMore
		hasNext = request.Prev != "" // We used Prev cursor to get here, so there's always a next page
	} else {
		// Going forward: hasMore means there are more next pages
		hasNext = hasMore
		hasPrev = request.Next != "" // We used Next cursor to get here, so there's always a prev page
	}

	// Build cursors
	var nextCursor, prevCursor string
	if len(events) > 0 {
		if hasNext {
			lastEvent := events[len(events)-1]
			nextCursor = formatCursor(lastEvent.Time, lastEvent.ID)
		}
		if hasPrev {
			firstEvent := events[0]
			prevCursor = formatCursor(firstEvent.Time, firstEvent.ID)
		}
	}

	// Count query
	var countArgs []interface{}
	countArgs = append(countArgs, request.TenantID, start.UnixMilli(), end.UnixMilli())

	countDestFilter := ""
	if len(request.DestinationIDs) > 0 {
		countArgs = append(countArgs, request.DestinationIDs)
		countDestFilter = " AND destination_id IN (?)"
	}

	countTopicFilter := ""
	if len(request.Topics) > 0 {
		countArgs = append(countArgs, request.Topics)
		countTopicFilter = " AND topic IN (?)"
	}

	// Build count query - if status filter is present, we need to count after grouping
	var countQuery string
	if request.Status != "" {
		countArgs = append(countArgs, request.Status)
		countQuery = fmt.Sprintf(`
			SELECT count(*) FROM (
				SELECT event_id
				FROM event_log
				WHERE tenant_id = ?
					AND event_time >= fromUnixTimestamp64Milli(?)
					AND event_time <= fromUnixTimestamp64Milli(?)
					%s
					%s
				GROUP BY event_id
				HAVING argMax(status, delivery_time) = ?
			)
		`, countDestFilter, countTopicFilter)
	} else {
		countQuery = fmt.Sprintf(`
			SELECT count(DISTINCT event_id)
			FROM event_log
			WHERE tenant_id = ?
				AND event_time >= fromUnixTimestamp64Milli(?)
				AND event_time <= fromUnixTimestamp64Milli(?)
				%s
				%s
		`, countDestFilter, countTopicFilter)
	}

	var totalCount uint64
	if err := s.chDB.QueryRow(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("count query failed: %w", err)
	}

	return driver.ListEventResponse{
		Data:  events,
		Next:  nextCursor,
		Prev:  prevCursor,
		Count: int64(totalCount),
	}, nil
}

func formatCursor(t time.Time, id string) string {
	return fmt.Sprintf("%d|%s", t.UnixMilli(), id)
}

func parseCursor(cursor string) (time.Time, string, error) {
	parts := strings.Split(cursor, "|")
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}

	var unixMilli int64
	if _, err := fmt.Sscanf(parts[0], "%d", &unixMilli); err != nil {
		return time.Time{}, "", fmt.Errorf("invalid timestamp in cursor: %w", err)
	}
	timestamp := time.UnixMilli(unixMilli)

	return timestamp, parts[1], nil
}

func (s *logStoreImpl) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	query := `
		SELECT
			event_id,
			tenant_id,
			destination_id,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data,
			argMax(status, delivery_time) as status
		FROM event_log
		WHERE tenant_id = ? AND event_id = ?
		GROUP BY event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data
		LIMIT 1
	`

	row := s.chDB.QueryRow(ctx, query, tenantID, eventID)

	var metadataStr, dataStr string
	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Time,
		&metadataStr,
		&dataStr,
		&event.Status,
	)
	if err != nil {
		if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, fmt.Errorf("query failed: %w", err)
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

func (s *logStoreImpl) RetrieveEventByDestination(ctx context.Context, tenantID, destinationID, eventID string) (*models.Event, error) {
	query := `
		SELECT
			event_id,
			tenant_id,
			destination_id,
			topic,
			eligible_for_retry,
			event_time,
			metadata,
			data,
			argMax(status, delivery_time) as status
		FROM event_log
		WHERE tenant_id = ? AND destination_id = ? AND event_id = ?
		GROUP BY event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data
		LIMIT 1
	`

	row := s.chDB.QueryRow(ctx, query, tenantID, destinationID, eventID)

	var metadataStr, dataStr string
	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Time,
		&metadataStr,
		&dataStr,
		&event.Status,
	)
	if err != nil {
		if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, fmt.Errorf("query failed: %w", err)
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

func (s *logStoreImpl) ListDelivery(ctx context.Context, request driver.ListDeliveryRequest) ([]*models.Delivery, error) {
	query := `
		SELECT
			delivery_id,
			delivery_event_id,
			event_id,
			destination_id,
			status,
			delivery_time,
			code,
			response_data
		FROM event_log
		WHERE tenant_id = ?
			AND event_id = ?
			AND delivery_id != ''
		ORDER BY delivery_time DESC
	`

	rows, err := s.chDB.Query(ctx, query, request.TenantID, request.EventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []*models.Delivery
	for rows.Next() {
		var responseDataStr string
		var code string
		delivery := &models.Delivery{}
		if err := rows.Scan(
			&delivery.ID,
			&delivery.DeliveryEventID,
			&delivery.EventID,
			&delivery.DestinationID,
			&delivery.Status,
			&delivery.Time,
			&code,
			&responseDataStr,
		); err != nil {
			return nil, err
		}
		delivery.Code = code
		if responseDataStr != "" {
			if err := json.Unmarshal([]byte(responseDataStr), &delivery.ResponseData); err != nil {
				return nil, fmt.Errorf("failed to unmarshal response_data: %w", err)
			}
		}
		deliveries = append(deliveries, delivery)
	}

	return deliveries, nil
}

func (s *logStoreImpl) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	if len(deliveryEvents) == 0 {
		return nil
	}

	batch, err := s.chDB.PrepareBatch(ctx,
		`INSERT INTO event_log (
			event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
			delivery_id, delivery_event_id, status, delivery_time, code, response_data
		)`,
	)
	if err != nil {
		return err
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

		// Determine delivery fields
		var deliveryID, deliveryEventID, status, code string
		var deliveryTime time.Time
		var responseDataJSON []byte

		if de.Delivery != nil {
			deliveryID = de.Delivery.ID
			deliveryEventID = de.Delivery.DeliveryEventID
			status = de.Delivery.Status
			deliveryTime = de.Delivery.Time
			code = de.Delivery.Code
			responseDataJSON, err = json.Marshal(de.Delivery.ResponseData)
			if err != nil {
				return fmt.Errorf("failed to marshal response_data: %w", err)
			}
		} else {
			// Pending event - no delivery yet.
			// We set delivery_time = event_time as a placeholder. This is semantically
			// incorrect (there's no actual delivery), but necessary because we use
			// argMax(status, delivery_time) to determine the latest status. By using
			// event_time, pending status will always "lose" to any real delivery
			// (which will have a later delivery_time), ensuring correct status resolution.
			deliveryID = ""
			deliveryEventID = de.ID
			status = "pending"
			deliveryTime = de.Event.Time
			code = ""
			responseDataJSON = []byte("{}")
		}

		if err := batch.Append(
			de.Event.ID,
			de.Event.TenantID,
			de.DestinationID,
			de.Event.Topic,
			de.Event.EligibleForRetry,
			de.Event.Time,
			string(metadataJSON),
			string(dataJSON),
			deliveryID,
			deliveryEventID,
			status,
			deliveryTime,
			code,
			string(responseDataJSON),
		); err != nil {
			return err
		}
	}

	return batch.Send()
}
