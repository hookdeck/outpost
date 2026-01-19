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
	mu             sync.RWMutex
	deliveryEvents []*models.DeliveryEvent
}

var _ driver.LogStore = (*memLogStore)(nil)

func NewLogStore() driver.LogStore {
	return &memLogStore{
		deliveryEvents: make([]*models.DeliveryEvent, 0),
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

	// Dedupe by event ID and filter
	eventMap := make(map[string]*models.Event)
	for _, de := range s.deliveryEvents {
		if !s.matchesEventFilter(&de.Event, req) {
			continue
		}
		if _, exists := eventMap[de.Event.ID]; !exists {
			eventMap[de.Event.ID] = copyEvent(&de.Event)
		}
	}

	var allEvents []*models.Event
	for _, event := range eventMap {
		allEvents = append(allEvents, event)
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

func (s *memLogStore) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, de := range deliveryEvents {
		copied := &models.DeliveryEvent{
			ID:            de.ID,
			Attempt:       de.Attempt,
			DestinationID: de.DestinationID,
			Event:         de.Event,
			Delivery:      de.Delivery,
			Manual:        de.Manual,
		}

		// Idempotent upsert: match on event_id + delivery_id
		found := false
		for i, existing := range s.deliveryEvents {
			if existing.Event.ID == de.Event.ID && existing.Delivery != nil && de.Delivery != nil && existing.Delivery.ID == de.Delivery.ID {
				s.deliveryEvents[i] = copied
				found = true
				break
			}
		}

		if !found {
			s.deliveryEvents = append(s.deliveryEvents, copied)
		}
	}
	return nil
}

func (s *memLogStore) ListDeliveryEvent(ctx context.Context, req driver.ListDeliveryEventRequest) (driver.ListDeliveryEventResponse, error) {
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

	// Filter delivery events
	var allDeliveryEvents []*models.DeliveryEvent
	for _, de := range s.deliveryEvents {
		if !s.matchesFilter(de, req) {
			continue
		}
		allDeliveryEvents = append(allDeliveryEvents, de)
	}

	// deliveryEventWithTimeID pairs a delivery event with its sortable time ID.
	type deliveryEventWithTimeID struct {
		de     *models.DeliveryEvent
		timeID string
	}

	// Build list with time IDs (using delivery time)
	deliveryEventsWithTimeID := make([]deliveryEventWithTimeID, len(allDeliveryEvents))
	for i, de := range allDeliveryEvents {
		deliveryEventsWithTimeID[i] = deliveryEventWithTimeID{
			de:     de,
			timeID: makeTimeID(de.Delivery.Time, de.Delivery.ID),
		}
	}

	res, err := pagination.Run(ctx, pagination.Config[deliveryEventWithTimeID]{
		Limit: limit,
		Order: sortOrder,
		Next:  req.Next,
		Prev:  req.Prev,
		Fetch: func(_ context.Context, q pagination.QueryInput) ([]deliveryEventWithTimeID, error) {
			// Sort based on query direction
			isDesc := q.SortDir == "desc"
			sort.Slice(deliveryEventsWithTimeID, func(i, j int) bool {
				if isDesc {
					return deliveryEventsWithTimeID[i].timeID > deliveryEventsWithTimeID[j].timeID
				}
				return deliveryEventsWithTimeID[i].timeID < deliveryEventsWithTimeID[j].timeID
			})

			// Filter using q.Compare (like SQL WHERE clause)
			var filtered []deliveryEventWithTimeID
			for _, de := range deliveryEventsWithTimeID {
				// If no cursor, include all items
				// If cursor exists, filter using Compare operator
				if q.CursorPos == "" || compareTimeID(de.timeID, q.Compare, q.CursorPos) {
					filtered = append(filtered, de)
				}
			}

			// Return up to limit items
			if len(filtered) > q.Limit {
				filtered = filtered[:q.Limit]
			}

			result := make([]deliveryEventWithTimeID, len(filtered))
			for i, de := range filtered {
				result[i] = deliveryEventWithTimeID{
					de:     copyDeliveryEvent(de.de),
					timeID: de.timeID,
				}
			}
			return result, nil
		},
		Cursor: pagination.Cursor[deliveryEventWithTimeID]{
			Encode: func(de deliveryEventWithTimeID) string {
				return cursor.Encode(cursorResourceDelivery, cursorVersion, de.timeID)
			},
			Decode: func(c string) (string, error) {
				return cursor.Decode(c, cursorResourceDelivery, cursorVersion)
			},
		},
	})
	if err != nil {
		return driver.ListDeliveryEventResponse{}, err
	}

	// Extract delivery events from results
	data := make([]*models.DeliveryEvent, len(res.Items))
	for i, item := range res.Items {
		data[i] = item.de
	}

	return driver.ListDeliveryEventResponse{
		Data: data,
		Next: res.Next,
		Prev: res.Prev,
	}, nil
}

func (s *memLogStore) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, de := range s.deliveryEvents {
		if de.Event.ID == req.EventID {
			if req.TenantID != "" && de.Event.TenantID != req.TenantID {
				continue
			}
			if req.DestinationID != "" && de.Event.DestinationID != req.DestinationID {
				continue
			}
			return copyEvent(&de.Event), nil
		}
	}
	return nil, nil
}

func (s *memLogStore) RetrieveDeliveryEvent(ctx context.Context, req driver.RetrieveDeliveryEventRequest) (*models.DeliveryEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, de := range s.deliveryEvents {
		if de.Delivery != nil && de.Delivery.ID == req.DeliveryID {
			if req.TenantID != "" && de.Event.TenantID != req.TenantID {
				continue
			}
			return copyDeliveryEvent(de), nil
		}
	}
	return nil, nil
}

func (s *memLogStore) matchesFilter(de *models.DeliveryEvent, req driver.ListDeliveryEventRequest) bool {
	if req.TenantID != "" && de.Event.TenantID != req.TenantID {
		return false
	}

	if req.EventID != "" && de.Event.ID != req.EventID {
		return false
	}

	if len(req.DestinationIDs) > 0 {
		found := false
		for _, destID := range req.DestinationIDs {
			if de.DestinationID == destID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.Status != "" && de.Delivery.Status != req.Status {
		return false
	}

	if len(req.Topics) > 0 {
		found := false
		for _, topic := range req.Topics {
			if de.Event.Topic == topic {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.TimeFilter.GTE != nil && de.Delivery.Time.Before(*req.TimeFilter.GTE) {
		return false
	}
	if req.TimeFilter.LTE != nil && de.Delivery.Time.After(*req.TimeFilter.LTE) {
		return false
	}
	if req.TimeFilter.GT != nil && !de.Delivery.Time.After(*req.TimeFilter.GT) {
		return false
	}
	if req.TimeFilter.LT != nil && !de.Delivery.Time.Before(*req.TimeFilter.LT) {
		return false
	}

	return true
}

func copyDeliveryEvent(de *models.DeliveryEvent) *models.DeliveryEvent {
	return &models.DeliveryEvent{
		ID:            de.ID,
		Attempt:       de.Attempt,
		DestinationID: de.DestinationID,
		Event:         *copyEvent(&de.Event),
		Delivery:      copyDelivery(de.Delivery),
		Manual:        de.Manual,
	}
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
		copied.Data = make(map[string]interface{}, len(e.Data))
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
		EventID:       d.EventID,
		DestinationID: d.DestinationID,
		Status:        d.Status,
		Time:          d.Time,
		Code:          d.Code,
	}

	if d.ResponseData != nil {
		copied.ResponseData = make(map[string]interface{}, len(d.ResponseData))
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
