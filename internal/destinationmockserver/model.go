package destinationmockserver

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
)

type EntityStore interface {
	ListDestination(ctx context.Context) []models.Destination
	RetrieveDestination(ctx context.Context, id string) *models.Destination
	UpsertDestination(ctx context.Context, destination models.Destination) error
	DeleteDestination(ctx context.Context, id string)

	ReceiveEvent(ctx context.Context, destinationID string, payload map[string]interface{})
	ListEvent(ctx context.Context, destinationID string) []map[string]interface{}
}

type entityStore struct {
	destinations map[string]models.Destination
	events       map[string][]map[string]interface{}
}

func NewEntityStore() EntityStore {
	return &entityStore{
		destinations: make(map[string]models.Destination),
		events:       make(map[string][]map[string]interface{}),
	}
}

func (s *entityStore) ListDestination(ctx context.Context) []models.Destination {
	destinationList := make([]models.Destination, len(s.destinations))
	index := 0
	for _, destination := range s.destinations {
		destinationList[index] = destination
		index += 1
	}
	return destinationList
}

func (s *entityStore) RetrieveDestination(ctx context.Context, id string) *models.Destination {
	destination, ok := s.destinations[id]
	if !ok {
		return nil
	}
	return &destination
}

func (s *entityStore) UpsertDestination(ctx context.Context, destination models.Destination) error {
	s.destinations[destination.ID] = destination
	return nil
}

func (s *entityStore) DeleteDestination(ctx context.Context, id string) {
	delete(s.destinations, id)
	delete(s.events, id)
}

func (s *entityStore) ReceiveEvent(ctx context.Context, destinationID string, payload map[string]interface{}) {
	if s.events[destinationID] == nil {
		s.events[destinationID] = make([]map[string]interface{}, 0)
	}
	s.events[destinationID] = append(s.events[destinationID], payload)
}

func (s *entityStore) ListEvent(ctx context.Context, destinationID string) []map[string]interface{} {
	events := s.events[destinationID]
	if events == nil {
		return []map[string]interface{}{}
	}
	return events
}
