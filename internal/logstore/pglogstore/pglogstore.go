package pglogstore

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type logStore struct {
	db           *pgxpool.Pool
	cursorParser eventCursorParser
}

func NewLogStore(db *pgxpool.Pool) driver.LogStore {
	return &logStore{
		db:           db,
		cursorParser: newEventCursorParser(),
	}
}

func (s *logStore) ListEvent(ctx context.Context, req driver.ListEventRequest) (driver.ListEventResponse, error) {
	// TODO: validate only one of next or prev is set

	var decodedNext, decodedPrev string
	if req.Next != "" {
		var err error
		decodedNext, err = s.cursorParser.Parse(req.Next)
		if err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("invalid cursor: %v", err)
		}
	}
	if req.Prev != "" {
		var err error
		decodedPrev, err = s.cursorParser.Parse(req.Prev)
		if err != nil {
			return driver.ListEventResponse{}, fmt.Errorf("invalid cursor: %v", err)
		}
	}

	// Step 1: Query the index to get relevant event IDs and their status
	indexQuery := `
		-- Step 1: Apply some filters & dedup index table to get event with status
		WITH latest_status AS (
			SELECT DISTINCT ON (event_id, destination_id) 
				event_id,
				destination_id,
				delivery_time,
				event_time,
				time_event_id,
				status
			FROM event_delivery_index
			WHERE tenant_id = $6  -- tenant_id
			AND event_time >= COALESCE($4, COALESCE($5, NOW()) - INTERVAL '1 hour')
			AND event_time <= COALESCE($5, NOW())
			AND (array_length($7::text[], 1) IS NULL OR destination_id = ANY($7))  -- destination_ids
			AND (array_length($8::text[], 1) IS NULL OR topic = ANY($8))  -- topics
			ORDER BY event_id, destination_id, delivery_time DESC
		),
		-- Step 2: Apply status filter
		filtered_before_cursor AS (
			SELECT *
			FROM latest_status
			WHERE ($9 = '' OR status = $9)  -- status filter
		),
		-- Step 3: Apply pagination (cursor & limit)
		filtered AS (
			SELECT 
				event_id,
				destination_id,
				delivery_time,
				event_time,
				time_event_id,
				status,
				(SELECT COUNT(*) FROM filtered_before_cursor) as total_count
			FROM filtered_before_cursor
			WHERE ($1 = '' OR time_event_id < $1) AND ($2 = '' OR time_event_id > $2)  -- cursor pagination
			ORDER BY 
				CASE WHEN $2 != '' THEN time_event_id END ASC, -- prev cursor: sort ascending to get right window
				time_event_id DESC -- default sort and next cursor sort
			LIMIT CASE WHEN $3 = 0 THEN NULL ELSE $3 + 1 END
		),
		-- Step 4: Re-sort for consistent response
		final AS (
			SELECT *
			FROM filtered
			ORDER BY time_event_id DESC
		)
		SELECT * FROM final`

	indexRows, err := s.db.Query(ctx, indexQuery,
		decodedNext,
		decodedPrev,
		req.Limit,
		req.Start,
		req.End,
		req.TenantID,
		req.DestinationIDs,
		req.Topics,
		req.Status,
	)
	if err != nil {
		return driver.ListEventResponse{}, err
	}
	defer indexRows.Close()

	// Collect event IDs and their status
	type eventInfo struct {
		eventID       string
		destinationID string
		deliveryTime  time.Time
		eventTime     time.Time
		timeEventID   string
		status        string
	}
	eventInfos := []eventInfo{}
	var totalCount int64
	for indexRows.Next() {
		var info eventInfo
		err := indexRows.Scan(&info.eventID, &info.destinationID, &info.deliveryTime, &info.eventTime, &info.timeEventID, &info.status, &totalCount)
		if err != nil {
			return driver.ListEventResponse{}, err
		}
		eventInfos = append(eventInfos, info)
	}

	if len(eventInfos) == 0 {
		return driver.ListEventResponse{
			Data:  []*models.Event{},
			Next:  "",
			Prev:  "",
			Count: 0,
		}, nil
	}

	// Handle pagination
	var hasNext, hasPrev bool
	if req.Prev != "" {
		hasNext = true                                          // We came backwards, so definitely more ahead
		hasPrev = len(eventInfos) > req.Limit || req.Limit == 0 // Check if more behind
		if len(eventInfos) > req.Limit && req.Limit > 0 {
			eventInfos = eventInfos[1:] // Trim first item (newest) when going backward
		}
	} else if req.Next != "" {
		hasPrev = true                                          // We came forwards, so definitely more behind
		hasNext = len(eventInfos) > req.Limit || req.Limit == 0 // Check if more ahead
		if len(eventInfos) > req.Limit && req.Limit > 0 {
			eventInfos = eventInfos[:len(eventInfos)-1] // Trim last item when going forward
		}
	} else {
		// First page
		hasPrev = false
		hasNext = len(eventInfos) > req.Limit || req.Limit == 0
		if len(eventInfos) > req.Limit && req.Limit > 0 {
			eventInfos = eventInfos[:len(eventInfos)-1] // Trim last item on first page
		}
	}

	// Step 2: Get full event data
	eventIDs := make([]string, len(eventInfos))
	for i, info := range eventInfos {
		eventIDs[i] = info.eventID
	}

	eventQuery := `
		SELECT
			id,
			tenant_id,
			time,
			topic,
			eligible_for_retry,
			data,
			metadata
		FROM events e
		WHERE id = ANY($1)`

	eventRows, err := s.db.Query(ctx, eventQuery, eventIDs)
	if err != nil {
		return driver.ListEventResponse{}, err
	}
	defer eventRows.Close()

	// Build map of events
	eventMap := make(map[string]*models.Event)
	for eventRows.Next() {
		event := &models.Event{}
		err := eventRows.Scan(
			&event.ID,
			&event.TenantID,
			&event.Time,
			&event.Topic,
			&event.EligibleForRetry,
			&event.Data,
			&event.Metadata,
		)
		if err != nil {
			return driver.ListEventResponse{}, err
		}
		eventMap[event.ID] = event
	}

	// Combine events with their status in correct order
	events := make([]*models.Event, 0, len(eventInfos))
	for _, info := range eventInfos {
		if baseEvent, ok := eventMap[info.eventID]; ok {
			// Create new event for each destination
			event := *baseEvent // Make copy
			event.DestinationID = info.destinationID
			event.Status = info.status
			events = append(events, &event)
		}
	}

	var nextCursor, prevCursor string
	if len(events) > 0 {
		lastItem := eventInfos[len(eventInfos)-1].timeEventID
		firstItem := eventInfos[0].timeEventID

		if hasNext {
			nextCursor = s.cursorParser.Format(lastItem)
		}
		if hasPrev {
			prevCursor = s.cursorParser.Format(firstItem)
		}
	}

	return driver.ListEventResponse{
		Data:  events,
		Next:  nextCursor,
		Prev:  prevCursor,
		Count: totalCount,
	}, nil
}

