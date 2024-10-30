package models

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/hookdeck/EventKit/internal/destinationadapter"
	"github.com/hookdeck/EventKit/internal/redis"
)

var (
	ErrInvalidTopics       = errors.New("validation failed: invalid topics")
	ErrInvalidTopicsFormat = errors.New("validation failed: invalid topics format")
)

type Destination struct {
	ID          string      `json:"id" redis:"id"`
	Type        string      `json:"type" redis:"type"`
	Topics      Topics      `json:"topics" redis:"-"`
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

func (d *Destination) ValidateTopics(availableTopics []string) error {
	return d.Topics.Validate(availableTopics)
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
		return &DestinationPublishError{Err: err}
	}
	if err := adapter.Publish(
		ctx,
		destinationadapter.Destination{
			ID:          d.ID,
			Type:        d.Type,
			Config:      d.Config,
			Credentials: d.Credentials,
		},
		event.ToAdapterEvent(),
	); err != nil {
		return &DestinationPublishError{Err: err}
	}
	return nil
}

type DestinationSummary struct {
	ID     string `json:"id"`
	Topics Topics `json:"topics"`
}

var _ encoding.BinaryMarshaler = &DestinationSummary{}
var _ encoding.BinaryUnmarshaler = &DestinationSummary{}

func (ds *DestinationSummary) MarshalBinary() ([]byte, error) {
	return json.Marshal(ds)
}

func (ds *DestinationSummary) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, ds)
}

func (d *Destination) ToSummary() *DestinationSummary {
	return &DestinationSummary{
		ID:     d.ID,
		Topics: d.Topics,
	}
}

// ============================== Types ==============================

type Topics []string

var _ encoding.BinaryMarshaler = &Topics{}
var _ encoding.BinaryUnmarshaler = &Topics{}
var _ json.Marshaler = &Topics{}
var _ json.Unmarshaler = &Topics{}

func (t *Topics) MatchesAll() bool {
	return len(*t) == 1 && (*t)[0] == "*"
}

func (t *Topics) Validate(availableTopics []string) error {
	if len(*t) == 0 {
		return ErrInvalidTopics
	}
	if t.MatchesAll() {
		return nil
	}
	for _, topic := range *t {
		if topic == "*" {
			return ErrInvalidTopics
		}
		if !slices.Contains(availableTopics, topic) {
			return ErrInvalidTopics
		}
	}
	return nil
}

func (t *Topics) MarshalBinary() ([]byte, error) {
	str := strings.Join(*t, ",")
	return []byte(str), nil
}

func (t *Topics) UnmarshalBinary(data []byte) error {
	*t = TopicsFromString(string(data))
	return nil
}

func (t *Topics) MarshalJSON() ([]byte, error) {
	return json.Marshal(*t)
}

func (t *Topics) UnmarshalJSON(data []byte) error {
	if string(data) == `"*"` {
		*t = TopicsFromString("*")
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		log.Println(err)
		return ErrInvalidTopicsFormat
	}
	*t = arr
	return nil
}

func TopicsFromString(s string) Topics {
	return Topics(strings.Split(s, ","))
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

type DestinationPublishError struct {
	Err error
}

var _ error = &DestinationPublishError{}

func (e *DestinationPublishError) Error() string {
	return e.Err.Error()
}
