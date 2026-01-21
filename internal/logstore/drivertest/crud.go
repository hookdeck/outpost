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
	var allDeliveries []*models.Delivery
	destinationEvents := map[string][]*models.Event{}
	topicEvents := map[string][]*models.Event{}
	statusDeliveries := map[string][]*models.Delivery{}

	t.Run("insert and verify", func(t *testing.T) {
		t.Run("single delivery", func(t *testing.T) {
			destID := destinationIDs[0]
			topic := testutil.TestTopics[0]
			event := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID("single_evt"),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithDestinationID(destID),
				testutil.EventFactory.WithTopic(topic),
				testutil.EventFactory.WithTime(baseTime.Add(-30*time.Minute)),
			)
			delivery := testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID("single_del"),
				testutil.DeliveryFactory.WithTenantID(tenantID),
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destID),
				testutil.DeliveryFactory.WithStatus("success"),
				testutil.DeliveryFactory.WithTime(baseTime.Add(-30*time.Minute)),
			)

			err := logStore.InsertMany(ctx, []*models.Event{event}, []*models.Delivery{delivery})
			require.NoError(t, err)
			require.NoError(t, h.FlushWrites(ctx))

			// Track in maps for later filter tests
			destinationEvents[destID] = append(destinationEvents[destID], event)
			topicEvents[topic] = append(topicEvents[topic], event)
			statusDeliveries["success"] = append(statusDeliveries["success"], delivery)

			// Verify via List
			response, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				EventID:    event.ID,
				Limit:      10,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.Equal(t, event.ID, response.Data[0].Event.ID)
			assert.Equal(t, "success", response.Data[0].Delivery.Status)

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
			var events []*models.Event
			var deliveries []*models.Delivery

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
					testutil.EventFactory.WithTopic(topic),
					testutil.EventFactory.WithTime(eventTime),
				)
				delivery := testutil.DeliveryFactory.AnyPointer(
					testutil.DeliveryFactory.WithID(fmt.Sprintf("batch_del_%02d", i)),
					testutil.DeliveryFactory.WithTenantID(tenantID),
					testutil.DeliveryFactory.WithEventID(event.ID),
					testutil.DeliveryFactory.WithDestinationID(destID),
					testutil.DeliveryFactory.WithStatus(status),
					testutil.DeliveryFactory.WithTime(eventTime.Add(time.Millisecond)),
				)

				events = append(events, event)
				deliveries = append(deliveries, delivery)
				allDeliveries = append(allDeliveries, delivery)
				destinationEvents[destID] = append(destinationEvents[destID], event)
				topicEvents[topic] = append(topicEvents[topic], event)
				statusDeliveries[status] = append(statusDeliveries[status], delivery)
			}

			err := logStore.InsertMany(ctx, events, deliveries)
			require.NoError(t, err)
			require.NoError(t, h.FlushWrites(ctx))

			// Verify all inserted
			response, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			// 15 batch + 1 single = 16
			assert.GreaterOrEqual(t, len(response.Data), 15)
		})

		t.Run("empty batch is no-op", func(t *testing.T) {
			err := logStore.InsertMany(ctx, []*models.Event{}, []*models.Delivery{})
			require.NoError(t, err)
		})
	})

	t.Run("list filters", func(t *testing.T) {
		t.Run("ListEvent by destination", func(t *testing.T) {
			destID := destinationIDs[0]
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destID},
				Limit:          100,
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, len(destinationEvents[destID]))
			for _, event := range response.Data {
				assert.Equal(t, destID, event.DestinationID)
			}
		})

		t.Run("ListEvent by multiple destinations", func(t *testing.T) {
			destIDs := []string{destinationIDs[0], destinationIDs[1]}
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
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
				TenantID:   tenantID,
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
				TenantID:   tenantID,
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

		t.Run("ListDelivery by destination", func(t *testing.T) {
			destID := destinationIDs[0]
			response, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destID},
				Limit:          100,
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			for _, dr := range response.Data {
				assert.Equal(t, destID, dr.Delivery.DestinationID)
			}
		})

		t.Run("ListDelivery by status", func(t *testing.T) {
			response, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Status:     "success",
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			for _, dr := range response.Data {
				assert.Equal(t, "success", dr.Delivery.Status)
			}
		})

		t.Run("ListDelivery by topic", func(t *testing.T) {
			topic := testutil.TestTopics[0]
			response, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Topics:     []string{topic},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			for _, dr := range response.Data {
				assert.Equal(t, topic, dr.Event.Topic)
			}
		})

		t.Run("ListDelivery by event ID", func(t *testing.T) {
			eventID := "batch_evt_00"
			response, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				EventID:    eventID,
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
		knownDeliveryID := "batch_del_00"

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

		t.Run("RetrieveEvent with destination filter", func(t *testing.T) {
			retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID:      tenantID,
				EventID:       knownEventID,
				DestinationID: destinationIDs[0],
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved)
			assert.Equal(t, destinationIDs[0], retrieved.DestinationID)
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

		t.Run("RetrieveDelivery existing", func(t *testing.T) {
			retrieved, err := logStore.RetrieveDelivery(ctx, driver.RetrieveDeliveryRequest{
				TenantID:   tenantID,
				DeliveryID: knownDeliveryID,
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved)
			assert.Equal(t, knownDeliveryID, retrieved.Delivery.ID)
		})

		t.Run("RetrieveDelivery non-existent returns nil", func(t *testing.T) {
			retrieved, err := logStore.RetrieveDelivery(ctx, driver.RetrieveDeliveryRequest{
				TenantID:   tenantID,
				DeliveryID: "non-existent-delivery",
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)
		})

		t.Run("RetrieveDelivery wrong tenant returns nil", func(t *testing.T) {
			retrieved, err := logStore.RetrieveDelivery(ctx, driver.RetrieveDeliveryRequest{
				TenantID:   "wrong-tenant",
				DeliveryID: knownDeliveryID,
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)
		})
	})
}
