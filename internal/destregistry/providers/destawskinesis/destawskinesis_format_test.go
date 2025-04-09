package destawskinesis_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatWithPartitionKey tests different partition key template scenarios
func TestFormatWithPartitionKey(t *testing.T) {
	testCases := []struct {
		name                 string
		partitionKeyTpl      string
		event                models.Event
		metadataInPayload    bool
		expectedPartitionKey string
		expectedError        bool
	}{
		{
			name:            "Default template (event ID)",
			partitionKeyTpl: "",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
				},
			},
			metadataInPayload:    false,
			expectedPartitionKey: "event-123",
			expectedError:        false,
		},
		{
			name:            "Simple metadata field access",
			partitionKeyTpl: "metadata.topic",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "test-topic",
			expectedError:        false,
		},
		{
			name:            "Data field access",
			partitionKeyTpl: "data.user_id",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
					"user_id": "user-456",
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "user-456",
			expectedError:        false,
		},
		{
			name:            "Join metadata fields",
			partitionKeyTpl: "join('-', [metadata.topic, metadata.\"event-id\"])",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "test-topic-event-123",
			expectedError:        false,
		},
		{
			name:            "Join data fields",
			partitionKeyTpl: "join(':', [data.user_id, data.message])",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
					"user_id": "user-456",
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "user-456:Hello World",
			expectedError:        false,
		},
		{
			name:            "Invalid template syntax",
			partitionKeyTpl: "metadata.topic[",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "event-123", // Fallback to event ID
			expectedError:        false,       // We don't return error, just fall back
		},
		{
			name:            "Non-existent field returns event ID",
			partitionKeyTpl: "metadata.nonexistent",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "event-123", // Fallback to event ID
			expectedError:        false,       // We don't return error, just fall back
		},
		{
			name:            "With metadata in payload",
			partitionKeyTpl: "metadata.topic",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Metadata: map[string]string{
					"custom_field": "custom_value",
				},
				Data: map[string]interface{}{
					"message": "Hello World",
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "test-topic",
			expectedError:        false,
		},
		{
			name:            "Nested data access",
			partitionKeyTpl: "data.user.id",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
					"user": map[string]interface{}{
						"id":   "user-456",
						"name": "Test User",
					},
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "user-456",
			expectedError:        false,
		},
		{
			name:            "Numeric value in data",
			partitionKeyTpl: "data.count",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
					"count":   123,
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "123",
			expectedError:        false,
		},
		{
			name:            "Boolean value in data",
			partitionKeyTpl: "data.active",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
					"active":  true,
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "true",
			expectedError:        false,
		},
		{
			name:            "Complex expression",
			partitionKeyTpl: "join('-', [metadata.topic, to_string(data.count)])",
			event: models.Event{
				ID:    "event-123",
				Topic: "test-topic",
				Data: map[string]interface{}{
					"message": "Hello World",
					"count":   42,
				},
			},
			metadataInPayload:    true,
			expectedPartitionKey: "test-topic-42",
			expectedError:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create publisher using the helper function
			publisher := destawskinesis.NewAWSKinesisPublisher(
				nil,
				"test-stream",
				tc.partitionKeyTpl,
				tc.metadataInPayload,
			)

			// Call Format
			result, err := publisher.Format(context.Background(), &tc.event)

			if tc.expectedError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Only verify the partition key
			assert.Equal(t, tc.expectedPartitionKey, *result.PartitionKey)
		})
	}
}

// TestMetadataInPayload tests the metadata structure generated in the payload
func TestMetadataInPayload(t *testing.T) {
	testEvent := models.Event{
		ID:       "event-123",
		Topic:    "test-topic",
		TenantID: "tenant-789",
		Time:     time.Now(),
		Metadata: map[string]string{
			"custom_field":  "custom_value",
			"another_field": "42",
		},
		Data: map[string]interface{}{
			"message": "Hello World",
		},
	}

	t.Run("With metadata in payload", func(t *testing.T) {
		publisher := destawskinesis.NewAWSKinesisPublisher(
			nil,
			"test-stream",
			"metadata.topic",
			true, // metadataInPayload = true
		)

		result, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)

		// Convert result to JSON
		var actual map[string]interface{}
		err = json.Unmarshal(result.Data, &actual)
		require.NoError(t, err)

		// Verify metadata fields
		metadata, ok := actual["metadata"].(map[string]interface{})
		require.True(t, ok, "metadata should be a map")

		// Check core system metadata
		assert.Equal(t, testEvent.Topic, metadata["topic"])
		assert.Equal(t, testEvent.ID, metadata["event-id"])
		assert.Contains(t, metadata, "timestamp")

		// Check custom metadata
		assert.Equal(t, "custom_value", metadata["custom_field"])
		assert.Equal(t, "42", metadata["another_field"])

		// Check data using JSONEq
		dataJSON, err := json.Marshal(testEvent.Data)
		require.NoError(t, err)

		actualDataJSON, err := json.Marshal(actual["data"])
		require.NoError(t, err)

		assert.JSONEq(t, string(dataJSON), string(actualDataJSON))
	})

	t.Run("Without metadata in payload", func(t *testing.T) {
		publisher := destawskinesis.NewAWSKinesisPublisher(
			nil,
			"test-stream",
			"metadata.topic",
			false, // metadataInPayload = false
		)

		result, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)

		// Convert result to JSON and verify it's just the data
		var actualData map[string]interface{}
		err = json.Unmarshal(result.Data, &actualData)
		require.NoError(t, err)

		// When metadata is not in payload, the entire payload should be the data
		// No metadata field should be present
		assert.NotContains(t, actualData, "metadata")

		// The payload should be the event data directly
		dataJSON, err := json.Marshal(testEvent.Data)
		require.NoError(t, err)

		resultJSON, err := json.Marshal(actualData)
		require.NoError(t, err)

		assert.JSONEq(t, string(dataJSON), string(resultJSON))
	})
}
