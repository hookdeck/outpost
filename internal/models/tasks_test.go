package models_test

import (
	"encoding/json"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEvent_ToMessage_FromMessage_PreservesKeyOrder verifies that serialising
// an Event to an MQ message and deserialising it back preserves the original
// JSON key order in Event.Data.
func TestEvent_ToMessage_FromMessage_PreservesKeyOrder(t *testing.T) {
	t.Parallel()

	rawData := json.RawMessage(`{"z":1,"a":2}`)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithData(rawData),
	)

	msg, err := event.ToMessage()
	require.NoError(t, err)

	var restored models.Event
	err = restored.FromMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, string(rawData), string(restored.Data))
}

// TestDeliveryTask_ToMessage_FromMessage_PreservesKeyOrder verifies that
// the nested Event.Data inside a DeliveryTask survives an MQ round-trip
// with its original key order intact.
func TestDeliveryTask_ToMessage_FromMessage_PreservesKeyOrder(t *testing.T) {
	t.Parallel()

	rawData := json.RawMessage(`{"z":1,"a":2}`)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithData(rawData),
	)
	task := models.NewDeliveryTask(event, "dest_123")

	msg, err := task.ToMessage()
	require.NoError(t, err)

	var restored models.DeliveryTask
	err = restored.FromMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, string(rawData), string(restored.Event.Data))
}

// TestLogEntry_ToMessage_FromMessage_PreservesKeyOrder verifies that
// the nested Event.Data inside a LogEntry survives an MQ round-trip
// with its original key order intact.
func TestLogEntry_ToMessage_FromMessage_PreservesKeyOrder(t *testing.T) {
	t.Parallel()

	rawData := json.RawMessage(`{"z":1,"a":2}`)
	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithData(rawData),
	)
	attempt := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithEventID(event.ID),
	)
	entry := &models.LogEntry{
		Event:   event,
		Attempt: attempt,
	}

	msg, err := entry.ToMessage()
	require.NoError(t, err)

	var restored models.LogEntry
	err = restored.FromMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, string(rawData), string(restored.Event.Data))
}

func TestDeliveryTask_MessageID_NewPerDeliveryAndManualRetry(t *testing.T) {
	t.Parallel()

	event := testutil.EventFactory.Any()
	first := models.NewDeliveryTask(event, "dest_1")
	again := models.NewDeliveryTask(event, "dest_1")
	manual := models.NewManualDeliveryTask(event, "dest_1", 2)

	require.NotEmpty(t, first.MessageID)
	assert.NotEqual(t, first.MessageID, again.MessageID)
	assert.NotEqual(t, first.MessageID, manual.MessageID, "a manual retry gets a new message ID")
	assert.NotContains(t, first.MessageID, event.ID, "not derived from the event ID")
}
