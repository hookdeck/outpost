package memlogstore

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
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

	nextPosition, err := cursor.Decode(req.Next, cursorResourceEvent, cursorVersion)
	if err != nil {
		return driver.ListEventResponse{}, convertCursorError(err)
	}
	prevPosition, err := cursor.Decode(req.Prev, cursorResourceEvent, cursorVersion)
	if err != nil {
		return driver.ListEventResponse{}, convertCursorError(err)
	}

	// Dedupe by event ID
	eventMap := make(map[string]*models.Event)
	for _, de := range s.deliveryEvents {
		if !s.matchesEventFilter(&de.Event, req) {
			continue
		}
		if _, exists := eventMap[de.Event.ID]; !exists {
			eventMap[de.Event.ID] = copyEvent(&de.Event)
		}
	}

	var filtered []*models.Event
	for _, event := range eventMap {
		filtered = append(filtered, event)
	}

	isDesc := sortOrder == "desc"
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].Time.Equal(filtered[j].Time) {
			if isDesc {
				return filtered[i].Time.After(filtered[j].Time)
			}
			return filtered[i].Time.Before(filtered[j].Time)
		}
		if isDesc {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].ID < filtered[j].ID
	})

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	startIdx := 0
	if nextPosition != "" {
		for i, event := range filtered {
			if event.ID == nextPosition {
				startIdx = i
				break
			}
		}
	} else if prevPosition != "" {
		for i, event := range filtered {
			if event.ID == prevPosition {
				startIdx = i - limit
				if startIdx < 0 {
					startIdx = 0
				}
				break
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}

	data := make([]*models.Event, endIdx-startIdx)
	for i, event := range filtered[startIdx:endIdx] {
		data[i] = copyEvent(event)
	}

	var nextEncoded, prevEncoded string
	if endIdx < len(filtered) {
		nextEncoded = cursor.Encode(cursorResourceEvent, cursorVersion, filtered[endIdx].ID)
	}
	if startIdx > 0 {
		prevEncoded = cursor.Encode(cursorResourceEvent, cursorVersion, filtered[startIdx].ID)
	}

	return driver.ListEventResponse{
		Data: data,
		Next: nextEncoded,
		Prev: prevEncoded,
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

	if req.EventStart != nil && event.Time.Before(*req.EventStart) {
		return false
	}
	if req.EventEnd != nil && event.Time.After(*req.EventEnd) {
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

	nextPosition, err := cursor.Decode(req.Next, cursorResourceDelivery, cursorVersion)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, convertCursorError(err)
	}
	prevPosition, err := cursor.Decode(req.Prev, cursorResourceDelivery, cursorVersion)
	if err != nil {
		return driver.ListDeliveryEventResponse{}, convertCursorError(err)
	}

	var filtered []*models.DeliveryEvent
	for _, de := range s.deliveryEvents {
		if !s.matchesFilter(de, req) {
			continue
		}
		filtered = append(filtered, de)
	}

	isDesc := sortOrder == "desc"
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].Delivery.Time.Equal(filtered[j].Delivery.Time) {
			if isDesc {
				return filtered[i].Delivery.Time.After(filtered[j].Delivery.Time)
			}
			return filtered[i].Delivery.Time.Before(filtered[j].Delivery.Time)
		}
		if isDesc {
			return filtered[i].Delivery.ID > filtered[j].Delivery.ID
		}
		return filtered[i].Delivery.ID < filtered[j].Delivery.ID
	})

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	startIdx := 0
	if nextPosition != "" {
		for i, de := range filtered {
			if de.ID == nextPosition {
				startIdx = i
				break
			}
		}
	} else if prevPosition != "" {
		for i, de := range filtered {
			if de.ID == prevPosition {
				startIdx = i - limit
				if startIdx < 0 {
					startIdx = 0
				}
				break
			}
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}

	data := make([]*models.DeliveryEvent, endIdx-startIdx)
	for i, de := range filtered[startIdx:endIdx] {
		data[i] = copyDeliveryEvent(de)
	}

	var nextEncoded, prevEncoded string
	if endIdx < len(filtered) {
		nextEncoded = cursor.Encode(cursorResourceDelivery, cursorVersion, filtered[endIdx].ID)
	}
	if startIdx > 0 {
		prevEncoded = cursor.Encode(cursorResourceDelivery, cursorVersion, filtered[startIdx].ID)
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

	if req.Start != nil && de.Delivery.Time.Before(*req.Start) {
		return false
	}
	if req.End != nil && de.Delivery.Time.After(*req.End) {
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

// convertCursorError converts cursor package errors to driver errors.
func convertCursorError(err error) error {
	if errors.Is(err, cursor.ErrInvalidCursor) || errors.Is(err, cursor.ErrVersionMismatch) {
		return driver.ErrInvalidCursor
	}
	return err
}
