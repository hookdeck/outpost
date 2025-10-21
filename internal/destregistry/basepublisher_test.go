package destregistry_test

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMakeMetadata_WithoutDeliveryMetadata(t *testing.T) {
	t.Parallel()

	publisher := destregistry.NewBasePublisher()
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithMetadata(map[string]string{
			"user-id": "usr_456",
			"action":  "signup",
		}),
	)
	timestamp := time.Unix(1609459200, 0) // 2021-01-01 00:00:00 UTC

	metadata := publisher.MakeMetadata(&event, timestamp)

	// System metadata should be present
	assert.Equal(t, "1609459200", metadata["timestamp"])
	assert.Equal(t, "evt_123", metadata["event-id"])
	assert.Equal(t, "user.created", metadata["topic"])

	// Event metadata should be present
	assert.Equal(t, "usr_456", metadata["user-id"])
	assert.Equal(t, "signup", metadata["action"])

	// Should have exactly 5 keys (3 system + 2 event)
	assert.Len(t, metadata, 5)
}

func TestMakeMetadata_WithDeliveryMetadata(t *testing.T) {
	t.Parallel()

	publisher := destregistry.NewBasePublisher(
		destregistry.WithDeliveryMetadata(map[string]string{
			"app-id": "my-app",
			"source": "outpost",
			"region": "us-east-1",
		}),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithMetadata(map[string]string{
			"user-id": "usr_456",
		}),
	)
	timestamp := time.Unix(1609459200, 0)

	metadata := publisher.MakeMetadata(&event, timestamp)

	// System metadata should be present
	assert.Equal(t, "1609459200", metadata["timestamp"])
	assert.Equal(t, "evt_123", metadata["event-id"])
	assert.Equal(t, "user.created", metadata["topic"])

	// Delivery metadata should be present
	assert.Equal(t, "my-app", metadata["app-id"])
	assert.Equal(t, "outpost", metadata["source"])
	assert.Equal(t, "us-east-1", metadata["region"])

	// Event metadata should be present
	assert.Equal(t, "usr_456", metadata["user-id"])

	// Should have 7 keys (3 system + 3 delivery + 1 event)
	assert.Len(t, metadata, 7)
}

func TestMakeMetadata_MergePriority(t *testing.T) {
	t.Parallel()

	// Test the merge priority: System < DeliveryMetadata < Event
	// Expected behavior:
	// - System metadata has lowest priority
	// - Delivery metadata can override system metadata
	// - Event metadata has highest priority and can override both

	publisher := destregistry.NewBasePublisher(
		destregistry.WithDeliveryMetadata(map[string]string{
			"timestamp": "999",     // Should override system timestamp
			"app-id":    "my-app",  // New key from delivery metadata
			"source":    "outpost", // Will be overridden by event metadata
		}),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithMetadata(map[string]string{
			"source":  "user-service", // Should override delivery metadata "source"
			"user-id": "usr_456",      // New key from event metadata
		}),
	)
	timestamp := time.Unix(1609459200, 0) // System timestamp: 1609459200

	metadata := publisher.MakeMetadata(&event, timestamp)

	// System metadata
	assert.Equal(t, "evt_123", metadata["event-id"], "system event-id should be present")
	assert.Equal(t, "user.created", metadata["topic"], "system topic should be present")

	// Delivery metadata should override system timestamp
	assert.Equal(t, "999", metadata["timestamp"], "delivery_metadata should override system timestamp")

	// Delivery metadata new keys
	assert.Equal(t, "my-app", metadata["app-id"], "delivery_metadata app-id should be present")

	// Event metadata should override delivery metadata source
	assert.Equal(t, "user-service", metadata["source"], "event metadata should override delivery_metadata source")

	// Event metadata new keys
	assert.Equal(t, "usr_456", metadata["user-id"], "event user-id should be present")

	// Should have 6 keys: event-id, topic, timestamp, app-id, source, user-id
	assert.Len(t, metadata, 6)
}

func TestMakeMetadata_WithMillisecondTimestamp(t *testing.T) {
	t.Parallel()

	publisher := destregistry.NewBasePublisher(
		destregistry.WithMillisecondTimestamp(true),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
	)
	timestamp := time.Unix(1609459200, 123456789) // With nanoseconds

	metadata := publisher.MakeMetadata(&event, timestamp)

	// Should include both timestamp and timestamp-ms
	assert.Equal(t, "1609459200", metadata["timestamp"])
	assert.Equal(t, "1609459200123", metadata["timestamp-ms"])
}

func TestMakeMetadata_WithMillisecondTimestampAndDeliveryMetadata(t *testing.T) {
	t.Parallel()

	// Test that delivery_metadata can override millisecond timestamp too
	publisher := destregistry.NewBasePublisher(
		destregistry.WithMillisecondTimestamp(true),
		destregistry.WithDeliveryMetadata(map[string]string{
			"timestamp-ms": "999999999999", // Override the millisecond timestamp
		}),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
	)
	timestamp := time.Unix(1609459200, 123456789)

	metadata := publisher.MakeMetadata(&event, timestamp)

	// Delivery metadata should override system timestamp-ms
	assert.Equal(t, "999999999999", metadata["timestamp-ms"])
}

func TestMakeMetadata_EmptyDeliveryMetadata(t *testing.T) {
	t.Parallel()

	// Test with empty delivery metadata (should behave same as without)
	publisher := destregistry.NewBasePublisher(
		destregistry.WithDeliveryMetadata(map[string]string{}),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithMetadata(nil), // Explicitly set to nil
	)
	timestamp := time.Unix(1609459200, 0)

	metadata := publisher.MakeMetadata(&event, timestamp)

	// Should only have system metadata
	assert.Equal(t, "1609459200", metadata["timestamp"])
	assert.Equal(t, "evt_123", metadata["event-id"])
	assert.Equal(t, "user.created", metadata["topic"])
	assert.Len(t, metadata, 3)
}

func TestMakeMetadata_NilEventMetadata(t *testing.T) {
	t.Parallel()

	// Test with nil event metadata
	publisher := destregistry.NewBasePublisher(
		destregistry.WithDeliveryMetadata(map[string]string{
			"app-id": "my-app",
		}),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithMetadata(nil), // Explicitly set to nil
	)
	timestamp := time.Unix(1609459200, 0)

	metadata := publisher.MakeMetadata(&event, timestamp)

	// System metadata
	assert.Equal(t, "1609459200", metadata["timestamp"])
	assert.Equal(t, "evt_123", metadata["event-id"])
	assert.Equal(t, "user.created", metadata["topic"])

	// Delivery metadata
	assert.Equal(t, "my-app", metadata["app-id"])

	// Should have 4 keys (3 system + 1 delivery)
	assert.Len(t, metadata, 4)
}
