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
