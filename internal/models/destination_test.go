package models_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDestinationTopics_Validate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		topics          models.Topics
		availableTopics []string
		validated       bool
	}

	testCases := []testCase{
		{
			topics:          []string{"user.created"},
			availableTopics: testutil.TestTopics,
			validated:       true,
		},
		{
			topics:          []string{"user.created", "user.updated"},
			availableTopics: testutil.TestTopics,
			validated:       true,
		},
		{
			topics:          []string{"*"},
			availableTopics: testutil.TestTopics,
			validated:       true,
		},
		{
			topics:          []string{"*", "user.created"},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		{
			topics:          []string{"user.invalid"},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		{
			topics:          []string{"user.created", "user.invalid"},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		{
			topics:          []string{},
			availableTopics: testutil.TestTopics,
			validated:       false,
		},
		// Test cases for empty availableTopics
		{
			topics:          []string{"any.topic"},
			availableTopics: []string{},
			validated:       true,
		},
		{
			topics:          []string{"any.topic", "another.topic"},
			availableTopics: []string{},
			validated:       true,
		},
		{
			topics:          []string{"*"},
			availableTopics: []string{},
			validated:       true,
		},
		{
			topics:          []string{},
			availableTopics: []string{},
			validated:       false, // still require at least one topic
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("validate topics %v with available topics %v", tc.topics, tc.availableTopics), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.validated, tc.topics.Validate(tc.availableTopics) == nil)
		})
	}
}

func TestDestination_JSONMarshalWithDeliveryMetadata(t *testing.T) {
	t.Parallel()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_123"),
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": "https://example.com"}),
		testutil.DestinationFactory.WithDeliveryMetadata(map[string]string{
			"app-id":     "my-app",
			"source":     "outpost",
			"custom-key": "custom-value",
		}),
		testutil.DestinationFactory.WithMetadata(map[string]string{
			"description": "Production webhook",
			"team":        "platform",
		}),
	)

	// Marshal to JSON
	jsonBytes, err := json.Marshal(destination)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled models.Destination
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	assert.NoError(t, err)

	// Verify new fields are preserved
	assert.Equal(t, destination.DeliveryMetadata, unmarshaled.DeliveryMetadata)
	assert.Equal(t, "my-app", unmarshaled.DeliveryMetadata["app-id"])
	assert.Equal(t, "outpost", unmarshaled.DeliveryMetadata["source"])
	assert.Equal(t, "custom-value", unmarshaled.DeliveryMetadata["custom-key"])

	assert.Equal(t, destination.Metadata, unmarshaled.Metadata)
	assert.Equal(t, "Production webhook", unmarshaled.Metadata["description"])
	assert.Equal(t, "platform", unmarshaled.Metadata["team"])

	// Verify existing fields still work
	assert.Equal(t, destination.ID, unmarshaled.ID)
	assert.Equal(t, destination.Type, unmarshaled.Type)
	assert.Equal(t, destination.Topics, unmarshaled.Topics)
}

func TestDestination_JSONMarshalWithoutNewFields(t *testing.T) {
	t.Parallel()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_123"),
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTopics([]string{"user.created"}),
	)

	// Marshal to JSON
	jsonBytes, err := json.Marshal(destination)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled models.Destination
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	assert.NoError(t, err)

	// Verify new fields are nil when not provided
	assert.Nil(t, unmarshaled.DeliveryMetadata)
	assert.Nil(t, unmarshaled.Metadata)
}

