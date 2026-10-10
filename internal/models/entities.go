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
	// ExpiresAt is when the destination stops receiving events. Nil means it
	// never expires.
	ExpiresAt *time.Time `json:"expires_at,omitempty" redis:"-"`
}

// DestinationTypeMCP is the type of MCP event subscriptions.
const DestinationTypeMCP = "mcp"

// ExactTopicTypes lists the destination types that only receive events
// published to one of their own topics, spelled exactly: never a topic-less
// or "*" publish, and neither "*" nor wildcard patterns in their topics match
// anything. Read-only.
var ExactTopicTypes = map[string]struct{}{
	DestinationTypeMCP: {},
}

// IsExactTopicType reports whether destinations of type typ match topics
// exactly (see ExactTopicTypes).
func IsExactTopicType(typ string) bool {
	_, ok := ExactTopicTypes[typ]
	return ok
}

func (d *Destination) Validate(topics []string, allowWildcards bool) error {
	if err := d.Topics.Validate(topics, allowWildcards); err != nil {
		return err
	}
	return nil
}

// IsExpired reports whether the destination has an expiry at or before now.
func (d *Destination) IsExpired(now time.Time) bool {
	return d.ExpiresAt != nil && !now.Before(*d.ExpiresAt)
}

// MatchEvent checks if the destination matches the given event.
// Returns true if the destination is enabled, not expired, topic matches, and
// filter matches. Embedded wildcard topic patterns are ignored when
// allowWildcards is false.
func (d *Destination) MatchEvent(event Event, allowWildcards bool) bool {
	if d.DisabledAt != nil || d.IsExpired(time.Now()) {
		return false
	}
	if !MatchDestinationTopic(d.Type, d.Topics, event.Topic, allowWildcards) {
		return false
	}
	return MatchFilter(d.Filter, event)
}

// MatchDestinationTopic reports whether a destination of type typ subscribed
// to topics receives an event published to eventTopic. Types listed in
// ExactTopicTypes only match an exact, concrete topic.
func MatchDestinationTopic(typ string, topics Topics, eventTopic string, allowWildcards bool) bool {
	if IsExactTopicType(typ) {
		return topics.MatchExactTopic(eventTopic)
	}
	return topics.MatchTopic(eventTopic, allowWildcards)
}

// FilterInput is the document destination filters are evaluated against. It
// is built once per event with NewFilterInput and shared by every filter
// matched against that event; matching never modifies it.
type FilterInput map[string]any

// NewFilterInput builds the filter input for event, parsing its data once.
func NewFilterInput(event Event) FilterInput {
	input := FilterInput{
		"id":       event.ID,
		"topic":    event.Topic,
		"time":     event.Time.Format("2006-01-02T15:04:05Z07:00"),
		"metadata": map[string]any{},
		"data":     map[string]any{},
	}
	// Convert metadata to map[string]any
	if event.Metadata != nil {
		metadata := make(map[string]any, len(event.Metadata))
		for k, v := range event.Metadata {
			metadata[k] = v
		}
		input["metadata"] = metadata
	}
	// Parse data from raw JSON.
	// ParsedData() should never fail here: ingestion validates that Data is a
	// valid JSON object. If it does fail, we fall back to empty data so the
	// filter runs against no data fields (likely a no-match).
	parsed, err := event.ParsedData()
	if err == nil && parsed != nil {
		input["data"] = parsed
	}
	return input
}

// MatchFilterInput checks if the filter input matches the filter.
// Returns true if no filter is set (nil or empty) or if the input matches.
func MatchFilterInput(filter Filter, input FilterInput) bool {
	if len(filter) == 0 {
		return true
	}
	return simplejsonmatch.Match(map[string]any(input), map[string]any(filter))
}

// MatchFilter checks if the given event matches the filter.
// Returns true if no filter is set (nil or empty) or if the event matches the filter.
// To match several filters against one event, build the input once with
// NewFilterInput and use MatchFilterInput.
func MatchFilter(filter Filter, event Event) bool {
	if len(filter) == 0 {
		return true
	}
	return MatchFilterInput(filter, NewFilterInput(event))
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
	// SchemaValid is the publish-time payload schema verdict: true or false
	// when the topic validates its data (false only in warn mode, as enforce
	// rejects the publish), nil when unchecked. Outpost sets it; it is never
	// bound from publish input.
	SchemaValid *bool `json:"schema_valid,omitempty"`

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
	// Provider response time in ms, measured around Publish. Nil when the
	// attempt never reached the provider or predates latency tracking.
	LatencyMs *int64 `json:"latency_ms"`
}

// ============================== Types ==============================

type Topics []string

// isWildcardPattern reports whether topic embeds a wildcard, such as "user.*".
// The standalone "*" is not a pattern.
func isWildcardPattern(topic string) bool {
	return topic != "*" && strings.Contains(topic, "*")
}

