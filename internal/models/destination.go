package models

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/hookdeck/EventKit/internal/destinationadapter"
	"github.com/hookdeck/EventKit/internal/redis"
)

type Destination struct {
	ID          string      `json:"id" redis:"id"`
	Type        string      `json:"type" redis:"type"`
	Topics      Strings     `json:"topics" redis:"-"`
	Config      Config      `json:"config" redis:"-"`
	Credentials Credentials `json:"credentials" redis:"-"`
	CreatedAt   time.Time   `json:"created_at" redis:"created_at"`
	DisabledAt  *time.Time  `json:"disabled_at" redis:"disabled_at"`
	TenantID    string      `json:"-" redis:"-"`
}

func (d *Destination) parseRedisHash(cmd *redis.MapStringStringCmd, cipher Cipher) error {
	hash, err := cmd.Result()
	if err != nil {
		return err
	}
	if len(hash) == 0 {
		return redis.Nil
	}
	if err = cmd.Scan(d); err != nil {
		return err
	}
	err = d.Topics.UnmarshalBinary([]byte(hash["topics"]))
	if err != nil {
		return fmt.Errorf("invalid topics: %w", err)
	}
	err = d.Config.UnmarshalBinary([]byte(hash["config"]))
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	credentialsBytes, err := cipher.Decrypt([]byte(hash["credentials"]))
	if err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}
	err = d.Credentials.UnmarshalBinary(credentialsBytes)
	if err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}
	return nil
}

func (d *Destination) Validate(ctx context.Context) error {
	adapter, err := destinationadapter.NewAdapater(d.Type)
	if err != nil {
		return err
	}
	return adapter.Validate(ctx, destinationadapter.Destination{
		ID:          d.ID,
		Type:        d.Type,
		Config:      d.Config,
		Credentials: d.Credentials,
	})
}

func (d *Destination) Publish(ctx context.Context, event *Event) error {
	adapter, err := destinationadapter.NewAdapater(d.Type)
	if err != nil {
		return err
	}
	return adapter.Publish(
		ctx,
		destinationadapter.Destination{
			ID:          d.ID,
			Type:        d.Type,
			Config:      d.Config,
			Credentials: d.Credentials,
		},
		event.ToAdapterEvent(),
	)
}

// ============================== Types ==============================

type Strings []string

var _ encoding.BinaryMarshaler = &Strings{}
var _ encoding.BinaryUnmarshaler = &Strings{}

func (s *Strings) MarshalBinary() ([]byte, error) {
	return json.Marshal(s)
}

func (s *Strings) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, s)
}

type Config map[string]string

var _ encoding.BinaryMarshaler = &Config{}
var _ encoding.BinaryUnmarshaler = &Config{}

func (c *Config) MarshalBinary() ([]byte, error) {
	return json.Marshal(c)
}

func (c *Config) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, c)
}

type Credentials map[string]string

var _ encoding.BinaryMarshaler = &Credentials{}
var _ encoding.BinaryUnmarshaler = &Credentials{}

func (c *Credentials) MarshalBinary() ([]byte, error) {
	return json.Marshal(c)
}

func (c *Credentials) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, c)
}

// ============================== Helpers ==============================

func FilterTopics(destinations []Destination, topic string) []Destination {
	if topic == "" {
		return destinations
	}

	filteredDestinations := []Destination{}

	for _, destination := range destinations {
		if destination.Topics[0] == "*" || slices.Contains(destination.Topics, topic) {
			filteredDestinations = append(filteredDestinations, destination)
		}
	}

	return filteredDestinations
}