func TestDestination_JSONUnmarshalEmptyMaps(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"id": "dest_123",
		"tenant_id": "tenant_1",
		"type": "webhook",
		"topics": ["user.created"],
		"config": {},
		"credentials": {},
		"delivery_metadata": {},
		"metadata": {},
		"created_at": "2024-01-01T00:00:00Z"
	}`

	var destination models.Destination
	err := json.Unmarshal([]byte(jsonData), &destination)
	assert.NoError(t, err)

	// Empty maps should be preserved as empty, not nil
	assert.NotNil(t, destination.DeliveryMetadata)
	assert.Empty(t, destination.DeliveryMetadata)
	assert.NotNil(t, destination.Metadata)
	assert.Empty(t, destination.Metadata)
}

func TestTopics_MatchTopic(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name       string
		topics     models.Topics
		eventTopic string
		expected   bool
	}

	testCases := []testCase{
		// Event topic is empty string (matches all)
		{
			name:       "empty event topic matches any destination",
			topics:     []string{"user.created"},
			eventTopic: "",
			expected:   true,
		},
		// Event topic is wildcard (matches all)
		{
			name:       "wildcard event topic matches any destination",
			topics:     []string{"user.created"},
			eventTopic: "*",
			expected:   true,
		},
		// Destination has wildcard topic (matches all)
		{
			name:       "destination with wildcard matches any event topic",
			topics:     []string{"*"},
			eventTopic: "user.created",
			expected:   true,
		},
		// Exact match
		{
			name:       "exact topic match",
			topics:     []string{"user.created", "user.updated"},
			eventTopic: "user.created",
			expected:   true,
		},
		// No match
		{
			name:       "no topic match",
			topics:     []string{"user.created", "user.updated"},
			eventTopic: "user.deleted",
			expected:   false,
		},
		// Empty destination topics (edge case)
		{
			name:       "empty destination topics does not match",
			topics:     []string{},
			eventTopic: "user.created",
			expected:   false,
		},
		// Empty destination topics with wildcard event
		{
			name:       "empty destination topics matches wildcard event",
			topics:     []string{},
			eventTopic: "*",
			expected:   true,
		},
		// Empty destination topics with empty event
		{
			name:       "empty destination topics matches empty event",
			topics:     []string{},
			eventTopic: "",
			expected:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.topics.MatchTopic(tc.eventTopic))
		})
	}
}

func TestFilter_MarshalBinary(t *testing.T) {
	t.Parallel()

	t.Run("nil filter marshals to nil", func(t *testing.T) {
		t.Parallel()
		var f models.Filter
		data, err := f.MarshalBinary()
		assert.NoError(t, err)
		assert.Nil(t, data)
	})

	t.Run("empty filter marshals to nil", func(t *testing.T) {
		t.Parallel()
		f := models.Filter{}
		data, err := f.MarshalBinary()
		assert.NoError(t, err)
		assert.Nil(t, data)
	})

	t.Run("filter with data marshals to JSON", func(t *testing.T) {
		t.Parallel()
		f := models.Filter{
			"data": map[string]any{
				"type": "order.created",
			},
		}
		data, err := f.MarshalBinary()
		assert.NoError(t, err)
		assert.NotNil(t, data)

		var unmarshaled map[string]any
		err = json.Unmarshal(data, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, "order.created", unmarshaled["data"].(map[string]any)["type"])
	})
}

func TestFilter_UnmarshalBinary(t *testing.T) {
	t.Parallel()

	t.Run("empty data unmarshals to nil filter", func(t *testing.T) {
		t.Parallel()
		var f models.Filter
		err := f.UnmarshalBinary([]byte{})
		assert.NoError(t, err)
		assert.Nil(t, f)
	})

	t.Run("JSON data unmarshals correctly", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"data":{"type":"order.created"}}`)
		var f models.Filter
		err := f.UnmarshalBinary(data)
		assert.NoError(t, err)
		assert.Equal(t, "order.created", f["data"].(map[string]any)["type"])
	})
}

