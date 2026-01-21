package memlogstore

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

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

// memLogStore is an in-memory implementation of driver.LogStore.
// It serves as a reference implementation and is useful for testing.
type memLogStore struct {
	mu         sync.RWMutex
	events     map[string]*models.Event // keyed by event ID
	deliveries []*models.Delivery       // list of all deliveries
}

var _ driver.LogStore = (*memLogStore)(nil)

func NewLogStore() driver.LogStore {
	return &memLogStore{
		events:     make(map[string]*models.Event),
		deliveries: make([]*models.Delivery, 0),
	}
}

func (s *memLogStore) ListEvent(ctx context.Context, req driver.ListEventRequest) (driver.ListEventResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Filter events
	var allEvents []*models.Event
	for _, event := range s.events {
		if !s.matchesEventFilter(event, req) {
			continue
		}
		allEvents = append(allEvents, copyEvent(event))
	}

	// eventWithTimeID pairs an event with its sortable time ID for cursor operations.
	type eventWithTimeID struct {
		event  *models.Event
		timeID string
	}

	// Build list with time IDs
	eventsWithTimeID := make([]eventWithTimeID, len(allEvents))
	for i, e := range allEvents {
		eventsWithTimeID[i] = eventWithTimeID{
			event:  e,
			timeID: makeTimeID(e.Time, e.ID),
		}
	}

	res, err := pagination.Run(ctx, pagination.Config[eventWithTimeID]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(_ context.Context, q pagination.QueryInput) ([]eventWithTimeID, error) {
			// Sort based on query direction
			isDesc := q.SortDir == "desc"
			sort.Slice(eventsWithTimeID, func(i, j int) bool {
				if isDesc {
					return eventsWithTimeID[i].timeID > eventsWithTimeID[j].timeID
				}
				return eventsWithTimeID[i].timeID < eventsWithTimeID[j].timeID
			})

			// Filter using q.Compare (like SQL WHERE clause)
			var filtered []eventWithTimeID
			for _, e := range eventsWithTimeID {
				// If no cursor, include all items
				// If cursor exists, filter using Compare operator
				if q.CursorPos == "" || compareTimeID(e.timeID, q.Compare, q.CursorPos) {
					filtered = append(filtered, e)
				}
			}

			// Return up to limit items
			if len(filtered) > q.Limit {
				filtered = filtered[:q.Limit]
			}

			result := make([]eventWithTimeID, len(filtered))
			for i, e := range filtered {
				result[i] = eventWithTimeID{
					event:  copyEvent(e.event),
					timeID: e.timeID,
				}
			}
			return result, nil
		},
		Cursor: pagination.Cursor[eventWithTimeID]{
			Encode: func(e eventWithTimeID) string {
				return cursor.Encode(cursorResourceEvent, cursorVersion, e.timeID)
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
		data[i] = item.event
	}

	return driver.ListEventResponse{
		Data: data,
		Next: res.Next,
		Prev: res.Prev,
	}, nil
}

func (s *memLogStore) matchesEventFilter(event *models.Event, req driver.ListEventRequest) bool {
	if req.TenantID != "" && event.TenantID != req.TenantID {
		return false
	}

	if len(req.DestinationIDs) > 0 {
		found := false
		for _, destID := range req.DestinationIDs {
			if event.DestinationID == destID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(req.Topics) > 0 {
		found := false
		for _, topic := range req.Topics {
			if event.Topic == topic {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.TimeFilter.GTE != nil && event.Time.Before(*req.TimeFilter.GTE) {
		return false
	}
	if req.TimeFilter.LTE != nil && event.Time.After(*req.TimeFilter.LTE) {
		return false
	}
	if req.TimeFilter.GT != nil && !event.Time.After(*req.TimeFilter.GT) {
		return false
	}
	if req.TimeFilter.LT != nil && !event.Time.Before(*req.TimeFilter.LT) {
		return false
	}

	return true
}

func (s *memLogStore) InsertMany(ctx context.Context, entries []*models.LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range entries {
		// Insert event (dedupe by ID)
		s.events[entry.Event.ID] = copyEvent(entry.Event)

		// Insert delivery (idempotent upsert: match on event_id + delivery_id)
		d := entry.Delivery
		copied := copyDelivery(d)

		found := false
		for i, existing := range s.deliveries {
			if existing.EventID == d.EventID && existing.ID == d.ID {
				s.deliveries[i] = copied
				found = true
				break
			}
		}

		if !found {
			s.deliveries = append(s.deliveries, copied)
		}
	}
	return nil
}

func (s *memLogStore) ListDelivery(ctx context.Context, req driver.ListDeliveryRequest) (driver.ListDeliveryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Filter deliveries and build records with events
	var allRecords []*driver.DeliveryRecord
	for _, d := range s.deliveries {
		event := s.events[d.EventID]
		if event == nil {
			continue // skip orphan deliveries
		}
		if !s.matchesDeliveryFilter(d, event, req) {
			continue
		}
		allRecords = append(allRecords, &driver.DeliveryRecord{
			Delivery: copyDelivery(d),
			Event:    copyEvent(event),
		})
	}

	// deliveryRecordWithTimeID pairs a delivery record with its sortable time ID.
	type deliveryRecordWithTimeID struct {
		record *driver.DeliveryRecord
		timeID string
	}

	// Build list with time IDs (using delivery time)
	recordsWithTimeID := make([]deliveryRecordWithTimeID, len(allRecords))
	for i, r := range allRecords {
		recordsWithTimeID[i] = deliveryRecordWithTimeID{
			record: r,
			timeID: makeTimeID(r.Delivery.Time, r.Delivery.ID),
		}
	}

	res, err := pagination.Run(ctx, pagination.Config[deliveryRecordWithTimeID]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(_ context.Context, q pagination.QueryInput) ([]deliveryRecordWithTimeID, error) {
			// Sort based on query direction
			isDesc := q.SortDir == "desc"
			sort.Slice(recordsWithTimeID, func(i, j int) bool {
				if isDesc {
					return recordsWithTimeID[i].timeID > recordsWithTimeID[j].timeID
				}
				return recordsWithTimeID[i].timeID < recordsWithTimeID[j].timeID
			})

			// Filter using q.Compare (like SQL WHERE clause)
			var filtered []deliveryRecordWithTimeID
			for _, r := range recordsWithTimeID {
				// If no cursor, include all items
				// If cursor exists, filter using Compare operator
				if q.CursorPos == "" || compareTimeID(r.timeID, q.Compare, q.CursorPos) {
					filtered = append(filtered, r)
				}
			}

			// Return up to limit items
			if len(filtered) > q.Limit {
				filtered = filtered[:q.Limit]
			}

			result := make([]deliveryRecordWithTimeID, len(filtered))
			for i, r := range filtered {
				result[i] = deliveryRecordWithTimeID{
					record: &driver.DeliveryRecord{
						Delivery: copyDelivery(r.record.Delivery),
						Event:    copyEvent(r.record.Event),
					},
					timeID: r.timeID,
				}
			}
			return result, nil
		},
		Cursor: pagination.Cursor[deliveryRecordWithTimeID]{
			Encode: func(r deliveryRecordWithTimeID) string {
				return cursor.Encode(cursorResourceDelivery, cursorVersion, r.timeID)
			},
			Decode: func(c string) (string, error) {
				return cursor.Decode(c, cursorResourceDelivery, cursorVersion)
			},
		},
	})
	if err != nil {
		return driver.ListDeliveryResponse{}, err
	}

	// Extract records from results
	data := make([]*driver.DeliveryRecord, len(res.Items))
	for i, item := range res.Items {
		data[i] = item.record
	}

	return driver.ListDeliveryResponse{
		Data: data,
		Next: res.Next,
		Prev: res.Prev,
	}, nil
}

func (s *memLogStore) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	event := s.events[req.EventID]
	if event == nil {
		return nil, nil
	}

	if req.TenantID != "" && event.TenantID != req.TenantID {
		return nil, nil
	}
	if req.DestinationID != "" && event.DestinationID != req.DestinationID {
		return nil, nil
	}
	return copyEvent(event), nil
}

func (s *memLogStore) RetrieveDelivery(ctx context.Context, req driver.RetrieveDeliveryRequest) (*driver.DeliveryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, d := range s.deliveries {
		if d.ID == req.DeliveryID {
			event := s.events[d.EventID]
			if event == nil {
				continue
			}
			if req.TenantID != "" && event.TenantID != req.TenantID {
				continue
			}
			return &driver.DeliveryRecord{
				Delivery: copyDelivery(d),
				Event:    copyEvent(event),
			}, nil
		}
	}
	return nil, nil
}

func (s *memLogStore) matchesDeliveryFilter(d *models.Delivery, event *models.Event, req driver.ListDeliveryRequest) bool {
	// Filter by event's tenant ID since deliveries don't have tenant_id in the database
	if req.TenantID != "" && event.TenantID != req.TenantID {
		return false
	}

	if req.EventID != "" && d.EventID != req.EventID {
		return false
	}

	if len(req.DestinationIDs) > 0 {
		found := false
		for _, destID := range req.DestinationIDs {
			if d.DestinationID == destID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.Status != "" && d.Status != req.Status {
		return false
	}

	if len(req.Topics) > 0 {
		found := false
		for _, topic := range req.Topics {
			if event.Topic == topic {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.TimeFilter.GTE != nil && d.Time.Before(*req.TimeFilter.GTE) {
		return false
	}
	if req.TimeFilter.LTE != nil && d.Time.After(*req.TimeFilter.LTE) {
		return false
	}
	if req.TimeFilter.GT != nil && !d.Time.After(*req.TimeFilter.GT) {
		return false
	}
	if req.TimeFilter.LT != nil && !d.Time.Before(*req.TimeFilter.LT) {
		return false
	}

	return true
}

func copyEvent(e *models.Event) *models.Event {
	copied := &models.Event{
		ID:               e.ID,
		TenantID:         e.TenantID,
		DestinationID:    e.DestinationID,
		Topic:            e.Topic,
		EligibleForRetry: e.EligibleForRetry,
		Time:             e.Time,
	}

	if e.Metadata != nil {
		copied.Metadata = make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			copied.Metadata[k] = v
		}
	}
	if e.Data != nil {
		copied.Data = make(map[string]any, len(e.Data))
		for k, v := range e.Data {
			copied.Data[k] = v
		}
	}

	return copied
}

func copyDelivery(d *models.Delivery) *models.Delivery {
	if d == nil {
		return nil
	}
	copied := &models.Delivery{
		ID:            d.ID,
		TenantID:      d.TenantID,
		EventID:       d.EventID,
		DestinationID: d.DestinationID,
		Attempt:       d.Attempt,
		Manual:        d.Manual,
		Status:        d.Status,
		Time:          d.Time,
		Code:          d.Code,
	}

	if d.ResponseData != nil {
		copied.ResponseData = make(map[string]any, len(d.ResponseData))
		for k, v := range d.ResponseData {
			copied.ResponseData[k] = v
		}
	}

	return copied
}

// makeTimeID creates a sortable string from time and ID, similar to pglogstore's time_id.
// Uses fixed-width nanoseconds to ensure correct string sorting (RFC3339Nano has variable width).
// Format: "2006-01-02T15:04:05.000000000Z_id"
func makeTimeID(t time.Time, id string) string {
	return fmt.Sprintf("%s_%s", t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"), id)
}

// compareTimeID compares two time IDs using the given operator.
func compareTimeID(a, op, b string) bool {
	switch op {
	case "<":
		return a < b
	case ">":
		return a > b
	default:
		return false
	}
}
