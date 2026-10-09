package publishmq_test

import (
	"encoding/json"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pairs zips a result's destination IDs with their types.
func pairs(ids, types []string) map[string]string {
	m := make(map[string]string, len(ids))
	for i, id := range ids {
		m[id] = types[i]
	}
	return m
}

func TestEventHandler_MCPDestinations(t *testing.T) {
	// newHarness adds an mcp subscription sub_1 to user.created beside the
	// webhook d1 of the schema harness.
	newHarness := func(t *testing.T) *schemaHarness {
		h := newSchemaHarness(t)
		require.NoError(t, h.store.UpsertDestination(t.Context(), testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("sub_1"),
			testutil.DestinationFactory.WithTenantID("t1"),
			testutil.DestinationFactory.WithType(models.DestinationTypeMCP),
			testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		)))
		return h
	}

	t.Run("event carries only non-MCP matches, the result all", func(t *testing.T) {
		h := newHarness(t)

		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))
		require.NoError(t, err)
		require.Len(t, result.MatchedDestinationTypes, len(result.DestinationIDs))
		assert.Equal(t, map[string]string{"d1": "webhook", "sub_1": "mcp"},
			pairs(result.DestinationIDs, result.MatchedDestinationTypes))

		delivered := map[string]bool{}
		for range 2 {
			task := h.receiveTask(t)
			delivered[task.DestinationID] = true
			assert.Equal(t, []string{"d1"}, task.Event.MatchedDestinationIDs)
		}
		assert.Equal(t, map[string]bool{"d1": true, "sub_1": true}, delivered, "MCP subscriptions are still delivered")
	})

	t.Run("types stay out of the JSON response", func(t *testing.T) {
		h := newHarness(t)
		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))
		require.NoError(t, err)
		b, err := json.Marshal(result)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(b, &fields))
		assert.Len(t, fields, 3)
		assert.Contains(t, fields, "id")
		assert.Contains(t, fields, "duplicate")
		assert.Contains(t, fields, "destination_ids")
	})

	t.Run("MCP-only match", func(t *testing.T) {
		h := newHarness(t)
		require.NoError(t, h.store.DeleteDestination(t.Context(), "t1", "d1"))

		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))
		require.NoError(t, err)
		assert.Equal(t, []string{"sub_1"}, result.DestinationIDs)
		assert.Equal(t, []string{"mcp"}, result.MatchedDestinationTypes)

		task := h.receiveTask(t)
		assert.Equal(t, "sub_1", task.DestinationID)
		assert.NotNil(t, task.Event.MatchedDestinationIDs)
		assert.Empty(t, task.Event.MatchedDestinationIDs)
	})

	t.Run("specific MCP destination", func(t *testing.T) {
		h := newHarness(t)
		event := schemaEvent("evt_1")
		event.DestinationID = "sub_1"

		result, err := h.handler.Handle(t.Context(), event)
		require.NoError(t, err)
		assert.Equal(t, []string{"sub_1"}, result.DestinationIDs)
		assert.Equal(t, []string{"mcp"}, result.MatchedDestinationTypes)
		assert.Empty(t, h.receiveTask(t).Event.MatchedDestinationIDs)
	})

	t.Run("a * publish never reaches MCP subscriptions", func(t *testing.T) {
		h := newHarness(t)
		event := schemaEvent("evt_1")
		event.Topic = "*"

		result, err := h.handler.Handle(t.Context(), event)
		require.NoError(t, err)
		assert.Equal(t, []string{"d1"}, result.DestinationIDs)
		assert.Equal(t, []string{"webhook"}, result.MatchedDestinationTypes)
	})

	t.Run("no match", func(t *testing.T) {
		h := newHarness(t)
		event := schemaEvent("evt_1")
		event.TenantID = "nobody"

		result, err := h.handler.Handle(t.Context(), event)
		require.NoError(t, err)
		assert.NotNil(t, result.DestinationIDs)
		assert.Empty(t, result.DestinationIDs)
		assert.NotNil(t, event.MatchedDestinationIDs)
		assert.Empty(t, event.MatchedDestinationIDs)
	})
}