func TestDestinationSummary_MatchFilter(t *testing.T) {
	t.Parallel()

	baseEvent := testutil.EventFactory.Any(
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithMetadata(map[string]string{
			"source":     "api",
			"user_id":    "user_456",
			"request_id": "req_789",
		}),
		testutil.EventFactory.WithData(map[string]interface{}{
			"type":   "order.created",
			"amount": float64(100),
			"customer": map[string]any{
				"id":   "cust_123",
				"tier": "premium",
			},
		}),
	)

	testCases := []struct {
		name     string
		filter   models.Filter
		event    models.Event
		expected bool
	}{
		{
			name:     "nil filter matches all events",
			filter:   nil,
			event:    baseEvent,
			expected: true,
		},
		{
			name:     "empty filter matches all events",
			filter:   models.Filter{},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by data.type exact match",
			filter: models.Filter{
				"data": map[string]any{
					"type": "order.created",
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by data.type no match",
			filter: models.Filter{
				"data": map[string]any{
					"type": "order.updated",
				},
			},
			event:    baseEvent,
			expected: false,
		},
		{
			name: "filter by metadata.source exact match",
			filter: models.Filter{
				"metadata": map[string]any{
					"source": "api",
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by metadata.source no match",
			filter: models.Filter{
				"metadata": map[string]any{
					"source": "webhook",
				},
			},
			event:    baseEvent,
			expected: false,
		},
		{
			name: "filter by topic exact match",
			filter: models.Filter{
				"topic": "order.created",
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by nested data.customer.tier",
			filter: models.Filter{
				"data": map[string]any{
					"customer": map[string]any{
						"tier": "premium",
					},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by nested data.customer.tier no match",
			filter: models.Filter{
				"data": map[string]any{
					"customer": map[string]any{
						"tier": "basic",
					},
				},
			},
			event:    baseEvent,
			expected: false,
		},
		{
			name: "filter with $gt operator",
			filter: models.Filter{
				"data": map[string]any{
					"amount": map[string]any{
						"$gt": float64(50),
					},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter with $gt operator no match",
			filter: models.Filter{
				"data": map[string]any{
					"amount": map[string]any{
						"$gt": float64(150),
					},
				},
			},
			event:    baseEvent,
			expected: false,
		},
		{
			name: "filter with $in operator",
			filter: models.Filter{
				"data": map[string]any{
					"type": map[string]any{
						"$in": []any{"order.created", "order.updated"},
					},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter with $in operator no match",
			filter: models.Filter{
				"data": map[string]any{
					"type": map[string]any{
						"$in": []any{"order.deleted", "order.cancelled"},
					},
				},
			},
			event:    baseEvent,
			expected: false,
		},
		{
			name: "filter with $or operator",
			filter: models.Filter{
				"$or": []any{
					map[string]any{"data": map[string]any{"type": "order.created"}},
					map[string]any{"data": map[string]any{"type": "order.updated"}},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter with $and operator",
			filter: models.Filter{
				"$and": []any{
					map[string]any{"data": map[string]any{"type": "order.created"}},
					map[string]any{"metadata": map[string]any{"source": "api"}},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter with $and operator partial match fails",
			filter: models.Filter{
				"$and": []any{
					map[string]any{"data": map[string]any{"type": "order.created"}},
					map[string]any{"metadata": map[string]any{"source": "webhook"}},
				},
			},
			event:    baseEvent,
			expected: false,
		},
		{
			name: "filter with $not operator at top level",
			filter: models.Filter{
				"$not": map[string]any{
					"data": map[string]any{
						"type": "order.deleted",
					},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter with $startsWith operator",
			filter: models.Filter{
				"data": map[string]any{
					"type": map[string]any{
						"$startsWith": "order.",
					},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter with $exist operator true",
			filter: models.Filter{
				"data": map[string]any{
					"amount": map[string]any{
						"$exist": true,
					},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter with $exist operator false",
			filter: models.Filter{
				"data": map[string]any{
					"missing_field": map[string]any{
						"$exist": false,
					},
				},
			},
			event:    baseEvent,
			expected: true,
		},
		// Time comparison tests - RFC3339 timestamps are lexicographically sortable
		{
			name: "filter by time $gt (after date)",
			filter: models.Filter{
				"time": map[string]any{
					"$gt": "2020-01-01T00:00:00Z",
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by time $lt (before date)",
			filter: models.Filter{
				"time": map[string]any{
					"$lt": "2099-12-31T23:59:59Z",
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by time $gte and $lte (date range)",
			filter: models.Filter{
				"time": map[string]any{
					"$gte": "2020-01-01T00:00:00Z",
					"$lte": "2099-12-31T23:59:59Z",
				},
			},
			event:    baseEvent,
			expected: true,
		},
		{
			name: "filter by time no match (event before filter date)",
			filter: models.Filter{
				"time": map[string]any{
					"$gt": "2099-01-01T00:00:00Z",
				},
			},
			event:    baseEvent,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ds := models.DestinationSummary{
				ID:     "dest_1",
				Type:   "webhook",
				Topics: []string{"*"},
				Filter: tc.filter,
			}
			assert.Equal(t, tc.expected, ds.MatchFilter(tc.event))
		})
	}
}

func TestDestination_JSONMarshalWithFilter(t *testing.T) {
	t.Parallel()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_123"),
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		testutil.DestinationFactory.WithFilter(models.Filter{
			"data": map[string]any{
				"type": "order.created",
			},
		}),
	)

	// Marshal to JSON
	jsonBytes, err := json.Marshal(destination)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled models.Destination
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	assert.NoError(t, err)

	// Verify filter is preserved
	assert.NotNil(t, unmarshaled.Filter)
	assert.Equal(t, "order.created", unmarshaled.Filter["data"].(map[string]any)["type"])
}

func TestDestinationSummary_ToSummaryIncludesFilter(t *testing.T) {
	t.Parallel()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_123"),
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTopics([]string{"*"}),
		testutil.DestinationFactory.WithFilter(models.Filter{
			"data": map[string]any{
				"type": "order.created",
			},
		}),
	)

	summary := destination.ToSummary()

	assert.Equal(t, destination.ID, summary.ID)
	assert.Equal(t, destination.Type, summary.Type)
	assert.Equal(t, destination.Topics, summary.Topics)
	assert.Equal(t, destination.Filter, summary.Filter)
}
