package destinationmockserver

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
)

type EntityStore interface {
	ListDestination(ctx context.Context) []models.Destination
	UpsertDestination(ctx context.Context, destination models.Destination) error
	DeleteDestination(ctx context.Context, id string)
}

type entityStore struct {
	destinations map[string]models.Destination
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

func (s *entityStore) UpsertDestination(ctx context.Context, destination models.Destination) error {
	s.destinations[destination.ID] = destination
	return nil
}

func (s *entityStore) DeleteDestination(ctx context.Context, id string) {
	delete(s.destinations, id)
}

func NewEntityStore() EntityStore {
	return &entityStore{
		destinations: make(map[string]models.Destination),
	}
}
