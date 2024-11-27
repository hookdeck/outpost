package destinationmockserver

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/models"
)

type EntityStore interface {
	ListDestination(ctx context.Context) ([]models.Destination, error)
	RetrieveDestination(ctx context.Context, id string) (*models.Destination, error)
	UpsertDestination(ctx context.Context, destination models.Destination) error
	DeleteDestination(ctx context.Context, id string) error

	ReceiveEvent(ctx context.Context, destinationID string, payload map[string]interface{}, metadata map[string]string) (*Event, error)
	ListEvent(ctx context.Context, destinationID string) ([]Event, error)
}

type Event struct {
	Success bool                   `json:"success"`
	Payload map[string]interface{} `json:"payload"`
}

type entityStore struct {
	destinations map[string]models.Destination
	events       map[string][]Event
}

func NewEntityStore() EntityStore {
	return &entityStore{
		destinations: make(map[string]models.Destination),
		events:       make(map[string][]Event),
	}
}

func (s *entityStore) ListDestination(ctx context.Context) ([]models.Destination, error) {
	destinationList := make([]models.Destination, len(s.destinations))
	index := 0
	for _, destination := range s.destinations {
		destinationList[index] = destination
		index += 1
	}
	return destinationList, nil
}

func (s *entityStore) RetrieveDestination(ctx context.Context, id string) (*models.Destination, error) {
	destination, ok := s.destinations[id]
	if !ok {
		return nil, errors.New("destination not found")
	}
	return &destination, nil
}

func (s *entityStore) UpsertDestination(ctx context.Context, destination models.Destination) error {
	s.destinations[destination.ID] = destination
	return nil
}

func (s *entityStore) DeleteDestination(ctx context.Context, id string) error {
	if _, ok := s.destinations[id]; !ok {
		return errors.New("destination not found")
	}
	delete(s.destinations, id)
	delete(s.events, id)
	return nil
}

func (s *entityStore) ReceiveEvent(ctx context.Context, destinationID string, payload map[string]interface{}, metadata map[string]string) (*Event, error) {
	if _, ok := s.destinations[destinationID]; !ok {
		return nil, errors.New("destination not found")
	}
	if s.events[destinationID] == nil {
		s.events[destinationID] = make([]Event, 0)
	}
	event := Event{Success: true, Payload: payload}
	if metadata["should_err"] == "true" {
		event.Success = false
	}
	s.events[destinationID] = append(s.events[destinationID], event)
	return &event, nil
}

func (s *entityStore) ListEvent(ctx context.Context, destinationID string) ([]Event, error) {
	events, ok := s.events[destinationID]
	if !ok {
		return nil, errors.New("no events found for destination")
	}
	return events, nil
}
