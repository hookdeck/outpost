package memlogstore

import (
	"context"
	"sort"
	"sync"

	"github.com/hookdeck/outpost/internal/logstore/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
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

func (s *memLogStore) InsertManyDeliveryEvent(ctx context.Context, deliveryEvents []*models.DeliveryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Deep copy to avoid external mutation
	for _, de := range deliveryEvents {
		copied := &models.DeliveryEvent{
			ID:            de.ID,
			DestinationID: de.DestinationID,
			Event:         de.Event,
			Delivery:      de.Delivery,
		}
		s.deliveryEvents = append(s.deliveryEvents, copied)
	}
	return nil
}

func (s *memLogStore) ListDeliveryEvent(ctx context.Context, req driver.ListDeliveryEventRequest) (driver.ListDeliveryEventResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Validate and set defaults for sort parameters
	sortBy := req.SortBy
	if sortBy != "event_time" && sortBy != "delivery_time" {
		sortBy = "delivery_time"
	}
	sortOrder := req.SortOrder
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Decode and validate cursors
	nextCursor, prevCursor, err := cursor.DecodeAndValidate(req.Next, req.Prev, sortBy, sortOrder)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, err
	}

	// Filter
	var filtered []*models.DeliveryEvent
	for _, de := range s.deliveryEvents {
		if !s.matchesFilter(de, req) {
			continue
		}
		filtered = append(filtered, de)
	}

	// Sort using multi-column ordering for deterministic pagination.
	// See drivertest.go for detailed documentation on sorting logic.
	//
	// Summary:
	// - delivery_time: ORDER BY delivery_time, delivery_id
	// - event_time:    ORDER BY event_time, event_id, delivery_time
	//
	// The secondary/tertiary columns ensure deterministic ordering when
	// primary sort values are identical (e.g., multiple deliveries for same event).
	isDesc := sortOrder == "desc"

	sort.Slice(filtered, func(i, j int) bool {
		if sortBy == "event_time" {
			// Primary: event_time
			if !filtered[i].Event.Time.Equal(filtered[j].Event.Time) {
				if isDesc {
					return filtered[i].Event.Time.After(filtered[j].Event.Time)
				}
				return filtered[i].Event.Time.Before(filtered[j].Event.Time)
			}
			// Secondary: event_id (groups deliveries for same event)
			if filtered[i].Event.ID != filtered[j].Event.ID {
				if isDesc {
					return filtered[i].Event.ID > filtered[j].Event.ID
				}
				return filtered[i].Event.ID < filtered[j].Event.ID
			}
			// Tertiary: delivery_time
			if !filtered[i].Delivery.Time.Equal(filtered[j].Delivery.Time) {
				if isDesc {
					return filtered[i].Delivery.Time.After(filtered[j].Delivery.Time)
				}
				return filtered[i].Delivery.Time.Before(filtered[j].Delivery.Time)
			}
			// Quaternary: delivery_id (for deterministic ordering when all above are equal)
			if isDesc {
				return filtered[i].Delivery.ID > filtered[j].Delivery.ID
			}
			return filtered[i].Delivery.ID < filtered[j].Delivery.ID
		}

		// Default: delivery_time
		// Primary: delivery_time
		if !filtered[i].Delivery.Time.Equal(filtered[j].Delivery.Time) {
			if isDesc {
				return filtered[i].Delivery.Time.After(filtered[j].Delivery.Time)
			}
			return filtered[i].Delivery.Time.Before(filtered[j].Delivery.Time)
		}
		// Secondary: delivery_id (for deterministic ordering)
		if isDesc {
			return filtered[i].Delivery.ID > filtered[j].Delivery.ID
		}
		return filtered[i].Delivery.ID < filtered[j].Delivery.ID
	})

	// Handle pagination
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	// Find start index based on cursor
	// The cursor position is the DeliveryEvent.ID
	startIdx := 0
	if !nextCursor.IsEmpty() {
		// Next cursor: find the item and start from there
		for i, de := range filtered {
			if de.ID == nextCursor.Position {
				startIdx = i
				break
			}
		}
	} else if !prevCursor.IsEmpty() {
		// Prev cursor: find the item and go back by limit
		for i, de := range filtered {
			if de.ID == prevCursor.Position {
				startIdx = i - limit
				if startIdx < 0 {
					startIdx = 0
				}
				break
			}
		}
	}

	// Slice the results
	endIdx := startIdx + limit
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}

	// Deep copy to ensure immutability
	data := make([]*models.DeliveryEvent, endIdx-startIdx)
	for i, de := range filtered[startIdx:endIdx] {
		data[i] = copyDeliveryEvent(de)
	}

	// Build cursors with sort parameters encoded
	var nextEncoded, prevEncoded string
	if endIdx < len(filtered) {
		nextEncoded = cursor.Encode(cursor.Cursor{
			SortBy:    sortBy,
			SortOrder: sortOrder,
			Position:  filtered[endIdx].ID,
		})
	}
	if startIdx > 0 {
		prevEncoded = cursor.Encode(cursor.Cursor{
			SortBy:    sortBy,
			SortOrder: sortOrder,
			Position:  filtered[startIdx].ID,
		})
	}

	return driver.ListDeliveryEventResponse{
		Data: data,
		Next: nextEncoded,
		Prev: prevEncoded,
	}, nil
}

func (s *memLogStore) RetrieveEvent(ctx context.Context, req driver.RetrieveEventRequest) (*models.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, de := range s.deliveryEvents {
		if de.Event.ID == req.EventID && de.Event.TenantID == req.TenantID {
			if req.DestinationID != "" && de.Event.DestinationID != req.DestinationID {
				continue
			}
			// Return a deep copy to ensure immutability
			return copyEvent(&de.Event), nil
		}
	}
	return nil, nil
}

func (s *memLogStore) matchesFilter(de *models.DeliveryEvent, req driver.ListDeliveryEventRequest) bool {
	// Tenant filter (required)
	if de.Event.TenantID != req.TenantID {
		return false
	}

	// Event ID filter
	if req.EventID != "" && de.Event.ID != req.EventID {
		return false
	}

	// Destination filter
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

	// Status filter
	if req.Status != "" && de.Delivery.Status != req.Status {
		return false
	}

	// Topics filter
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

	// Event time filter
	if req.EventStart != nil && de.Event.Time.Before(*req.EventStart) {
		return false
	}
	if req.EventEnd != nil && de.Event.Time.After(*req.EventEnd) {
		return false
	}

	// Delivery time filter
	if req.DeliveryStart != nil && de.Delivery.Time.Before(*req.DeliveryStart) {
		return false
	}
	if req.DeliveryEnd != nil && de.Delivery.Time.After(*req.DeliveryEnd) {
		return false
	}

	return true
}

// Deep copy helpers to ensure data immutability

func copyDeliveryEvent(de *models.DeliveryEvent) *models.DeliveryEvent {
	return &models.DeliveryEvent{
		ID:            de.ID,
		DestinationID: de.DestinationID,
		Event:         *copyEvent(&de.Event),
		Delivery:      copyDelivery(de.Delivery),
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

	// Deep copy maps
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

	// Deep copy ResponseData map if present
	if d.ResponseData != nil {
		copied.ResponseData = make(map[string]interface{}, len(d.ResponseData))
		for k, v := range d.ResponseData {
			copied.ResponseData[k] = v
		}
	}

	return copied
}
