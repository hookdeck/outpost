package publishmq_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventHandler_MCPTopicCheck(t *testing.T) {
	// newHarness adds an mcp subscription sub_1 to user.created beside the
	// webhook d1, with check deciding whether user.created is MCP-enabled.
	newHarness := func(t *testing.T, check func(string) bool) *schemaHarness {
		h := newSchemaHarness(t, publishmq.WithMCPTopicCheck(check))
		require.NoError(t, h.store.UpsertDestination(t.Context(), testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("sub_1"),
			testutil.DestinationFactory.WithTenantID("t1"),
			testutil.DestinationFactory.WithType(models.DestinationTypeMCP),
			testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		)))
		return h
	}
	noMoreTasks := func(t *testing.T, h *schemaHarness) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		_, err := h.tasks.Receive(ctx)
		assert.Error(t, err, "no other delivery task")
	}

	t.Run("a topic no longer MCP-enabled skips its subscriptions", func(t *testing.T) {
		var calls atomic.Int32
		h := newHarness(t, func(topic string) bool {
			calls.Add(1)
			assert.Equal(t, "user.created", topic)
			return false
		})

		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))
		require.NoError(t, err)
		assert.Equal(t, []string{"d1"}, result.DestinationIDs)
		assert.Equal(t, []string{"webhook"}, result.MatchedDestinationTypes)
		assert.EqualValues(t, 1, calls.Load())

		task := h.receiveTask(t)
		assert.Equal(t, "d1", task.DestinationID, "other destinations are still delivered")
		assert.Equal(t, []string{"d1"}, task.Event.MatchedDestinationIDs)
		noMoreTasks(t, h)

		fields := h.receivedLog(t)
		assert.EqualValues(t, 1, fields["mcp_topic_disabled_count"])
		assert.EqualValues(t, 1, fields["matched_destination_count"])
	})

	t.Run("an MCP-enabled topic reaches its subscriptions", func(t *testing.T) {
		h := newHarness(t, func(string) bool { return true })

		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"d1": "webhook", "sub_1": "mcp"},
			pairs(result.DestinationIDs, result.MatchedDestinationTypes))
		delivered := map[string]bool{}
		for range 2 {
			delivered[h.receiveTask(t).DestinationID] = true
		}
		assert.Equal(t, map[string]bool{"d1": true, "sub_1": true}, delivered)
		assert.NotContains(t, h.receivedLog(t), "mcp_topic_disabled_count")
	})

	t.Run("a specific MCP destination is skipped too", func(t *testing.T) {
		h := newHarness(t, func(string) bool { return false })
		event := schemaEvent("evt_1")
		event.DestinationID = "sub_1"

		result, err := h.handler.Handle(t.Context(), event)
		require.NoError(t, err)
		assert.Empty(t, result.DestinationIDs)
		assert.Empty(t, result.MatchedDestinationTypes)
		noMoreTasks(t, h)
	})

	t.Run("the check only runs when an MCP subscription matched", func(t *testing.T) {
		var calls atomic.Int32
		h := newSchemaHarness(t, publishmq.WithMCPTopicCheck(func(string) bool {
			calls.Add(1)
			return false
		}))
		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))
		require.NoError(t, err)
		assert.Equal(t, []string{"d1"}, result.DestinationIDs)
		assert.Zero(t, calls.Load())
	})
}
