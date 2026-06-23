package drivertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCRUD tests basic CRUD operations with a single shared harness and dataset.
func testCRUD(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	// Shared test data
	tenantID := idgen.String()
	destinationIDs := []string{idgen.Destination(), idgen.Destination(), idgen.Destination()}
	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-48 * time.Hour)

	// We'll populate these as we insert
	var allDeliveries []*models.Attempt
	destinationEvents := map[string][]*models.Event{}
	topicEvents := map[string][]*models.Event{}
	statusDeliveries := map[string][]*models.Attempt{}

	t.Run("insert and verify", func(t *testing.T) {
		t.Run("single delivery", func(t *testing.T) {
			destID := destinationIDs[0]
			topic := testutil.TestTopics[0]
			event := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID("single_evt"),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithDestinationID(destID),
				testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
				testutil.EventFactory.WithTopic(topic),
				testutil.EventFactory.WithTime(baseTime.Add(-30*time.Minute)),
			)
			delivery := testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID("single_del"),
				testutil.AttemptFactory.WithTenantID(tenantID),
				testutil.AttemptFactory.WithEventID(event.ID),
				testutil.AttemptFactory.WithDestinationID(destID),
				testutil.AttemptFactory.WithStatus("success"),
				testutil.AttemptFactory.WithTime(baseTime.Add(-30*time.Minute)),
			)

			err := logStore.InsertMany(ctx, []*models.LogEntry{{Event: event, Attempt: delivery}})
			require.NoError(t, err)
			require.NoError(t, h.FlushWrites(ctx))

			// Track in maps for later filter tests
			destinationEvents[destID] = append(destinationEvents[destID], event)
			topicEvents[topic] = append(topicEvents[topic], event)
			statusDeliveries["success"] = append(statusDeliveries["success"], delivery)

			// Verify via List
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{tenantID},
				EventIDs:   []string{event.ID},
				Limit:      10,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.Equal(t, event.ID, response.Data[0].Event.ID)
			assert.Equal(t, "success", response.Data[0].Attempt.Status)

			// Verify via Retrieve
			retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: tenantID,
				EventID:  event.ID,
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved)
			assert.Equal(t, event.ID, retrieved.ID)
		})

		t.Run("batch deliveries", func(t *testing.T) {
			// Create 15 events spread across destinations and topics for filter testing
			var entries []*models.LogEntry

			for i := range 15 {
				destID := destinationIDs[i%len(destinationIDs)]
				topic := testutil.TestTopics[i%len(testutil.TestTopics)]
				eventTime := baseTime.Add(-time.Duration(i+1) * time.Minute)
				status := "success"
				if i%2 == 1 {
					status = "failed"
				}

				event := testutil.EventFactory.AnyPointer(
					testutil.EventFactory.WithID(fmt.Sprintf("batch_evt_%02d", i)),
					testutil.EventFactory.WithTenantID(tenantID),
					testutil.EventFactory.WithDestinationID(destID),
					testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
					testutil.EventFactory.WithTopic(topic),
					testutil.EventFactory.WithTime(eventTime),
				)
				delivery := testutil.AttemptFactory.AnyPointer(
					testutil.AttemptFactory.WithID(fmt.Sprintf("batch_del_%02d", i)),
					testutil.AttemptFactory.WithTenantID(tenantID),
					testutil.AttemptFactory.WithEventID(event.ID),
					testutil.AttemptFactory.WithDestinationID(destID),
					testutil.AttemptFactory.WithStatus(status),
					testutil.AttemptFactory.WithTime(eventTime.Add(time.Millisecond)),
				)

				entries = append(entries, &models.LogEntry{Event: event, Attempt: delivery})
				allDeliveries = append(allDeliveries, delivery)
				destinationEvents[destID] = append(destinationEvents[destID], event)
				topicEvents[topic] = append(topicEvents[topic], event)
				statusDeliveries[status] = append(statusDeliveries[status], delivery)
			}

			err := logStore.InsertMany(ctx, entries)
			require.NoError(t, err)
			require.NoError(t, h.FlushWrites(ctx))

			// Verify all inserted
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{tenantID},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			// 15 batch + 1 single = 16
			assert.GreaterOrEqual(t, len(response.Data), 15)
		})

		t.Run("empty batch is no-op", func(t *testing.T) {
			err := logStore.InsertMany(ctx, []*models.LogEntry{})
			require.NoError(t, err)
		})

		t.Run("nil metadata persists as empty", func(t *testing.T) {
			// An event published without metadata has a nil Metadata map.
			// Stores must persist it without error and round-trip it as an
			// empty (non-nil) map, regardless of backend. Postgres in
			// particular has `metadata jsonb NOT NULL`, so a nil map that
			// encodes to SQL NULL is rejected at insert time.
			nilTenantID := idgen.String()
			destID := idgen.Destination()

			event := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID("nil_meta_evt"),
				testutil.EventFactory.WithTenantID(nilTenantID),
				testutil.EventFactory.WithDestinationID(destID),
				testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
				testutil.EventFactory.WithMetadata(nil),
				testutil.EventFactory.WithTime(baseTime.Add(-8*time.Minute)),
			)
			delivery := testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID("nil_meta_del"),
				testutil.AttemptFactory.WithTenantID(nilTenantID),
				testutil.AttemptFactory.WithEventID(event.ID),
				testutil.AttemptFactory.WithDestinationID(destID),
				testutil.AttemptFactory.WithStatus("success"),
				testutil.AttemptFactory.WithTime(baseTime.Add(-8*time.Minute)),
			)

			err := logStore.InsertMany(ctx, []*models.LogEntry{{Event: event, Attempt: delivery}})
			require.NoError(t, err, "inserting an event with nil metadata must not error")
			require.NoError(t, h.FlushWrites(ctx))

			retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: nilTenantID,
				EventID:  event.ID,
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved)
			assert.NotNil(t, retrieved.Metadata, "nil metadata should round-trip as a non-nil empty map")
			assert.Empty(t, retrieved.Metadata)
		})

		t.Run("duplicate entries in batch", func(t *testing.T) {
			// Duplicates arise from MQ redelivery and producer re-publish;
			// InsertMany must tolerate intra-batch duplicates (same Attempt.ID)
			// without error and persist a single attempt row.
			// Isolated tenant so shared filter counts above stay intact.
			dupTenantID := idgen.String()
			destID := idgen.Destination()

			event := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID("dup_evt"),
				testutil.EventFactory.WithTenantID(dupTenantID),
				testutil.EventFactory.WithDestinationID(destID),
				testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
				testutil.EventFactory.WithTime(baseTime.Add(-10*time.Minute)),
			)
			delivery := testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID("dup_del"),
				testutil.AttemptFactory.WithTenantID(dupTenantID),
				testutil.AttemptFactory.WithEventID(event.ID),
				testutil.AttemptFactory.WithDestinationID(destID),
				testutil.AttemptFactory.WithStatus("failed"),
				testutil.AttemptFactory.WithTime(baseTime.Add(-10*time.Minute)),
			)
			otherEvent := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID("dup_evt_other"),
				testutil.EventFactory.WithTenantID(dupTenantID),
				testutil.EventFactory.WithDestinationID(destID),
				testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
				testutil.EventFactory.WithTime(baseTime.Add(-9*time.Minute)),
			)
			otherDelivery := testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID("dup_del_other"),
				testutil.AttemptFactory.WithTenantID(dupTenantID),
				testutil.AttemptFactory.WithEventID(otherEvent.ID),
				testutil.AttemptFactory.WithDestinationID(destID),
				testutil.AttemptFactory.WithStatus("success"),
				testutil.AttemptFactory.WithTime(baseTime.Add(-9*time.Minute)),
			)

			err := logStore.InsertMany(ctx, []*models.LogEntry{
				{Event: event, Attempt: delivery},
				{Event: otherEvent, Attempt: otherDelivery},
				{Event: event, Attempt: delivery}, // duplicate copy
			})
			require.NoError(t, err)
			require.NoError(t, h.FlushWrites(ctx))

			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{dupTenantID},
				Limit:      10,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2, "duplicate entry should persist a single attempt row")

			dupResponse, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{dupTenantID},
				EventIDs:   []string{event.ID},
				Limit:      10,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, dupResponse.Data, 1)
			assert.Equal(t, delivery.ID, dupResponse.Data[0].Attempt.ID)
		})
	})

	t.Run("list filters", func(t *testing.T) {
		t.Run("ListEvent by destination", func(t *testing.T) {
			destID := destinationIDs[0]
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantIDs:      []string{tenantID},
				DestinationIDs: []string{destID},
				Limit:          100,
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, len(destinationEvents[destID]))
			for _, event := range response.Data {
				assert.Contains(t, event.MatchedDestinationIDs, destID)
			}
		})

		t.Run("ListEvent by multiple destinations", func(t *testing.T) {
			destIDs := []string{destinationIDs[0], destinationIDs[1]}
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantIDs:      []string{tenantID},
				DestinationIDs: destIDs,
				Limit:          100,
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			expectedCount := len(destinationEvents[destIDs[0]]) + len(destinationEvents[destIDs[1]])
			require.Len(t, response.Data, expectedCount)
		})

		t.Run("ListEvent by topic", func(t *testing.T) {
			topic := testutil.TestTopics[0]
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantIDs:  []string{tenantID},
				Topics:     []string{topic},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, len(topicEvents[topic]))
		})

		t.Run("ListEvent by time range", func(t *testing.T) {
			eventStart := baseTime.Add(-5 * time.Minute)
			eventEnd := baseTime
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantIDs:  []string{tenantID},
				TimeFilter: driver.TimeFilter{GTE: &eventStart, LTE: &eventEnd},
				Limit:      100,
			})
			require.NoError(t, err)
			// Should include events within the 5-minute window
			assert.NotEmpty(t, response.Data)
			for _, evt := range response.Data {
				assert.True(t, !evt.Time.Before(eventStart) && !evt.Time.After(eventEnd))
			}
		})

		t.Run("ListAttempt by destination", func(t *testing.T) {
			destID := destinationIDs[0]
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:      []string{tenantID},
				DestinationIDs: []string{destID},
				Limit:          100,
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			for _, dr := range response.Data {
				assert.Equal(t, destID, dr.Attempt.DestinationID)
			}
		})

		t.Run("ListAttempt by status", func(t *testing.T) {
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{tenantID},
				Status:     "success",
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			for _, dr := range response.Data {
				assert.Equal(t, "success", dr.Attempt.Status)
			}
		})

		t.Run("ListAttempt by topic", func(t *testing.T) {
			topic := testutil.TestTopics[0]
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{tenantID},
				Topics:     []string{topic},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			for _, dr := range response.Data {
				assert.Equal(t, topic, dr.Event.Topic)
			}
		})

		t.Run("ListAttempt by event ID", func(t *testing.T) {
			eventID := "batch_evt_00"
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{tenantID},
				EventIDs:   []string{eventID},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.Equal(t, eventID, response.Data[0].Event.ID)
		})
	})

	t.Run("retrieve", func(t *testing.T) {
		// Use one of our batch events for retrieve tests
		knownEventID := "batch_evt_00"
		knownAttemptID := "batch_del_00"

		t.Run("RetrieveEvent existing", func(t *testing.T) {
			retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: tenantID,
				EventID:  knownEventID,
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved)
			assert.Equal(t, knownEventID, retrieved.ID)
			assert.Equal(t, tenantID, retrieved.TenantID)
		})

		t.Run("RetrieveEvent non-existent returns nil", func(t *testing.T) {
			retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: tenantID,
				EventID:  "non-existent-event",
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)
		})

		t.Run("RetrieveEvent wrong tenant returns nil", func(t *testing.T) {
			retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: "wrong-tenant",
				EventID:  knownEventID,
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)
		})

		t.Run("RetrieveAttempt existing", func(t *testing.T) {
			retrieved, err := logStore.RetrieveAttempt(ctx, driver.RetrieveAttemptRequest{
				TenantID:  tenantID,
				AttemptID: knownAttemptID,
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved)
			assert.Equal(t, knownAttemptID, retrieved.Attempt.ID)
		})

		t.Run("RetrieveAttempt non-existent returns nil", func(t *testing.T) {
			retrieved, err := logStore.RetrieveAttempt(ctx, driver.RetrieveAttemptRequest{
				TenantID:  tenantID,
				AttemptID: "non-existent-delivery",
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)
		})

		t.Run("RetrieveAttempt wrong tenant returns nil", func(t *testing.T) {
			retrieved, err := logStore.RetrieveAttempt(ctx, driver.RetrieveAttemptRequest{
				TenantID:  "wrong-tenant",
				AttemptID: knownAttemptID,
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)
		})
	})
}