func (s *logStore) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	query := `
		SELECT
			id,
			tenant_id,
			time,
			topic,
			eligible_for_retry,
			data,
			metadata,
			CASE
				WHEN EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id AND d.status = 'success') THEN 'success'
				WHEN EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = e.id) THEN 'failed'
				ELSE 'pending'
			END as status
		FROM events e
		WHERE tenant_id = $1 AND id = $2`

	row := s.db.QueryRow(ctx, query, tenantID, eventID)

	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.Time,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Data,
		&event.Metadata,
		&event.Status,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return event, nil
}

func (s *logStore) RetrieveEventByDestination(ctx context.Context, tenantID, destinationID, eventID string) (*models.Event, error) {
	query := `
		WITH latest_status AS (
			SELECT DISTINCT ON (event_id, destination_id) status
			FROM event_delivery_index
			WHERE tenant_id = $1 AND destination_id = $2 AND event_id = $3
			ORDER BY event_id, destination_id, delivery_time DESC
		)
		SELECT
			e.id,
			e.tenant_id,
			e.time,
			e.topic,
			e.eligible_for_retry,
			e.data,
			e.metadata,
			$2 as destination_id,
			COALESCE(s.status, 'pending') as status
		FROM events e
		LEFT JOIN latest_status s ON true
		WHERE e.tenant_id = $1 AND e.id = $3`

	row := s.db.QueryRow(ctx, query, tenantID, destinationID, eventID)

	event := &models.Event{}
	err := row.Scan(
		&event.ID,
		&event.TenantID,
		&event.Time,
		&event.Topic,
		&event.EligibleForRetry,
		&event.Data,
		&event.Metadata,
		&event.DestinationID,
		&event.Status,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return event, nil
}

func (s *logStore) ListDelivery(ctx context.Context, req driver.ListDeliveryRequest) ([]*models.Delivery, error) {
	query := `
		SELECT id, event_id, destination_id, status, time, code, response_data
		FROM deliveries
		WHERE event_id = $1
		AND ($2 = '' OR destination_id = $2)
		ORDER BY time DESC`

	rows, err := s.db.Query(ctx, query,
		req.EventID,
		req.DestinationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []*models.Delivery
	for rows.Next() {
		delivery := &models.Delivery{}
		err := rows.Scan(
			&delivery.ID,
			&delivery.EventID,
			&delivery.DestinationID,
			&delivery.Status,
			&delivery.Time,
			&delivery.Code,
			&delivery.ResponseData,
		)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}

	return deliveries, nil
}

func (s *logStore) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
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
		deliveryIDs[i] = de.ID
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
