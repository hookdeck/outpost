package destawseventbridge_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawseventbridge"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	t.Parallel()

	testEvent := models.Event{
		ID:       "event-123",
		Topic:    "user.created",
		TenantID: "tenant-789",
		Time:     time.Now(),
		Metadata: map[string]string{
			"custom_field": "custom_value",
		},
		Data: json.RawMessage(`{"message":"Hello World"}`),
	}

	t.Run("Source, DetailType, and EventBusName are set from provider config, not per-event", func(t *testing.T) {
		t.Parallel()
		publisher := destawseventbridge.NewAWSEventBridgePublisher(nil, "my-bus", "outpost")

		input, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)
		require.Len(t, input.Entries, 1)

		entry := input.Entries[0]
		assert.Equal(t, "outpost", aws.ToString(entry.Source))
		assert.Equal(t, "user.created", aws.ToString(entry.DetailType), "DetailType should be the event topic")
		assert.Equal(t, "my-bus", aws.ToString(entry.EventBusName))
	})

	t.Run("EventBusName is omitted (uses account default bus) when not configured", func(t *testing.T) {
		t.Parallel()
		publisher := destawseventbridge.NewAWSEventBridgePublisher(nil, "", "outpost")

		input, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)
		require.Len(t, input.Entries, 1)

		assert.Nil(t, input.Entries[0].EventBusName)
	})

	t.Run("Detail contains metadata and data, same envelope shape as aws_kinesis", func(t *testing.T) {
		t.Parallel()
		publisher := destawseventbridge.NewAWSEventBridgePublisher(nil, "my-bus", "outpost")

		input, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)

		var detail map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(aws.ToString(input.Entries[0].Detail)), &detail))

		metadata, ok := detail["metadata"].(map[string]interface{})
		require.True(t, ok, "metadata should be a map")
		assert.Equal(t, testEvent.Topic, metadata["topic"])
		assert.Equal(t, testEvent.ID, metadata["event-id"])
		assert.Equal(t, "custom_value", metadata["custom_field"])
		assert.Contains(t, metadata, "timestamp")

		dataJSON, err := json.Marshal(detail["data"])
		require.NoError(t, err)
		assert.JSONEq(t, string(testEvent.Data), string(dataJSON))
	})

	t.Run("two different topics produce two different DetailType values", func(t *testing.T) {
		t.Parallel()
		publisher := destawseventbridge.NewAWSEventBridgePublisher(nil, "my-bus", "outpost")

		orderEvent := testEvent
		orderEvent.Topic = "order.shipped"

		input1, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)
		input2, err := publisher.Format(context.Background(), &orderEvent)
		require.NoError(t, err)

		assert.Equal(t, "user.created", aws.ToString(input1.Entries[0].DetailType))
		assert.Equal(t, "order.shipped", aws.ToString(input2.Entries[0].DetailType))
	})
}
