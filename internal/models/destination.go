package models

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
)

type Destination struct {
	ID         string     `json:"id" redis:"id"`
	Type       string     `json:"type" redis:"type"`
	Topics     Strings    `json:"topics" redis:"-"` // type not supported by redis-go
	CreatedAt  time.Time  `json:"created_at" redis:"created_at"`
	DisabledAt *time.Time `json:"disabled_at" redis:"disabled_at"`
	TenantID   string     `json:"-" redis:"-"`
}

type DestinationModel struct{}

func NewDestinationModel() *DestinationModel {
	return &DestinationModel{}
}

func (m *DestinationModel) Get(c context.Context, cmdable redis.Cmdable, id, tenantID string) (*Destination, error) {
	cmd := cmdable.HGetAll(c, redisDestinationID(id, tenantID))
	return m.parse(c, tenantID, cmd)
}

func (m *DestinationModel) Set(ctx context.Context, cmdable redis.Cmdable, destination Destination) error {
	key := redisDestinationID(destination.ID, destination.TenantID)
	_, err := cmdable.Pipelined(ctx, func(r redis.Pipeliner) error {
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

func (m *DestinationModel) Clear(c context.Context, cmdable redis.Cmdable, id, tenantID string) error {
	return cmdable.Del(c, redisDestinationID(id, tenantID)).Err()
}

func (m *DestinationModel) ClearMany(c context.Context, cmdable redis.Cmdable, tenantID string, ids ...string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisDestinationID(id, tenantID)
	}
	return cmdable.Del(c, keys...).Result()
}

// TODO: get this from config
const MAX_DESTINATIONS_PER_TENANT = 100

// TODO: consider splitting this into two methods, one for getting keys and one for getting values
// in case the flow doesn't require the destination values (DELETE /:tenantID)
// NOTE: this method requires its own client as it uses an internal pipeline.
func (m *DestinationModel) List(c context.Context, client *redis.Client, tenantID string) ([]Destination, error) {
	keys, _, err := client.Scan(c, 0, redisDestinationID("*", tenantID), MAX_DESTINATIONS_PER_TENANT).Result()
	if err != nil {
		return nil, err
	}

	pipe := client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.HGetAll(c, key)
	}
	_, err = pipe.Exec(c)
	if err != nil {
		return nil, err
	}

	destinations := make([]Destination, len(keys))
	for i, cmd := range cmds {
		destination, err := m.parse(c, tenantID, cmd)
		if err != nil {
			return []Destination{}, err
		}
		destinations[i] = *destination
	}

	sort.Slice(destinations, func(i, j int) bool {
		return destinations[i].CreatedAt.Before(destinations[j].CreatedAt)
	})

	return destinations, nil
}

func (m *DestinationModel) parse(_ context.Context, tenantID string, cmd *redis.MapStringStringCmd) (*Destination, error) {
	hash, err := cmd.Result()
	if err != nil {
		return nil, err
	}
	if len(hash) == 0 {
		return nil, nil
	}
	destination := &Destination{}
	destination.TenantID = tenantID
	if err = cmd.Scan(destination); err != nil {
		return nil, err
	}
	err = destination.Topics.UnmarshalBinary([]byte(hash["topics"]))
	if err != nil {
		return nil, fmt.Errorf("invalid topics: %w", err)
	}
	return destination, nil
}

func redisDestinationID(destinationID, tenantID string) string {
	return fmt.Sprintf("tenant:%s:destination:%s", tenantID, destinationID)
}

type Strings []string

var _ encoding.BinaryMarshaler = &Strings{}
var _ encoding.BinaryUnmarshaler = &Strings{}

func (s *Strings) MarshalBinary() ([]byte, error) {
	return json.Marshal(s)
}

func (s *Strings) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, s)
}
