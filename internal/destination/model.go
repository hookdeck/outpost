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

func GetDestination(c context.Context, id string) (*Destination, error) {
	destination, err := redis.Client().Get(c, redisDestinationID(id)).Result()
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

func SetDestination(c context.Context, destination Destination) error {
	if err := redis.Client().Set(c, redisDestinationID(destination.ID), destination.Name, 0).Err(); err != nil {
		return err
	}
	return nil
}

func ClearDestination(c context.Context, id string) (*Destination, error) {
	destination, err := GetDestination(c, id)
	if err != nil {
		return nil, err
	}
	if destination == nil {
		return nil, nil
	}
	if err := redis.Client().Del(c, redisDestinationID(id)).Err(); err != nil {
		return nil, err
	}
	return destination, nil
}

func redisDestinationID(destinationID string) string {
	return fmt.Sprintf("destination:%s", destinationID)
}
