package chlogstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
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

func (s *logStoreImpl) ListEvent(ctx context.Context, request driver.ListEventRequest) (driver.ListEventResponse, error) {
	// Build the main query with CTE to get events with their latest delivery status
	// Decision: Use CTEs similar to PG approach for clarity, ClickHouse's argMax is perfect for getting latest status

	// Set default time range
	start := request.Start
	end := request.End
	if start == nil && end == nil {
		// Default to last 1 hour
		now := time.Now()
		oneHourAgo := now.Add(-1 * time.Hour)
		start = &oneHourAgo
		end = &now
	} else if start == nil && end != nil {
		// Default start to end - 1 hour
		oneHourBefore := end.Add(-1 * time.Hour)
		start = &oneHourBefore
	} else if start != nil && end == nil {
		// Default end to now
		now := time.Now()
		end = &now
	}

	limit := request.Limit
	if limit == 0 {
		limit = 100 // Default limit
	}

	// Build dynamic query parts using simple ? placeholders
	// We'll provide args in order and duplicate where needed
	var destFilterSubquery, topicFilterSubquery string
	var destFilterMain, topicFilterMain string
	var cursorFilter string

	var args []interface{}

	// Base args for subquery: tenant_id, start, end
	args = append(args, request.TenantID, *start, *end)

	// Add destination filter for subquery
	if len(request.DestinationIDs) > 0 {
		args = append(args, request.DestinationIDs)
		destFilterSubquery = " AND destination_id IN (?)"
	}

	// Add topic filter for subquery
	if len(request.Topics) > 0 {
		args = append(args, request.Topics)
		topicFilterSubquery = " AND topic IN (?)"
	}

	// Now add args for main query: tenant_id, start, end (duplicated)
	args = append(args, request.TenantID, *start, *end)

	// Add destination filter for main query
	if len(request.DestinationIDs) > 0 {
		args = append(args, request.DestinationIDs)
		destFilterMain = " AND e.destination_id IN (?)"
	}

	// Add topic filter for main query
	if len(request.Topics) > 0 {
		args = append(args, request.Topics)
		topicFilterMain = " AND e.topic IN (?)"
	}

	// Add status filter (will use HAVING since status is an aggregate)
	var havingFilter string
	if request.Status != "" {
		args = append(args, request.Status)
		havingFilter = " HAVING status = ?"
	}

	// Add cursor filter and determine sort order
	// For Prev cursor: query ascending to get the right window, then reverse in code
	// For Next/no cursor: query descending
	var orderBy string
	if request.Prev != "" {
		cursorTime, cursorID, err := parseCursor(request.Prev)
		if err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("invalid prev cursor: %w", err)
		}
		args = append(args, cursorTime, cursorTime, cursorID)
		cursorFilter = " WHERE (time > ? OR (time = ? AND id > ?))"
		orderBy = "ORDER BY time ASC, id ASC" // Ascending for Prev to get right window
	} else {
		if request.Next != "" {
			cursorTime, cursorID, err := parseCursor(request.Next)
			if err != nil {
				return driver.ListEventResponse{}, fmt.Errorf("invalid next cursor: %w", err)
			}
			args = append(args, cursorTime, cursorTime, cursorID)
			cursorFilter = " WHERE (time < ? OR (time = ? AND id < ?))"
		}
		orderBy = "ORDER BY time DESC, id DESC" // Descending for Next/first page
	}

	query := fmt.Sprintf(`
		WITH latest_deliveries AS (
			SELECT
				event_id,
				argMax(status, time) as status,
				max(time) as delivery_time
			FROM deliveries
			WHERE event_id IN (
				SELECT DISTINCT id
				FROM events
				WHERE tenant_id = ?
					AND time >= ?
					AND time <= ?
					%s
					%s
			)
			GROUP BY event_id
		),
		events_with_status AS (
			SELECT
				e.id,
				argMax(e.tenant_id, e.time) as tenant_id,
				argMax(e.destination_id, e.time) as destination_id,
				max(e.time) as time,
				argMax(e.topic, e.time) as topic,
				argMax(e.eligible_for_retry, e.time) as eligible_for_retry,
				argMax(e.metadata, e.time) as metadata,
				argMax(e.data, e.time) as data,
				argMax(COALESCE(ld.status, 'pending'), e.time) as status,
				argMax(COALESCE(ld.delivery_time, e.time), e.time) as delivery_time
			FROM events e
			LEFT JOIN latest_deliveries ld ON e.id = ld.event_id
			WHERE e.tenant_id = ?
				AND e.time >= ?
				AND e.time <= ?
				%s
				%s
			GROUP BY e.id
			%s
		)
		SELECT * FROM events_with_status
		%s
		%s
		LIMIT %d
	`, destFilterSubquery, topicFilterSubquery, destFilterMain, topicFilterMain, havingFilter, cursorFilter, orderBy, limit+1)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var events []*models.Event
	for rows.Next() {
		var metadataStr, dataStr string
		var deliveryTime time.Time
		event := &models.Event{}

		if err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.DestinationID,
			&event.Time,
			&event.Topic,
			&event.EligibleForRetry,
			&metadataStr,
			&dataStr,
			&event.Status,
			&deliveryTime,
		); err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("scan failed: %w", err)
		}

		// Unmarshal JSON strings
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
	var hasNext, hasPrev bool
	if request.Prev != "" {
		// Going backward - we came backwards, so definitely more ahead
		// Events are in ASC order from query, trim last (oldest), then reverse to DESC
		hasNext = true
		hasPrev = len(events) > int(limit)
		if hasPrev {
			events = events[:len(events)-1] // Trim last item (oldest in ASC order)
		}
		// Reverse the slice to get DESC order
		for i := 0; i < len(events)/2; i++ {
			events[i], events[len(events)-1-i] = events[len(events)-1-i], events[i]
		}
	} else if request.Next != "" {
		// Going forward - we came forwards, so definitely more behind
		// Events are already in DESC order from query
		hasPrev = true
		hasNext = len(events) > int(limit)
		if hasNext {
			events = events[:limit] // Trim the extra item
		}
	} else {
		// First page - events are in DESC order from query
		hasPrev = false
		hasNext = len(events) > int(limit)
		if hasNext {
			events = events[:limit] // Trim the extra item
		}
	}

	// Get total count (separate query)
	// Decision: Use a separate count query for accuracy (ClickHouse is fast at this)
	countQuery := fmt.Sprintf(`
		WITH latest_deliveries AS (
			SELECT
				event_id,
				argMax(status, time) as status
			FROM deliveries
			WHERE event_id IN (
				SELECT DISTINCT id
				FROM events
				WHERE tenant_id = ?
					AND time >= ?
					AND time <= ?
					%s
					%s
			)
			GROUP BY event_id
		)
		SELECT COUNT(*) FROM (
			SELECT e.id, argMax(COALESCE(ld.status, 'pending'), e.time) as status
			FROM events e
			LEFT JOIN latest_deliveries ld ON e.id = ld.event_id
			WHERE e.tenant_id = ?
				AND e.time >= ?
				AND e.time <= ?
				%s
				%s
			GROUP BY e.id
			%s
		)
	`, destFilterSubquery, topicFilterSubquery, destFilterMain, topicFilterMain, havingFilter)

	// Build count args (same as query args but without cursor)
	var countArgs []interface{}
	countArgs = append(countArgs, request.TenantID, *start, *end)
	if len(request.DestinationIDs) > 0 {
		countArgs = append(countArgs, request.DestinationIDs)
	}
	if len(request.Topics) > 0 {
		countArgs = append(countArgs, request.Topics)
	}
	countArgs = append(countArgs, request.TenantID, *start, *end)
	if len(request.DestinationIDs) > 0 {
		countArgs = append(countArgs, request.DestinationIDs)
	}
	if len(request.Topics) > 0 {
		countArgs = append(countArgs, request.Topics)
	}
	if request.Status != "" {
		countArgs = append(countArgs, request.Status)
	}

	var totalCount uint64
	if err := s.chDB.QueryRow(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("count query failed: %w", err)
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

	return driver.ListEventResponse{
		Data:  events,
		Next:  nextCursor,
		Prev:  prevCursor,
		Count: int64(totalCount),
	}, nil
}