func (t *Topics) MatchesAll() bool {
	return len(*t) == 1 && (*t)[0] == "*"
}

// MatchTopic reports whether an event topic matches this subscription.
// The standalone "*" keeps its subscribe-to-all meaning when allowWildcards is
// false, while embedded wildcard patterns such as "user.*" are ignored.
func (t *Topics) MatchTopic(eventTopic string, allowWildcards bool) bool {
	if eventTopic == "" || eventTopic == "*" || t.MatchesAll() {
		return true
	}
	for _, topic := range *t {
		if !allowWildcards && isWildcardPattern(topic) {
			continue
		}
		if matchTopicPattern(topic, eventTopic) {
			return true
		}
	}
	return false
}

// MatchExactTopic reports whether eventTopic is a concrete topic (neither
// empty nor "*") listed verbatim in this subscription. "*" and wildcard
// patterns in the subscription match nothing.
func (t *Topics) MatchExactTopic(eventTopic string) bool {
	if eventTopic == "" || eventTopic == "*" {
		return false
	}
	return slices.Contains(*t, eventTopic)
}

// WithoutWildcardPatterns returns the topics with embedded wildcard patterns
// such as "user.*" removed. The standalone "*" is kept.
func (t Topics) WithoutWildcardPatterns() Topics {
	if t == nil {
		return nil
	}
	result := make(Topics, 0, len(t))
	for _, topic := range t {
		if !isWildcardPattern(topic) {
			result = append(result, topic)
		}
	}
	return result
}

func (t *Topics) Validate(availableTopics []string, allowWildcards bool) error {
	if len(*t) == 0 {
		return ErrInvalidTopics
	}
	if t.MatchesAll() {
		return nil
	}
	// If no available topics are configured, allow any exact topic.
	if len(availableTopics) == 0 {
		if !allowWildcards {
			for _, topic := range *t {
				if strings.Contains(topic, "*") {
					return ErrInvalidTopics
				}
			}
		}
		return nil
	}
	for _, topic := range *t {
		if topic == "*" {
			return ErrInvalidTopics
		}
		if isWildcardPattern(topic) {
			if !allowWildcards {
				return ErrInvalidTopics
			}
			if !topicPatternMatchesAny(topic, availableTopics) {
				return ErrInvalidTopics
			}
			continue
		}
		if !slices.Contains(availableTopics, topic) {
			return ErrInvalidTopics
		}
	}
	return nil
}

// Normalize returns a topic set with redundant entries removed, preserving
// first-seen order. It performs two reductions:
//
//   - exact duplicates are collapsed to their first occurrence
//     (["user.created","user.created"] -> ["user.created"]).
//   - an entry is folded away when a sibling wildcard pattern covers it and the
//     entry does not itself cover that sibling
//     (["user.*","user.created"] -> ["user.*"]).
//
// Two mutually-non-covering patterns are both kept
// (["*.created","user.*"] is unchanged), and ["*"] is returned as-is.
// Normalization never changes MatchTopic results: it only drops entries that
// are already covered by a retained entry.
func (t Topics) Normalize() Topics {
	if t.MatchesAll() || len(t) <= 1 {
		return t
	}
	result := make(Topics, 0, len(t))
	for _, e := range t {
		if slices.Contains(result, e) {
			continue // exact duplicate of an already-kept entry
		}
		if coveredByOther(e, t) {
			continue // folded into a strictly-more-general sibling pattern
		}
		result = append(result, e)
	}
	return result
}

// coveredByOther reports whether entry e is covered by some other entry p in
// topics such that p covers e but e does not cover p (p is strictly more
// general). This keeps mutually-covering entries and pattern pairs that neither
// covers, so only strictly-redundant entries are folded.
func coveredByOther(e string, topics Topics) bool {
	for _, p := range topics {
		if p == e {
			continue
		}
		if matchTopicPattern(p, e) && !matchTopicPattern(e, p) {
			return true
		}
	}
	return false
}

func topicPatternMatchesAny(pattern string, topics []string) bool {
	for _, topic := range topics {
		if matchTopicPattern(pattern, topic) {
			return true
		}
	}
	return false
}

func matchTopicPattern(pattern, topic string) bool {
	if pattern == topic {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return false
	}

	patternIndex, topicIndex := 0, 0
	starIndex, starTopicIndex := -1, 0
	for topicIndex < len(topic) {
		if patternIndex < len(pattern) && pattern[patternIndex] == topic[topicIndex] {
			patternIndex++
			topicIndex++
			continue
		}
		if patternIndex < len(pattern) && pattern[patternIndex] == '*' {
			starIndex = patternIndex
			starTopicIndex = topicIndex
			patternIndex++
			continue
		}
		if starIndex != -1 {
			patternIndex = starIndex + 1
			starTopicIndex++
			topicIndex = starTopicIndex
			continue
		}
		return false
	}
	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}

func TopicsFromString(s string) Topics {
	return Topics(strings.Split(s, ","))
}
