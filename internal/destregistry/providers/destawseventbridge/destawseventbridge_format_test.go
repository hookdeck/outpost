package destawseventbridge_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawseventbridge"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPublisher(t *testing.T, eventBusName string) *destawseventbridge.AWSEventBridgePublisher {
	t.Helper()
	provider, err := destawseventbridge.New(testutil.Registry.MetadataLoader(), nil, destawseventbridge.WithSource("outpost"))
	require.NoError(t, err)

	config := map[string]string{"region": "us-east-1"}
	if eventBusName != "" {
		config["event_bus_name"] = eventBusName
	}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_eventbridge"),
		testutil.DestinationFactory.WithConfig(config),
		testutil.DestinationFactory.WithCredentials(map[string]string{"key": "test", "secret": "test"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })
	return publisher.(*destawseventbridge.AWSEventBridgePublisher)
}

func TestFormat(t *testing.T) {
	t.Parallel()

	testEvent := models.Event{
		ID:       "event-123",
		Topic:    "user.created",
		TenantID: "tenant-789",
		Time:     time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Metadata: map[string]string{
			"custom_field": "custom_value",
		},
		Data: json.RawMessage(`{"message":"Hello World"}`),
	}

	t.Run("Source and EventBusName come from the provider and destination, DetailType from the event topic", func(t *testing.T) {
		t.Parallel()
		publisher := newPublisher(t, "my-bus")

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
		publisher := newPublisher(t, "")

		input, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)
		require.Len(t, input.Entries, 1)

		assert.Nil(t, input.Entries[0].EventBusName)
	})

	t.Run("Detail contains metadata and data", func(t *testing.T) {
		t.Parallel()
		publisher := newPublisher(t, "my-bus")

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
		publisher := newPublisher(t, "my-bus")

		orderEvent := testEvent
		orderEvent.Topic = "order.shipped"

		input1, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)
		input2, err := publisher.Format(context.Background(), &orderEvent)
		require.NoError(t, err)

		assert.Equal(t, "user.created", aws.ToString(input1.Entries[0].DetailType))
		assert.Equal(t, "order.shipped", aws.ToString(input2.Entries[0].DetailType))
	})

	t.Run("an event without a topic gets the default DetailType", func(t *testing.T) {
		t.Parallel()
		publisher := newPublisher(t, "my-bus")

		noTopicEvent := testEvent
		noTopicEvent.Topic = ""

		input, err := publisher.Format(context.Background(), &noTopicEvent)
		require.NoError(t, err)

		assert.Equal(t, destawseventbridge.DefaultDetailType, aws.ToString(input.Entries[0].DetailType))
	})

	t.Run("Time is the event time, not the publish time", func(t *testing.T) {
		t.Parallel()
		publisher := newPublisher(t, "my-bus")

		input, err := publisher.Format(context.Background(), &testEvent)
		require.NoError(t, err)

		require.NotNil(t, input.Entries[0].Time)
		assert.True(t, testEvent.Time.Equal(*input.Entries[0].Time))
	})
}