// formatCursor creates a cursor from time and ID
// Decision: Use simple "timestamp|id" format
func formatCursor(t time.Time, id string) string {
	return fmt.Sprintf("%d|%s", t.Unix(), id)
}

// parseCursor extracts time and ID from cursor
func parseCursor(cursor string) (time.Time, string, error) {
	parts := strings.Split(cursor, "|")
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}

	var unixTime int64
	if _, err := fmt.Sscanf(parts[0], "%d", &unixTime); err != nil {
		return time.Time{}, "", fmt.Errorf("invalid timestamp in cursor: %w", err)
	}
	timestamp := time.Unix(unixTime, 0)

	return timestamp, parts[1], nil
}

func (s *logStoreImpl) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	// Query event with status calculation
	// Decision: Use a CTE to get the latest delivery status, similar to PG approach
	query := `
		WITH latest_delivery AS (
			SELECT argMax(status, time) as status
			FROM deliveries
			WHERE event_id = ?
		)
		SELECT
			e.id,
			e.tenant_id,
			e.destination_id,
			e.time,
			e.topic,
			e.eligible_for_retry,
			e.metadata,
			e.data,
			COALESCE(ld.status, 'pending') as status
		FROM events e
		LEFT JOIN latest_delivery ld ON true
		WHERE e.tenant_id = ? AND e.id = ?
	`

	row := s.chDB.QueryRow(ctx, query, eventID, tenantID, eventID)

	var metadataStr, dataStr string
	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Time,
		&event.Topic,
		&event.EligibleForRetry,
		&metadataStr,
		&dataStr,
		&event.Status,
	)
	if err != nil {
		// ClickHouse returns an error when no rows, not sql.ErrNoRows
		if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// Unmarshal JSON strings
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
			id,
			event_id,
			destination_id,
			status,
			time
		FROM deliveries
		WHERE event_id = ?
		ORDER BY time DESC
	`
	rows, err := s.chDB.Query(ctx, query, request.EventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []*models.Delivery
	for rows.Next() {
		delivery := &models.Delivery{}
		if err := rows.Scan(
			&delivery.ID,
			&delivery.EventID,
			&delivery.DestinationID,
			&delivery.Status,
			&delivery.Time,
		); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}

	return deliveries, nil
}

func (s *logStoreImpl) InsertManyEvent(ctx context.Context, events []*models.Event) error {
	if len(events) == 0 {
		return nil
	}

	batch, err := s.chDB.PrepareBatch(ctx,
		"INSERT INTO events (id, tenant_id, destination_id, topic, eligible_for_retry, time, metadata, data)",
	)
	if err != nil {
		return err
	}

	for _, event := range events {
		metadataJSON, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		dataJSON, err := json.Marshal(event.Data)
		if err != nil {
			return fmt.Errorf("failed to marshal data: %w", err)
		}

		if err := batch.Append(
			event.ID,
			event.TenantID,
			event.DestinationID,
			event.Topic,
			event.EligibleForRetry,
			event.Time,
			string(metadataJSON),
			string(dataJSON),
		); err != nil {
			return err
		}
	}

	if err := batch.Send(); err != nil {
		return err
	}

	// Decision: Force ClickHouse to merge data immediately for testing
	// NOT production-ready, but ensures data is immediately visible
	_ = s.chDB.Exec(ctx, "OPTIMIZE TABLE events FINAL")

	return nil
}

func (s *logStoreImpl) InsertManyDelivery(ctx context.Context, deliveries []*models.Delivery) error {
	if len(deliveries) == 0 {
		return nil
	}

	batch, err := s.chDB.PrepareBatch(ctx,
		"INSERT INTO deliveries (id, delivery_event_id, event_id, destination_id, status, time)",
	)
	if err != nil {
		return err
	}

	for _, delivery := range deliveries {
		if err := batch.Append(
			delivery.ID,
			delivery.DeliveryEventID,
			delivery.EventID,
			delivery.DestinationID,
			delivery.Status,
			delivery.Time,
		); err != nil {
			return err
		}
	}

	if err := batch.Send(); err != nil {
		return err
	}

	// Decision: Force ClickHouse to merge data immediately for testing
	// NOT production-ready, but ensures data is immediately visible
	_ = s.chDB.Exec(ctx, "OPTIMIZE TABLE deliveries FINAL")

	return nil
}

func (s *logStoreImpl) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	if len(deliveryEvents) == 0 {
		return nil
	}

	// Insert events
	events := make([]*models.Event, len(deliveryEvents))
	for i, de := range deliveryEvents {
		events[i] = &de.Event
	}
	if err := s.InsertManyEvent(ctx, events); err != nil {
		return fmt.Errorf("failed to insert events: %w", err)
	}

	// Insert deliveries
	deliveries := make([]*models.Delivery, 0, len(deliveryEvents))
	for _, de := range deliveryEvents {
		if de.Delivery != nil {
			deliveries = append(deliveries, de.Delivery)
		} else {
			// Create a pending delivery if none exists
			deliveries = append(deliveries, &models.Delivery{
				ID:              de.ID,
				DeliveryEventID: de.ID,
				EventID:         de.Event.ID,
				DestinationID:   de.DestinationID,
				Status:          "pending",
				Time:            time.Now(),
			})
		}
	}
	if err := s.InsertManyDelivery(ctx, deliveries); err != nil {
		return fmt.Errorf("failed to insert deliveries: %w", err)
	}

	return nil
}

func (s *logStoreImpl) RetrieveEventByDestination(ctx context.Context, tenantID, destinationID, eventID string) (*models.Event, error) {
	// Query event with destination-specific status
	// Decision: Get the latest delivery status for this specific destination
	query := `
		WITH latest_delivery AS (
			SELECT argMax(status, time) as status
			FROM deliveries
			WHERE event_id = ? AND destination_id = ?
		)
		SELECT
			e.id,
			e.tenant_id,
			? as destination_id,
			e.time,
			e.topic,
			e.eligible_for_retry,
			e.metadata,
			e.data,
			COALESCE(ld.status, 'pending') as status
		FROM events e
		LEFT JOIN latest_delivery ld ON true
		WHERE e.tenant_id = ? AND e.id = ?
	`

	row := s.chDB.QueryRow(ctx, query, eventID, destinationID, destinationID, tenantID, eventID)

	var metadataStr, dataStr string
	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.DestinationID,
		&event.Time,
		&event.Topic,
		&event.EligibleForRetry,
		&metadataStr,
		&dataStr,
		&event.Status,
	)
	if err != nil {
		// ClickHouse returns an error when no rows, not sql.ErrNoRows
		if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// Unmarshal JSON strings
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
