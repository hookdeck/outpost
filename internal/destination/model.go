package destination

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
)

type Destination struct {
	ID         string     `json:"id" redis:"id"`
	Type       string     `json:"type" redis:"type"`
	Topics     strings    `json:"topics" redis:"-"` // type not supported by redis-go
	CreatedAt  time.Time  `json:"created_at" redis:"created_at"`
	DisabledAt *time.Time `json:"disabled_at" redis:"disabled_at"`
}

type CreateDestinationRequest struct {
	Type   string   `json:"type" binding:"required"`
	Topics []string `json:"topics" binding:"required"`
}

type UpdateDestinationRequest struct {
	Type   string   `json:"type" binding:"-"`
	Topics []string `json:"topics" binding:"-"`
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
	cmd := m.redisClient.HGetAll(c, redisDestinationID(id))
	hash, err := cmd.Result()
	if err != nil {
		return nil, err
	}
	if len(hash) == 0 {
		return nil, nil
	}
	destination := &Destination{}
	if err = cmd.Scan(destination); err != nil {
		return nil, err
	}
	err = destination.Topics.UnmarshalBinary([]byte(hash["topics"]))
	if err != nil {
		return nil, fmt.Errorf("invalid topics: %w", err)
	}
	return destination, nil
}

func (m *DestinationModel) Set(ctx context.Context, destination Destination) error {
	key := redisDestinationID(destination.ID)
	_, err := m.redisClient.Pipelined(ctx, func(r redis.Pipeliner) error {
		r.HSet(ctx, key, "id", destination.ID)
		r.HSet(ctx, key, "type", destination.Type)
		r.HSet(ctx, key, "topics", &destination.Topics)
		r.HSet(ctx, key, "created_at", destination.CreatedAt)
		if destination.DisabledAt != nil {
			r.HSet(ctx, key, "disabled_at", destination.DisabledAt)
		}
		return nil
	})
	return err
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

type strings []string

var _ encoding.BinaryMarshaler = &strings{}
var _ encoding.BinaryUnmarshaler = &strings{}

func (s *strings) MarshalBinary() ([]byte, error) {
	return json.Marshal(s)
}

func (s *strings) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, s)
}
