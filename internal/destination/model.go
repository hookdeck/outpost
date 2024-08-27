package destination

import (
	"context"
	"fmt"

	"github.com/hookdeck/EventKit/internal/redis"
)

type Destination struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CreateDestinationRequest struct {
	Name string `json:"name" binding:"required"`
}

type UpdateDestinationRequest struct {
	Name string `json:"name" binding:"required"`
}

type DestinationModel struct {
	redisClient *redis.Client
}

func NewDestinationModel(redisClient *redis.Client) *DestinationModel {
	return &DestinationModel{
		redisClient: redisClient,
	}
}

func (m *DestinationModel) Get(c context.Context, id string) (*Destination, error) {
	destination, err := m.redisClient.Get(c, redisDestinationID(id)).Result()
	if err == redis.Nil {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &Destination{
		ID:   id,
		Name: destination,
	}, nil
}

func (m *DestinationModel) Set(c context.Context, destination Destination) error {
	if err := m.redisClient.Set(c, redisDestinationID(destination.ID), destination.Name, 0).Err(); err != nil {
		return err
	}
	return nil
}

func (m *DestinationModel) Clear(c context.Context, id string) (*Destination, error) {
	destination, err := m.Get(c, id)
	if err != nil {
		return nil, err
	}
	if destination == nil {
		return nil, nil
	}
	if err := m.redisClient.Del(c, redisDestinationID(id)).Err(); err != nil {
		return nil, err
	}
	return destination, nil
}

func redisDestinationID(destinationID string) string {
	return fmt.Sprintf("destination:%s", destinationID)
}
