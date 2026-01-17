package pglogstore

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type logStore struct {
	db *pgxpool.Pool
}

func NewLogStore(db *pgxpool.Pool) driver.LogStore {
	return &logStore{
		db: db,
	}
}

// ListEvent returns events matching the filter criteria.
// It queries the events table directly, sorted by event_time.
func (s *logStore) ListEvent(ctx context.Context, req driver.ListEventRequest) (driver.ListEventResponse, error) {
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
	var orderByClause, finalOrderByClause string
	if sortOrder == "desc" {
		if goingBackward {
			orderByClause = "time ASC, id ASC"
		} else {
			orderByClause = "time DESC, id DESC"
		}
		finalOrderByClause = "time DESC, id DESC"
	} else {
		if goingBackward {
			orderByClause = "time DESC, id DESC"
		} else {
			orderByClause = "time ASC, id ASC"
		}
		finalOrderByClause = "time ASC, id ASC"
	}

	// Build cursor conditions using time_id column (generated column in events table)
	var cursorCondition string
	if sortOrder == "desc" {
		cursorCondition = "AND ($6::text = '' OR time_id < $6::text) AND ($7::text = '' OR time_id > $7::text)"
	} else {
		cursorCondition = "AND ($6::text = '' OR time_id > $6::text) AND ($7::text = '' OR time_id < $7::text)"
	}

	query := fmt.Sprintf(`
		WITH filtered AS (
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
			%s
			ORDER BY %s
			LIMIT $8
		)
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
		FROM filtered
		ORDER BY %s
	`, cursorCondition, orderByClause, finalOrderByClause)

	rows, err := s.db.Query(ctx, query,
		req.TenantID,        // $1
		req.DestinationIDs,  // $2
		req.Topics,          // $3
		req.EventStart,      // $4
		req.EventEnd,        // $5
		nextCursor.Position, // $6
		prevCursor.Position, // $7
		limit+1,             // $8 - fetch one extra to detect if there's more
	)
	if err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		event  *models.Event
		timeID string
	}
	var results []rowData

	for rows.Next() {
		var (
			id               string
			tenantID         string
			destinationID    string
			eventTime        time.Time
			topic            string
			eligibleForRetry bool
			data             map[string]interface{}
			metadata         map[string]string
			timeID           string
		)

		err := rows.Scan(
			&id,
			&tenantID,
			&destinationID,
			&eventTime,
			&topic,
			&eligibleForRetry,
			&data,
			&metadata,
			&timeID,
		)
		if err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("scan failed: %w", err)
		}

		event := &models.Event{
			ID:               id,
			TenantID:         tenantID,
			DestinationID:    destinationID,
			Topic:            topic,
			EligibleForRetry: eligibleForRetry,
			Time:             eventTime,
			Data:             data,
			Metadata:         metadata,
		}

		results = append(results, rowData{event: event, timeID: timeID})
	}

	if err := rows.Err(); err != nil {
		return driver.ListEventResponse{}, fmt.Errorf("rows error: %w", err)
	}

	// Handle pagination cursors
	var hasMore bool
	if len(results) > limit {
		hasMore = true
		if goingBackward {
			results = results[1:]
		} else {
			results = results[:limit]
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
			return r.timeID
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

// ListDeliveryEvent returns delivery events matching the filter criteria.
// It joins event_delivery_index with events and deliveries tables to return
// complete DeliveryEvent records.
//
// Sorting is by delivery_time with delivery_id as tiebreaker for deterministic pagination.
func (s *logStore) ListDeliveryEvent(ctx context.Context, req driver.ListDeliveryEventRequest) (driver.ListDeliveryEventResponse, error) {
	// Always sort by delivery_time
	sortBy := "delivery_time"
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

	// Cursor column for delivery_time sort
	cursorCol := "time_delivery_id"

	// Determine if we're going backward (using prev cursor)
	goingBackward := !prevCursor.IsEmpty()

	// Build ORDER BY clause: ORDER BY delivery_time, delivery_id
	var orderByClause, finalOrderByClause string
	if sortOrder == "desc" {
		if goingBackward {
			orderByClause = "delivery_time ASC, delivery_id ASC"
		} else {
			orderByClause = "delivery_time DESC, delivery_id DESC"
		}
		finalOrderByClause = "delivery_time DESC, delivery_id DESC"
	} else {
		if goingBackward {
			orderByClause = "delivery_time DESC, delivery_id DESC"
		} else {
			orderByClause = "delivery_time ASC, delivery_id ASC"
		}
		finalOrderByClause = "delivery_time ASC, delivery_id ASC"
	}

	// Build cursor conditions - always include both conditions but use empty string check
	// This ensures PostgreSQL can infer types for both parameters
	// For next cursor (going forward in display order): get items that come AFTER in sort order
	// For prev cursor (going backward in display order): get items that come BEFORE in sort order
	// Since we flip the query order for prev, we use the same comparison direction
	var cursorCondition string
	if sortOrder == "desc" {
		// DESC: next means smaller values, prev means larger values (but we query with flipped order)
		cursorCondition = fmt.Sprintf("AND ($10::text = '' OR %s < $10::text) AND ($11::text = '' OR %s > $11::text)", cursorCol, cursorCol)
	} else {
		// ASC: next means larger values, prev means smaller values (but we query with flipped order)
		cursorCondition = fmt.Sprintf("AND ($10::text = '' OR %s > $10::text) AND ($11::text = '' OR %s < $11::text)", cursorCol, cursorCol)
	}

	query := fmt.Sprintf(`
		WITH filtered AS (
			SELECT
				idx.event_id,
				idx.delivery_id,
				idx.tenant_id,
				idx.destination_id,
				idx.event_time,
				idx.delivery_time,
				idx.topic,
				idx.status,
				idx.time_event_id,
				idx.time_delivery_id
			FROM event_delivery_index idx
			WHERE ($1::text = '' OR idx.tenant_id = $1)
			AND ($2::text = '' OR idx.event_id = $2)
			AND (array_length($3::text[], 1) IS NULL OR idx.destination_id = ANY($3))
			AND ($4::text = '' OR idx.status = $4)
			AND (array_length($5::text[], 1) IS NULL OR idx.topic = ANY($5))
			AND ($6::timestamptz IS NULL OR idx.event_time >= $6)
			AND ($7::timestamptz IS NULL OR idx.event_time <= $7)
			AND ($8::timestamptz IS NULL OR idx.delivery_time >= $8)
			AND ($9::timestamptz IS NULL OR idx.delivery_time <= $9)
			%s
			ORDER BY %s
			LIMIT $12
		)
		SELECT
			f.event_id,
			f.delivery_id,
			f.destination_id,
			f.event_time,
			f.delivery_time,
			f.topic,
			f.status,
			f.time_event_id,
			f.time_delivery_id,
			e.tenant_id,
			e.eligible_for_retry,
			e.data,
			e.metadata,
			d.code,
			d.response_data
		FROM filtered f
		JOIN events e ON e.id = f.event_id AND e.time = f.event_time
		JOIN deliveries d ON d.id = f.delivery_id AND d.time = f.delivery_time
		ORDER BY %s
	`, cursorCondition, orderByClause, finalOrderByClause)

	rows, err := s.db.Query(ctx, query,
		req.TenantID,        // $1
		req.EventID,         // $2
		req.DestinationIDs,  // $3
		req.Status,          // $4
		req.Topics,          // $5
		req.EventStart,      // $6
		req.EventEnd,        // $7
		req.DeliveryStart,   // $8
		req.DeliveryEnd,     // $9
		nextCursor.Position, // $10
		prevCursor.Position, // $11
		limit+1,             // $12 - fetch one extra to detect if there's more
	)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		de             *models.DeliveryEvent
		timeEventID    string
		timeDeliveryID string
	}
	var results []rowData

	for rows.Next() {
		var (
			eventID          string
			deliveryID       string
			destinationID    string
			eventTime        time.Time
			deliveryTime     time.Time
			topic            string
			status           string
			timeEventID      string
			timeDeliveryID   string
			tenantID         string
			eligibleForRetry bool
			data             map[string]interface{}
			metadata         map[string]string
			code             string
			responseData     map[string]interface{}
		)

		err := rows.Scan(
			&eventID,
			&deliveryID,
			&destinationID,
			&eventTime,
			&deliveryTime,
			&topic,
			&status,
			&timeEventID,
			&timeDeliveryID,
			&tenantID,
			&eligibleForRetry,
			&data,
			&metadata,
			&code,
			&responseData,
		)
		if err != nil {
			return driver.ListDeliveryEventResponse{}, fmt.Errorf("scan failed: %w", err)
		}

		de := &models.DeliveryEvent{
			ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
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

		results = append(results, rowData{de: de, timeEventID: timeEventID, timeDeliveryID: timeDeliveryID})
	}

	if err := rows.Err(); err != nil {
		return driver.ListDeliveryEventResponse{}, fmt.Errorf("rows error: %w", err)
	}

	// Handle pagination cursors
	// When going backward, the extra item (if any) is at the BEGINNING after re-sort
	// When going forward, the extra item is at the END
	var hasMore bool
	if len(results) > limit {
		hasMore = true
		if goingBackward {
			// Trim from beginning - the extra item is now first after DESC re-sort
			results = results[1:]
		} else {
			// Trim from end - the extra item is last
			results = results[:limit]
		}
	}

	// Build response
	data := make([]*models.DeliveryEvent, len(results))
	for i, r := range results {
		data[i] = r.de
	}

	var nextEncoded, prevEncoded string
	if len(results) > 0 {
		// Position value is timeDeliveryID (matches cursorCol)
		getPosition := func(r rowData) string {
			return r.timeDeliveryID
		}

		encodeCursor := func(position string) string {
			return cursor.Encode(cursor.Cursor{
				SortBy:    sortBy,
				SortOrder: sortOrder,
				Position:  position,
			})
		}

		if !prevCursor.IsEmpty() {
			// Came from prev, so there's definitely more "next"
			nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			if hasMore {
				prevEncoded = encodeCursor(getPosition(results[0]))
			}
		} else if !nextCursor.IsEmpty() {
			// Came from next, so there's definitely more "prev"
			prevEncoded = encodeCursor(getPosition(results[0]))
			if hasMore {
				nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			}
		} else {
			// First page
			if hasMore {
				nextEncoded = encodeCursor(getPosition(results[len(results)-1]))
			}
			// No prev on first page
		}
	}

	return driver.ListDeliveryEventResponse{
		Data: data,
		Next: nextEncoded,
		Prev: prevEncoded,
	}, nil
}

// RetrieveEvent retrieves a single event by ID.
// If DestinationID is provided, it scopes the query to that destination.
func (s *logStore) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	var query string
	var args []interface{}

	if req.DestinationID != "" {
		// Scope to specific destination - get status from index
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
		args = []interface{}{req.TenantID, req.EventID, req.DestinationID}
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
		args = []interface{}{req.TenantID, req.EventID}
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

// RetrieveDeliveryEvent retrieves a single delivery event by delivery ID.
func (s *logStore) RetrieveDeliveryEvent(ctx context.Context, req driver.RetrieveDeliveryEventRequest) (*models.DeliveryEvent, error) {
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
			d.response_data
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
		data             map[string]interface{}
		metadata         map[string]string
		code             string
		responseData     map[string]interface{}
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
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return &models.DeliveryEvent{
		ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
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

func (s *logStore) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	if len(deliveryEvents) == 0 {
		return nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Insert events
	events := make([]*models.Event, len(deliveryEvents))
	for i, de := range deliveryEvents {
		events[i] = &de.Event
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[], $5::text[], $6::boolean[], $7::jsonb[], $8::jsonb[])
		ON CONFLICT (time, id) DO NOTHING
	`, eventArrays(events)...)
	if err != nil {
		return err
	}

	// Insert deliveries
	deliveries := make([]*models.Delivery, len(deliveryEvents))
	for i, de := range deliveryEvents {
		if de.Delivery == nil {
			// Create a pending delivery if none exists
			deliveries[i] = &models.Delivery{
				ID:            de.ID,
				EventID:       de.Event.ID,
				DestinationID: de.DestinationID,
				Status:        "pending",
				Time:          time.Now(),
			}
		} else {
			deliveries[i] = de.Delivery
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO deliveries (id, event_id, destination_id, status, time, code, response_data)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::text[], $7::jsonb[])
		ON CONFLICT (time, id) DO UPDATE SET
			status = EXCLUDED.status,
			code = EXCLUDED.code,
			response_data = EXCLUDED.response_data
	`, deliveryArrays(deliveries)...)
	if err != nil {
		return err
	}

	// Insert into index
	_, err = tx.Exec(ctx, `
		INSERT INTO event_delivery_index (
			event_id, delivery_id, tenant_id, destination_id,
			event_time, delivery_time, topic, status
		)
		SELECT * FROM unnest(
			$1::text[], $2::text[], $3::text[], $4::text[],
			$5::timestamptz[], $6::timestamptz[], $7::text[], $8::text[]
		)
		ON CONFLICT (delivery_time, event_id, delivery_id) DO UPDATE SET
			status = EXCLUDED.status
	`, eventDeliveryIndexArrays(deliveryEvents)...)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func eventDeliveryIndexArrays(deliveryEvents []*models.DeliveryEvent) []interface{} {
	eventIDs := make([]string, len(deliveryEvents))
	deliveryIDs := make([]string, len(deliveryEvents))
	tenantIDs := make([]string, len(deliveryEvents))
	destinationIDs := make([]string, len(deliveryEvents))
	eventTimes := make([]time.Time, len(deliveryEvents))
	deliveryTimes := make([]time.Time, len(deliveryEvents))
	topics := make([]string, len(deliveryEvents))
	statuses := make([]string, len(deliveryEvents))

	for i, de := range deliveryEvents {
		eventIDs[i] = de.Event.ID
		if de.Delivery != nil {
			deliveryIDs[i] = de.Delivery.ID
		} else {
			deliveryIDs[i] = de.ID
		}
		tenantIDs[i] = de.Event.TenantID
		destinationIDs[i] = de.DestinationID
		eventTimes[i] = de.Event.Time
		if de.Delivery != nil {
			deliveryTimes[i] = de.Delivery.Time
			statuses[i] = de.Delivery.Status
		} else {
			deliveryTimes[i] = time.Now()
			statuses[i] = "pending"
		}
		topics[i] = de.Event.Topic
	}

	return []interface{}{
		eventIDs,
		deliveryIDs,
		tenantIDs,
		destinationIDs,
		eventTimes,
		deliveryTimes,
		topics,
		statuses,
	}
}

func eventArrays(events []*models.Event) []interface{} {
	ids := make([]string, len(events))
	tenantIDs := make([]string, len(events))
	destinationIDs := make([]string, len(events))
	times := make([]time.Time, len(events))
	topics := make([]string, len(events))
	eligibleForRetries := make([]bool, len(events))
	datas := make([]map[string]interface{}, len(events))
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

	return []interface{}{
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

func deliveryArrays(deliveries []*models.Delivery) []interface{} {
	ids := make([]string, len(deliveries))
	eventIDs := make([]string, len(deliveries))
	destinationIDs := make([]string, len(deliveries))
	statuses := make([]string, len(deliveries))
	times := make([]time.Time, len(deliveries))
	codes := make([]string, len(deliveries))
	responseDatas := make([]map[string]interface{}, len(deliveries))

	for i, d := range deliveries {
		ids[i] = d.ID
		eventIDs[i] = d.EventID
		destinationIDs[i] = d.DestinationID
		statuses[i] = d.Status
		times[i] = d.Time
		codes[i] = d.Code
		responseDatas[i] = d.ResponseData
	}

	return []interface{}{
		ids,
		eventIDs,
		destinationIDs,
		statuses,
		times,
		codes,
		responseDatas,
	}
}
