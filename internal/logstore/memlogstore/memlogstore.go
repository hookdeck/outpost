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
	cursorResourceEvent   = "evt"
	cursorResourceAttempt = "att"
	cursorVersion         = 1
)

// memLogStore is an in-memory implementation of driver.LogStore.
// It serves as a reference implementation and is useful for testing.
type memLogStore struct {
	mu       sync.RWMutex
	events   map[string]*models.Event // keyed by event ID
	attempts []*models.Attempt        // list of all attempts
}

var _ driver.LogStore = (*memLogStore)(nil)

func NewLogStore() driver.LogStore {
	return &memLogStore{
		events:   make(map[string]*models.Event),
		attempts: make([]*models.Attempt, 0),
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

		// Insert attempt (idempotent upsert: match on event_id + attempt_id)
		a := entry.Attempt
		copied := copyAttempt(a)

		found := false
		for i, existing := range s.attempts {
			if existing.EventID == a.EventID && existing.ID == a.ID {
				s.attempts[i] = copied
				found = true
				break
			}
		}

		if !found {
			s.attempts = append(s.attempts, copied)
		}
	}
	return nil
}

func (s *memLogStore) ListAttempt(ctx context.Context, req driver.ListAttemptRequest) (driver.ListAttemptResponse, error) {
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

	// Filter attempts and build records with events
	var allRecords []*driver.AttemptRecord
	for _, a := range s.attempts {
		event := s.events[a.EventID]
		if event == nil {
			continue // skip orphan attempts
		}
		if !s.matchesAttemptFilter(a, event, req) {
			continue
		}
		allRecords = append(allRecords, &driver.AttemptRecord{
			Attempt: copyAttempt(a),
			Event:   copyEvent(event),
		})
	}

	// attemptRecordWithTimeID pairs an attempt record with its sortable time ID.
	type attemptRecordWithTimeID struct {
		record *driver.AttemptRecord
		timeID string
	}

	// Build list with time IDs (using attempt time)
	recordsWithTimeID := make([]attemptRecordWithTimeID, len(allRecords))
	for i, r := range allRecords {
		recordsWithTimeID[i] = attemptRecordWithTimeID{
			record: r,
			timeID: makeTimeID(r.Attempt.Time, r.Attempt.ID),
		}
	}

	res, err := pagination.Run(ctx, pagination.Config[attemptRecordWithTimeID]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(_ context.Context, q pagination.QueryInput) ([]attemptRecordWithTimeID, error) {
			// Sort based on query direction
			isDesc := q.SortDir == "desc"
			sort.Slice(recordsWithTimeID, func(i, j int) bool {
				if isDesc {
					return recordsWithTimeID[i].timeID > recordsWithTimeID[j].timeID
				}
				return recordsWithTimeID[i].timeID < recordsWithTimeID[j].timeID
			})

			// Filter using q.Compare (like SQL WHERE clause)
			var filtered []attemptRecordWithTimeID
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

			result := make([]attemptRecordWithTimeID, len(filtered))
			for i, r := range filtered {
				result[i] = attemptRecordWithTimeID{
					record: &driver.AttemptRecord{
						Attempt: copyAttempt(r.record.Attempt),
						Event:   copyEvent(r.record.Event),
					},
					timeID: r.timeID,
				}
			}
			return result, nil
		},
		Cursor: pagination.Cursor[attemptRecordWithTimeID]{
			Encode: func(r attemptRecordWithTimeID) string {
				return cursor.Encode(cursorResourceAttempt, cursorVersion, r.timeID)
			},
			Decode: func(c string) (string, error) {
				return cursor.Decode(c, cursorResourceAttempt, cursorVersion)
			},
		},
	})
	if err != nil {
		return driver.ListAttemptResponse{}, err
	}

	// Extract records from results
	data := make([]*driver.AttemptRecord, len(res.Items))
	for i, item := range res.Items {
		data[i] = item.record
	}

	return driver.ListAttemptResponse{
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

func (s *memLogStore) RetrieveAttempt(ctx context.Context, req driver.RetrieveAttemptRequest) (*driver.AttemptRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, a := range s.attempts {
		if a.ID == req.AttemptID {
			event := s.events[a.EventID]
			if event == nil {
				continue
			}
			if req.TenantID != "" && event.TenantID != req.TenantID {
				continue
			}
			return &driver.AttemptRecord{
				Attempt: copyAttempt(a),
				Event:   copyEvent(event),
			}, nil
		}
	}
	return nil, nil
}

func (s *memLogStore) matchesAttemptFilter(a *models.Attempt, event *models.Event, req driver.ListAttemptRequest) bool {
	// Filter by event's tenant ID since attempts don't have tenant_id in the database
	if req.TenantID != "" && event.TenantID != req.TenantID {
		return false
	}

	if req.EventID != "" && a.EventID != req.EventID {
		return false
	}

	if len(req.DestinationIDs) > 0 {
		found := false
		for _, destID := range req.DestinationIDs {
			if a.DestinationID == destID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.Status != "" && a.Status != req.Status {
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

	if req.TimeFilter.GTE != nil && a.Time.Before(*req.TimeFilter.GTE) {
		return false
	}
	if req.TimeFilter.LTE != nil && a.Time.After(*req.TimeFilter.LTE) {
		return false
	}
	if req.TimeFilter.GT != nil && !a.Time.After(*req.TimeFilter.GT) {
		return false
	}
	if req.TimeFilter.LT != nil && !a.Time.Before(*req.TimeFilter.LT) {
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

func copyAttempt(a *models.Attempt) *models.Attempt {
	if a == nil {
		return nil
	}
	copied := &models.Attempt{
		ID:            a.ID,
		TenantID:      a.TenantID,
		EventID:       a.EventID,
		DestinationID: a.DestinationID,
		AttemptNumber: a.AttemptNumber,
		Manual:        a.Manual,
		Status:        a.Status,
		Time:          a.Time,
		Code:          a.Code,
	}

	if a.ResponseData != nil {
		copied.ResponseData = make(map[string]any, len(a.ResponseData))
		for k, v := range a.ResponseData {
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
