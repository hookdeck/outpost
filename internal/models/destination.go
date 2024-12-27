package models

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
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
	// Check for deleted resource before scanning
	if _, exists := hash["deleted_at"]; exists {
		return ErrDestinationDeleted
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

func (d *Destination) Validate(topics []string) error {
	if err := d.Topics.Validate(topics); err != nil {
		return err
	}
	return nil
}

type DestinationSummary struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Topics   Topics `json:"topics"`
	Disabled bool   `json:"disabled"`
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
		ID:       d.ID,
		Type:     d.Type,
		Topics:   d.Topics,
		Disabled: d.DisabledAt != nil,
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
	// If no available topics are configured, allow any topics
	if len(availableTopics) == 0 {
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
var _ json.Unmarshaler = &Credentials{}

func (c *Credentials) MarshalBinary() ([]byte, error) {
	return json.Marshal(c)
}

func (c *Credentials) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, c)
}

func (c *Credentials) UnmarshalJSON(data []byte) error {
	// First try to unmarshal as map[string]string
	var stringMap map[string]string
	if err := json.Unmarshal(data, &stringMap); err == nil {
		*c = stringMap
		return nil
	}

	// If that fails, try map[string]interface{} to handle mixed types
	var mixedMap map[string]interface{}
	if err := json.Unmarshal(data, &mixedMap); err != nil {
		return err
	}

	// Convert all values to strings
	result := make(map[string]string)
	for k, v := range mixedMap {
		switch val := v.(type) {
		case string:
			result[k] = val
		case bool:
			result[k] = fmt.Sprintf("%v", val)
		case float64:
			result[k] = fmt.Sprintf("%v", val)
		case nil:
			result[k] = ""
		default:
			// For other types, try to convert to string using JSON marshaling
			if b, err := json.Marshal(val); err == nil {
				result[k] = string(b)
			} else {
				result[k] = fmt.Sprintf("%v", val)
			}
		}
	}

	*c = result
	return nil
}
