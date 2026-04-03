package models

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/simplejsonmatch"
)

var (
	ErrInvalidTopics       = errors.New("validation failed: invalid topics")
	ErrInvalidTopicsFormat = errors.New("validation failed: invalid topics format")
)

type Tenant struct {
	ID                string    `json:"id" redis:"id"`
	DestinationsCount int       `json:"destinations_count" redis:"-"`
	Topics            []string  `json:"topics" redis:"-"`
	Metadata          Metadata  `json:"metadata,omitempty" redis:"-"`
	CreatedAt         time.Time `json:"created_at" redis:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" redis:"updated_at"`
}

type Destination struct {
	ID               string           `json:"id" redis:"id"`
	TenantID         string           `json:"tenant_id" redis:"-"`
	Type             string           `json:"type" redis:"type"`
	Topics           Topics           `json:"topics" redis:"-"`
	Filter           Filter           `json:"filter,omitempty" redis:"-"`
	Config           Config           `json:"config" redis:"-"`
	Credentials      Credentials      `json:"credentials" redis:"-"`
	DeliveryMetadata DeliveryMetadata `json:"delivery_metadata,omitempty" redis:"-"`
	Metadata         Metadata         `json:"metadata,omitempty" redis:"-"`
	CreatedAt        time.Time        `json:"created_at" redis:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at" redis:"updated_at"`
	DisabledAt       *time.Time       `json:"disabled_at" redis:"disabled_at"`
}

func (d *Destination) Validate(topics []string) error {
	if err := d.Topics.Validate(topics); err != nil {
		return err
	}
	return nil
}

// MatchEvent checks if the destination matches the given event.
// Returns true if the destination is enabled, topic matches, and filter matches.
func (d *Destination) MatchEvent(event Event) bool {
	if d.DisabledAt != nil {
		return false
	}
	if !d.Topics.MatchTopic(event.Topic) {
		return false
	}
	return MatchFilter(d.Filter, event)
}

// MatchFilter checks if the given event matches the filter.
// Returns true if no filter is set (nil or empty) or if the event matches the filter.
func MatchFilter(filter Filter, event Event) bool {
	if len(filter) == 0 {
		return true
	}
	// Build the filter input from the event
	filterInput := map[string]any{
		"id":       event.ID,
		"topic":    event.Topic,
		"time":     event.Time.Format("2006-01-02T15:04:05Z07:00"),
		"metadata": map[string]any{},
		"data":     map[string]any{},
	}
	// Convert metadata to map[string]any
	if event.Metadata != nil {
		metadata := make(map[string]any)
		for k, v := range event.Metadata {
			metadata[k] = v
		}
		filterInput["metadata"] = metadata
	}
	// Parse data from raw JSON.
	// ParsedData() should never fail here: ingestion validates that Data is a
	// valid JSON object. If it does fail, we fall back to empty data so the
	// filter runs against no data fields (likely a no-match).
	parsed, err := event.ParsedData()
	if err == nil && parsed != nil {
		filterInput["data"] = parsed
	}
	return simplejsonmatch.Match(filterInput, map[string]any(filter))
}

type Event struct {
	ID                    string    `json:"id"`
	TenantID              string    `json:"tenant_id"`
	DestinationID         string    `json:"destination_id"`
	MatchedDestinationIDs []string  `json:"matched_destination_ids"`
	Topic                 string    `json:"topic"`
	EligibleForRetry      bool      `json:"eligible_for_retry"`
	Time                  time.Time `json:"time"`
	Metadata              Metadata  `json:"metadata"`
	Data                  Data      `json:"data"`

	// Telemetry data, must exist to properly trace events between publish receiver & delivery handler
	Telemetry *EventTelemetry `json:"telemetry,omitempty"`
}

// ParsedData unmarshals the raw JSON Data into a map[string]any.
// This is used by code that needs to inspect individual fields (e.g. filters,
// partition-key extraction) without losing the original byte representation.
func (e *Event) ParsedData() (map[string]any, error) {
	if len(e.Data) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(e.Data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

const (
	AttemptStatusSuccess = "success"
	AttemptStatusFailed  = "failed"
)

type Attempt struct {
	ID              string                 `json:"id"`
	TenantID        string                 `json:"tenant_id"`
	EventID         string                 `json:"event_id"`
	DestinationID   string                 `json:"destination_id"`
	DestinationType string                 `json:"destination_type"`
	AttemptNumber   int                    `json:"attempt_number"`
	Manual          bool                   `json:"manual"`
	Status          string                 `json:"status"`
	Time            time.Time              `json:"time"`
	Code            string                 `json:"code"`
	ResponseData    map[string]interface{} `json:"response_data"`
}

// ============================== Types ==============================

type Topics []string

func (t *Topics) MatchesAll() bool {
	return len(*t) == 1 && (*t)[0] == "*"
}

func (t *Topics) MatchTopic(eventTopic string) bool {
	return eventTopic == "" || eventTopic == "*" || t.MatchesAll() || slices.Contains(*t, eventTopic)
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

func TopicsFromString(s string) Topics {
	return Topics(strings.Split(s, ","))
}
