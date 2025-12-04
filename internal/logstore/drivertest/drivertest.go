package drivertest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/cursor"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Harness interface {
	MakeDriver(ctx context.Context) (driver.LogStore, error)
	// FlushWrites ensures all writes are fully persisted and visible.
	// For eventually consistent stores (e.g., ClickHouse ReplacingMergeTree),
	// this forces merge/compaction. For immediately consistent stores (e.g., PostgreSQL),
	// this is a no-op.
	FlushWrites(ctx context.Context) error
	Close()
}

type HarnessMaker func(ctx context.Context, t *testing.T) (Harness, error)

func RunConformanceTests(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("TestInsertManyDeliveryEvent", func(t *testing.T) {
		testInsertManyDeliveryEvent(t, newHarness)
	})
	t.Run("TestListDeliveryEvent", func(t *testing.T) {
		testListDeliveryEvent(t, newHarness)
	})
	t.Run("TestRetrieveEvent", func(t *testing.T) {
		testRetrieveEvent(t, newHarness)
	})
	t.Run("TestTenantIsolation", func(t *testing.T) {
		testTenantIsolation(t, newHarness)
	})
	t.Run("TestPaginationSimple", func(t *testing.T) {
		testPaginationSimple(t, newHarness)
	})
	t.Run("TestPaginationSuite", func(t *testing.T) {
		testPaginationSuite(t, newHarness)
	})
	t.Run("TestEdgeCases", func(t *testing.T) {
		testEdgeCases(t, newHarness)
	})
	t.Run("TestCursorValidation", func(t *testing.T) {
		testCursorValidation(t, newHarness)
	})
}

// testInsertManyDeliveryEvent tests the InsertManyDeliveryEvent method
func testInsertManyDeliveryEvent(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	startTime := time.Now().Add(-1 * time.Hour)

	t.Run("insert single delivery event", func(t *testing.T) {
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithStatus("success"),
		)
		de := &models.DeliveryEvent{
			ID:            idgen.String(),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		}

		err := logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{de})
		require.NoError(t, err)

		// Verify it was inserted
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventID:    event.ID,
			Limit:      10,
			EventStart: &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1)
		assert.Equal(t, event.ID, response.Data[0].Event.ID)
		assert.Equal(t, "success", response.Data[0].Delivery.Status)
	})

	t.Run("insert multiple delivery events", func(t *testing.T) {
		eventID := idgen.Event()
		baseDeliveryTime := time.Now().Truncate(time.Second)
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(eventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
		)

		// Insert multiple deliveries for the same event (simulating retries)
		deliveryEvents := []*models.DeliveryEvent{}
		for i := 0; i < 3; i++ {
			status := "failed"
			if i == 2 {
				status = "success"
			}
			delivery := testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%d", i)),
				testutil.DeliveryFactory.WithEventID(eventID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus(status),
				testutil.DeliveryFactory.WithTime(baseDeliveryTime.Add(time.Duration(i)*time.Second)),
			)
			deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
				ID:            fmt.Sprintf("de_%d", i),
				DestinationID: destinationID,
				Event:         *event,
				Delivery:      delivery,
			})
		}

		err := logStore.InsertManyDeliveryEvent(ctx, deliveryEvents)
		require.NoError(t, err)

		// Verify all were inserted
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventID:    eventID,
			Limit:      10,
			EventStart: &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 3)
	})

	t.Run("insert empty slice", func(t *testing.T) {
		err := logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{})
		require.NoError(t, err)
	})

	t.Run("duplicate insert is idempotent", func(t *testing.T) {
		// Create unique tenant to isolate this test
		idempotentTenantID := idgen.String()
		idempotentDestID := idgen.Destination()
		eventTime := time.Now().Add(-30 * time.Minute).Truncate(time.Second)
		deliveryTime := eventTime.Add(1 * time.Second)

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithTenantID(idempotentTenantID),
			testutil.EventFactory.WithDestinationID(idempotentDestID),
			testutil.EventFactory.WithTime(eventTime),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(idempotentDestID),
			testutil.DeliveryFactory.WithStatus("success"),
			testutil.DeliveryFactory.WithTime(deliveryTime),
		)
		de := &models.DeliveryEvent{
			ID:            idgen.String(),
			DestinationID: idempotentDestID,
			Event:         *event,
			Delivery:      delivery,
		}
		batch := []*models.DeliveryEvent{de}

		// First insert
		err := logStore.InsertManyDeliveryEvent(ctx, batch)
		require.NoError(t, err)

		// Second insert (duplicate) - should not error
		err = logStore.InsertManyDeliveryEvent(ctx, batch)
		require.NoError(t, err)

		// Third insert (duplicate) - should not error
		err = logStore.InsertManyDeliveryEvent(ctx, batch)
		require.NoError(t, err)

		// Flush writes to ensure deduplication is visible (for eventually consistent stores)
		err = h.FlushWrites(ctx)
		require.NoError(t, err)

		// Verify only 1 record exists (no duplicates)
		queryStart := eventTime.Add(-1 * time.Hour)
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   idempotentTenantID,
			Limit:      100,
			EventStart: &queryStart,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1, "duplicate inserts should not create multiple records")
		assert.Equal(t, event.ID, response.Data[0].Event.ID)
		assert.Equal(t, delivery.ID, response.Data[0].Delivery.ID)
	})
}

// testListDeliveryEvent tests the ListDeliveryEvent method with various filters and pagination
func testListDeliveryEvent(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationIDs := []string{
		idgen.Destination(),
		idgen.Destination(),
		idgen.Destination(),
	}

	// Track events by various dimensions for assertions
	destinationDeliveryEvents := map[string][]*models.DeliveryEvent{}
	statusDeliveryEvents := map[string][]*models.DeliveryEvent{}
	topicDeliveryEvents := map[string][]*models.DeliveryEvent{}
	timeDeliveryEvents := map[string][]*models.DeliveryEvent{} // "1h", "3h", "6h", "24h"
	allDeliveryEvents := []*models.DeliveryEvent{}

	// Use a fixed baseTime for deterministic tests
	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-48 * time.Hour) // before ALL events
	start := &startTime

	for i := 0; i < 20; i++ {
		destinationID := destinationIDs[i%len(destinationIDs)]
		topic := testutil.TestTopics[i%len(testutil.TestTopics)]
		shouldSucceed := i%2 == 0
		shouldRetry := i%3 == 0

		// Event times are distributed across time buckets:
		// i=0-4:   within last hour (1h bucket)
		// i=5-9:   2-3 hours ago (3h bucket)
		// i=10-14: 5-6 hours ago (6h bucket)
		// i=15-19: 23-24 hours ago (24h bucket)
		var eventTime time.Time
		switch {
		case i < 5:
			eventTime = baseTime.Add(-time.Duration(i) * time.Minute)
		case i < 10:
			eventTime = baseTime.Add(-time.Duration(2*60+i) * time.Minute)
		case i < 15:
			eventTime = baseTime.Add(-time.Duration(5*60+i) * time.Minute)
		default:
			eventTime = baseTime.Add(-time.Duration(23*60+i) * time.Minute)
		}

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("evt_%02d", i)),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithTime(eventTime),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithEligibleForRetry(shouldRetry),
			testutil.EventFactory.WithTopic(topic),
			testutil.EventFactory.WithMetadata(map[string]string{
				"index": strconv.Itoa(i),
			}),
		)

		// Delivery times are based on eventTime for consistency
		// Each delivery is slightly after the event, with retries having earlier deliveryTime than final
		deliveryTime := eventTime.Add(time.Duration(i) * time.Millisecond)

		if shouldRetry {
			initDelivery := testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%02d_init", i)),
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus("failed"),
				testutil.DeliveryFactory.WithTime(deliveryTime),
			)
			de := &models.DeliveryEvent{
				ID:            fmt.Sprintf("de_%02d_init", i),
				DestinationID: destinationID,
				Event:         *event,
				Delivery:      initDelivery,
			}
			allDeliveryEvents = append(allDeliveryEvents, de)
			destinationDeliveryEvents[destinationID] = append(destinationDeliveryEvents[destinationID], de)
			statusDeliveryEvents["failed"] = append(statusDeliveryEvents["failed"], de)
			topicDeliveryEvents[topic] = append(topicDeliveryEvents[topic], de)
			categorizeByTime(i, de, timeDeliveryEvents)

			deliveryTime = deliveryTime.Add(time.Millisecond) // Final delivery is later
		}

		var finalStatus string
		if shouldSucceed {
			finalStatus = "success"
		} else {
			finalStatus = "failed"
		}

		finalDelivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%02d_final", i)),
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithStatus(finalStatus),
			testutil.DeliveryFactory.WithTime(deliveryTime),
		)
		de := &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_%02d_final", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      finalDelivery,
		}
		allDeliveryEvents = append(allDeliveryEvents, de)
		destinationDeliveryEvents[destinationID] = append(destinationDeliveryEvents[destinationID], de)
		statusDeliveryEvents[finalStatus] = append(statusDeliveryEvents[finalStatus], de)
		topicDeliveryEvents[topic] = append(topicDeliveryEvents[topic], de)
		categorizeByTime(i, de, timeDeliveryEvents)
	}

	// Insert all delivery events
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, allDeliveryEvents))

	// Sort allDeliveryEvents by delivery_time DESC for ordering assertions
	// This is the expected order when querying
	sortedDeliveryEvents := make([]*models.DeliveryEvent, len(allDeliveryEvents))
	copy(sortedDeliveryEvents, allDeliveryEvents)
	sort.Slice(sortedDeliveryEvents, func(i, j int) bool {
		return sortedDeliveryEvents[i].Delivery.Time.After(sortedDeliveryEvents[j].Delivery.Time)
	})

	t.Run("empty result for unknown tenant", func(t *testing.T) {
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   "unknown",
			Limit:      5,
			EventStart: start,
		})
		require.NoError(t, err)
		assert.Empty(t, response.Data)
		assert.Empty(t, response.Next)
		assert.Empty(t, response.Prev)
	})

	t.Run("default ordering (delivery_time DESC)", func(t *testing.T) {
		// Verify default ordering is by delivery_time DESC
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			Limit:      10,
			EventStart: start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 10)

		// Verify ordering: should be sorted by delivery_time DESC
		for i, de := range response.Data {
			assert.Equal(t, sortedDeliveryEvents[i].Delivery.ID, de.Delivery.ID,
				"delivery ID mismatch at position %d", i)
		}

		// Verify first page has next cursor but no prev cursor
		assert.NotEmpty(t, response.Next, "should have next cursor with more data")
		assert.Empty(t, response.Prev, "first page should have no prev cursor")
	})

	t.Run("filter by destination", func(t *testing.T) {
		destID := destinationIDs[0]
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:       tenantID,
			DestinationIDs: []string{destID},
			Limit:          100,
			EventStart:     start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(destinationDeliveryEvents[destID]))
		for _, de := range response.Data {
			assert.Equal(t, destID, de.DestinationID)
		}
	})

	t.Run("filter by multiple destinations", func(t *testing.T) {
		destIDs := []string{destinationIDs[0], destinationIDs[1]}
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:       tenantID,
			DestinationIDs: destIDs,
			Limit:          100,
			EventStart:     start,
		})
		require.NoError(t, err)
		expectedCount := len(destinationDeliveryEvents[destIDs[0]]) + len(destinationDeliveryEvents[destIDs[1]])
		require.Len(t, response.Data, expectedCount)
		for _, de := range response.Data {
			assert.Contains(t, destIDs, de.DestinationID)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			Status:     "success",
			Limit:      100,
			EventStart: start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(statusDeliveryEvents["success"]))
		for _, de := range response.Data {
			assert.Equal(t, "success", de.Delivery.Status)
		}
	})

	t.Run("filter by single topic", func(t *testing.T) {
		topic := testutil.TestTopics[0]
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			Topics:     []string{topic},
			Limit:      100,
			EventStart: start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(topicDeliveryEvents[topic]))
		for _, de := range response.Data {
			assert.Equal(t, topic, de.Event.Topic)
		}
	})

	t.Run("filter by multiple topics", func(t *testing.T) {
		topics := []string{testutil.TestTopics[0], testutil.TestTopics[1]}
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			Topics:     topics,
			Limit:      100,
			EventStart: start,
		})
		require.NoError(t, err)
		expectedCount := len(topicDeliveryEvents[topics[0]]) + len(topicDeliveryEvents[topics[1]])
		require.Len(t, response.Data, expectedCount)
		for _, de := range response.Data {
			assert.Contains(t, topics, de.Event.Topic)
		}
	})

	t.Run("filter by event ID (replaces ListDelivery)", func(t *testing.T) {
		t.Run("returns empty for unknown event", func(t *testing.T) {
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:   tenantID,
				EventID:    "unknown-event",
				Limit:      100,
				EventStart: start,
			})
			require.NoError(t, err)
			assert.Empty(t, response.Data)
		})

		t.Run("returns all deliveries for event", func(t *testing.T) {
			eventID := "evt_00" // This event has retry (i%3==0), so 2 deliveries
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:   tenantID,
				EventID:    eventID,
				Limit:      100,
				EventStart: start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2, "evt_00 should have 2 deliveries (init failed + final)")
			for _, de := range response.Data {
				assert.Equal(t, eventID, de.Event.ID)
			}
		})

		t.Run("filter by event ID and destination", func(t *testing.T) {
			eventID := "evt_00"
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:       tenantID,
				EventID:        eventID,
				DestinationIDs: []string{destinationIDs[0]}, // evt_00 goes to destinationIDs[0]
				Limit:          100,
				EventStart:     start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for _, de := range response.Data {
				assert.Equal(t, eventID, de.Event.ID)
				assert.Equal(t, destinationIDs[0], de.DestinationID)
			}
		})
	})

	t.Run("time range filtering", func(t *testing.T) {
		sevenHoursAgo := baseTime.Add(-7 * time.Hour)
		fiveHoursAgo := baseTime.Add(-5 * time.Hour)
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &sevenHoursAgo,
			EventEnd:   &fiveHoursAgo,
			Limit:      100,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(timeDeliveryEvents["6h"]))
	})

	t.Run("combined filters", func(t *testing.T) {
		threeHoursAgo := baseTime.Add(-3 * time.Hour)
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:       tenantID,
			EventStart:     &threeHoursAgo,
			DestinationIDs: []string{destinationIDs[0]},
			Status:         "success",
			Topics:         []string{testutil.TestTopics[0]},
			Limit:          100,
		})
		require.NoError(t, err)
		for _, de := range response.Data {
			assert.Equal(t, destinationIDs[0], de.DestinationID)
			assert.Equal(t, "success", de.Delivery.Status)
			assert.Equal(t, testutil.TestTopics[0], de.Event.Topic)
			assert.True(t, de.Event.Time.After(threeHoursAgo))
		}
	})

	t.Run("verify all fields returned correctly", func(t *testing.T) {
		// Get first delivery event and verify all fields
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventID:    "evt_00", // Known event with specific data
			Limit:      1,
			EventStart: start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1)

		de := response.Data[0]

		// Event fields
		assert.Equal(t, "evt_00", de.Event.ID)
		assert.Equal(t, tenantID, de.Event.TenantID)
		assert.Equal(t, destinationIDs[0], de.Event.DestinationID) // i=0 -> destinationIDs[0%3]
		assert.Equal(t, testutil.TestTopics[0], de.Event.Topic)    // i=0 -> TestTopics[0%len]
		assert.Equal(t, true, de.Event.EligibleForRetry)           // i=0 -> 0%3==0 -> true
		assert.NotNil(t, de.Event.Metadata)
		assert.Equal(t, "0", de.Event.Metadata["index"])

		// Delivery fields
		assert.NotEmpty(t, de.Delivery.ID)
		assert.Equal(t, "evt_00", de.Delivery.EventID)
		assert.Equal(t, destinationIDs[0], de.Delivery.DestinationID)
		assert.Contains(t, []string{"success", "failed"}, de.Delivery.Status)
		assert.False(t, de.Delivery.Time.IsZero())
	})

	t.Run("limit edge cases", func(t *testing.T) {
		t.Run("limit 1 returns single item", func(t *testing.T) {
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:   tenantID,
				Limit:      1,
				EventStart: start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.NotEmpty(t, response.Next, "should have next cursor with more data")
		})

		t.Run("limit greater than total returns all", func(t *testing.T) {
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:   tenantID,
				Limit:      1000,
				EventStart: start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, len(allDeliveryEvents))
			assert.Empty(t, response.Next, "should have no next cursor when all data returned")
		})
	})

}

// testRetrieveEvent tests the RetrieveEvent method
func testRetrieveEvent(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := idgen.Event()
	eventTime := time.Now().Truncate(time.Millisecond)

	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithTime(eventTime),
		testutil.EventFactory.WithEligibleForRetry(true),
		testutil.EventFactory.WithMetadata(map[string]string{
			"source": "test",
			"env":    "development",
		}),
		testutil.EventFactory.WithData(map[string]interface{}{
			"user_id": "usr_123",
			"email":   "test@example.com",
		}),
	)

	delivery := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithEventID(eventID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithStatus("success"),
	)

	de := &models.DeliveryEvent{
		ID:            idgen.String(),
		DestinationID: destinationID,
		Event:         *event,
		Delivery:      delivery,
	}

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{de}))

	t.Run("retrieve existing event with all fields", func(t *testing.T) {
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: tenantID,
			EventID:  eventID,
		})
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// Verify all event fields
		assert.Equal(t, eventID, retrieved.ID)
		assert.Equal(t, tenantID, retrieved.TenantID)
		assert.Equal(t, destinationID, retrieved.DestinationID)
		assert.Equal(t, "user.created", retrieved.Topic)
		assert.Equal(t, true, retrieved.EligibleForRetry)
		assert.WithinDuration(t, eventTime, retrieved.Time, time.Second)
		assert.Equal(t, "test", retrieved.Metadata["source"])
		assert.Equal(t, "development", retrieved.Metadata["env"])
		assert.Equal(t, "usr_123", retrieved.Data["user_id"])
		assert.Equal(t, "test@example.com", retrieved.Data["email"])
	})

	t.Run("retrieve with destination filter", func(t *testing.T) {
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID:      tenantID,
			EventID:       eventID,
			DestinationID: destinationID,
		})
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.Equal(t, eventID, retrieved.ID)
		assert.Equal(t, destinationID, retrieved.DestinationID)
	})

	t.Run("retrieve non-existent event", func(t *testing.T) {
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: tenantID,
			EventID:  "non-existent",
		})
		require.NoError(t, err)
		assert.Nil(t, retrieved)
	})

	t.Run("retrieve with wrong tenant", func(t *testing.T) {
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: "wrong-tenant",
			EventID:  eventID,
		})
		require.NoError(t, err)
		assert.Nil(t, retrieved, "should not return event for wrong tenant")
	})

	t.Run("retrieve with wrong destination", func(t *testing.T) {
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID:      tenantID,
			EventID:       eventID,
			DestinationID: "wrong-destination",
		})
		require.NoError(t, err)
		assert.Nil(t, retrieved, "should not return event for wrong destination")
	})
}

// testTenantIsolation ensures data from one tenant cannot be accessed by another
func testTenantIsolation(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenant1ID := idgen.String()
	tenant2ID := idgen.String()
	destinationID := idgen.Destination()

	// Create events for tenant1
	event1 := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("tenant1-event"),
		testutil.EventFactory.WithTenantID(tenant1ID),
		testutil.EventFactory.WithDestinationID(destinationID),
	)
	delivery1 := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithEventID(event1.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
	)

	// Create events for tenant2
	event2 := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("tenant2-event"),
		testutil.EventFactory.WithTenantID(tenant2ID),
		testutil.EventFactory.WithDestinationID(destinationID),
	)
	delivery2 := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithEventID(event2.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
	)

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
		{ID: idgen.DeliveryEvent(), DestinationID: destinationID, Event: *event1, Delivery: delivery1},
		{ID: idgen.DeliveryEvent(), DestinationID: destinationID, Event: *event2, Delivery: delivery2},
	}))

	startTime := time.Now().Add(-1 * time.Hour)

	t.Run("ListDeliveryEvent isolates by tenant", func(t *testing.T) {
		// Tenant1 should only see their events
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenant1ID,
			Limit:      100,
			EventStart: &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1)
		assert.Equal(t, "tenant1-event", response.Data[0].Event.ID)

		// Tenant2 should only see their events
		response, err = logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenant2ID,
			Limit:      100,
			EventStart: &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1)
		assert.Equal(t, "tenant2-event", response.Data[0].Event.ID)
	})

	t.Run("RetrieveEvent isolates by tenant", func(t *testing.T) {
		// Tenant1 cannot access tenant2's event
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: tenant1ID,
			EventID:  "tenant2-event",
		})
		require.NoError(t, err)
		assert.Nil(t, retrieved)

		// Tenant2 cannot access tenant1's event
		retrieved, err = logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: tenant2ID,
			EventID:  "tenant1-event",
		})
		require.NoError(t, err)
		assert.Nil(t, retrieved)
	})
}

// Helper function to categorize delivery events by time bucket
func categorizeByTime(i int, de *models.DeliveryEvent, timeDeliveryEvents map[string][]*models.DeliveryEvent) {
	switch {
	case i < 5:
		timeDeliveryEvents["1h"] = append(timeDeliveryEvents["1h"], de)
	case i < 10:
		timeDeliveryEvents["3h"] = append(timeDeliveryEvents["3h"], de)
	case i < 15:
		timeDeliveryEvents["6h"] = append(timeDeliveryEvents["6h"], de)
	default:
		timeDeliveryEvents["24h"] = append(timeDeliveryEvents["24h"], de)
	}
}

// =============================================================================
// SIMPLE PAGINATION TEST
// =============================================================================
//
// A quick sanity check for pagination during development. Tests core mechanics
// with minimal data. Run the full TestPaginationSuite for comprehensive testing.
//
// Usage: make test TEST=./internal/logstore/memlogstore TESTARGS="-run TestPaginationSimple"
// =============================================================================

func testPaginationSimple(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Truncate(time.Second)

	// Create 5 delivery events with distinct times
	var allEvents []*models.DeliveryEvent
	for i := 0; i < 5; i++ {
		event := &models.Event{
			ID:            fmt.Sprintf("evt_%d", i),
			TenantID:      tenantID,
			DestinationID: destinationID,
			Topic:         "test.topic",
			Time:          baseTime.Add(-time.Duration(i) * time.Hour),
		}
		delivery := &models.Delivery{
			ID:            fmt.Sprintf("del_%d", i),
			EventID:       event.ID,
			DestinationID: destinationID,
			Status:        "success",
			Time:          baseTime.Add(-time.Duration(i) * time.Hour),
		}
		de := &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_%d", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		}
		allEvents = append(allEvents, de)
	}

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, allEvents))

	startTime := baseTime.Add(-48 * time.Hour)

	t.Run("forward pagination collects all items", func(t *testing.T) {
		var collected []string
		req := driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &startTime,
			Limit:      2,
		}

		response, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		for _, de := range response.Data {
			collected = append(collected, de.Delivery.ID)
		}

		for response.Next != "" {
			req.Next = response.Next
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
			for _, de := range response.Data {
				collected = append(collected, de.Delivery.ID)
			}
		}

		assert.Len(t, collected, 5, "should collect all 5 items")
		// Default sort is delivery_time DESC, so del_0 (most recent) comes first
		assert.Equal(t, "del_0", collected[0], "first item should be most recent")
		assert.Equal(t, "del_4", collected[4], "last item should be oldest")
	})

	t.Run("backward pagination returns to start", func(t *testing.T) {
		req := driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &startTime,
			Limit:      2,
		}

		// Get first page
		firstPage, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		require.NotEmpty(t, firstPage.Next, "should have next cursor")
		assert.Empty(t, firstPage.Prev, "first page should have no prev")

		// Go to second page
		req.Next = firstPage.Next
		secondPage, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		require.NotEmpty(t, secondPage.Prev, "second page should have prev")

		// Go back to first page
		req.Next = ""
		req.Prev = secondPage.Prev
		backToFirst, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		assert.Equal(t, firstPage.Data[0].Delivery.ID, backToFirst.Data[0].Delivery.ID,
			"returning to first page should show same data")
		assert.Empty(t, backToFirst.Prev, "first page should have no prev")
	})

	t.Run("sorting changes order", func(t *testing.T) {
		req := driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &startTime,
			SortOrder:  "asc",
			Limit:      5,
		}

		response, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		require.Len(t, response.Data, 5)

		// ASC order: oldest first
		assert.Equal(t, "del_4", response.Data[0].Delivery.ID, "ASC: oldest should be first")
		assert.Equal(t, "del_0", response.Data[4].Delivery.ID, "ASC: newest should be last")
	})

	t.Run("cursor stability", func(t *testing.T) {
		req := driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &startTime,
			Limit:      2,
		}

		// Same request twice should return identical results
		resp1, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		resp2, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		assert.Equal(t, resp1.Next, resp2.Next, "cursors should be stable")
		assert.Equal(t, len(resp1.Data), len(resp2.Data))
		for i := range resp1.Data {
			assert.Equal(t, resp1.Data[i].Delivery.ID, resp2.Data[i].Delivery.ID)
		}
	})
}

// =============================================================================
// PAGINATION TEST SUITE
// =============================================================================
//
// This suite tests pagination behavior with various filters and sort options.
// It uses dedicated test data with realistic timestamps to verify:
// - Forward and backward traversal
// - Cursor stability
// - Different sort orders (event_time vs delivery_time)
// - Time-based filtering (EventStart/End, DeliveryStart/End)
//
// SORTING LOGIC
// =============
//
// Sorting uses multi-column ordering to ensure deterministic pagination.
// This is critical because:
// 1. Multiple deliveries can have the same event_time (same event, multiple attempts)
// 2. In rare cases, deliveries could have identical timestamps
// 3. Cursor-based pagination requires stable, repeatable ordering
//
// The sorting columns are:
//
// | SortBy        | SortOrder | SQL Equivalent                                      |
// |---------------|-----------|-----------------------------------------------------|
// | delivery_time | desc      | ORDER BY delivery_time DESC, delivery_id DESC      |
// | delivery_time | asc       | ORDER BY delivery_time ASC, delivery_id ASC        |
// | event_time    | desc      | ORDER BY event_time DESC, event_id DESC, delivery_time DESC |
// | event_time    | asc       | ORDER BY event_time ASC, event_id ASC, delivery_time ASC   |
//
// Why these columns?
//
// For delivery_time sorting:
// - Primary: delivery_time - the user's requested sort
// - Secondary: delivery_id - tiebreaker for identical timestamps (rare but possible)
//
// For event_time sorting:
// - Primary: event_time - the user's requested sort
// - Secondary: event_id - groups all deliveries for the same event together
// - Tertiary: delivery_time - orders retries within an event chronologically
//
// The secondary/tertiary columns always use the same direction (ASC/DESC) as the
// primary column to maintain consistent ordering semantics.
//
// TEST DATA STRUCTURE
// ===================
//
// We create 10 events spread over 24 hours, each with 1-5 delivery attempts.
// This models realistic webhook delivery with retries.
//
// Timeline (times relative to baseTime, which is "now"):
//
//   Event 0: event_time = -1h
//     └── del_0_0: delivery_time = -55m (success)
//         └── 1 delivery, immediate success
//
//   Event 1: event_time = -2h
//     ├── del_1_0: delivery_time = -1h55m (failed)
//     └── del_1_1: delivery_time = -1h50m (success)
//         └── 2 deliveries, 1 retry
//
//   Event 2: event_time = -3h
//     ├── del_2_0: delivery_time = -2h55m (failed)
//     ├── del_2_1: delivery_time = -2h50m (failed)
//     └── del_2_2: delivery_time = -2h45m (success)
//         └── 3 deliveries, 2 retries
//
//   Event 3: event_time = -5h
//     ├── del_3_0: delivery_time = -4h55m (failed)
//     ├── del_3_1: delivery_time = -4h50m (failed)
//     ├── del_3_2: delivery_time = -4h45m (failed)
//     └── del_3_3: delivery_time = -4h40m (success)
//         └── 4 deliveries, 3 retries
//
//   Event 4: event_time = -6h
//     ├── del_4_0: delivery_time = -5h55m (failed)
//     ├── del_4_1: delivery_time = -5h50m (failed)
//     ├── del_4_2: delivery_time = -5h45m (failed)
//     ├── del_4_3: delivery_time = -5h40m (failed)
//     └── del_4_4: delivery_time = -5h35m (success)
//         └── 5 deliveries, 4 retries
//
//   Event 5: event_time = -8h
//     ├── del_5_0: delivery_time = -7h55m (failed)
//     └── del_5_1: delivery_time = -7h50m (success)
//         └── 2 deliveries, 1 retry
//
//   Event 6: event_time = -12h
//     ├── del_6_0: delivery_time = -11h55m (failed)
//     ├── del_6_1: delivery_time = -11h50m (failed)
//     └── del_6_2: delivery_time = -11h45m (success)
//         └── 3 deliveries, 2 retries
//
//   Event 7: event_time = -18h
//     └── del_7_0: delivery_time = -17h55m (success)
//         └── 1 delivery, immediate success
//
//   Event 8: event_time = -20h
//     ├── del_8_0: delivery_time = -19h55m (failed)
//     └── del_8_1: delivery_time = -19h50m (success)
//         └── 2 deliveries, 1 retry
//
//   Event 9: event_time = -23h
//     ├── del_9_0: delivery_time = -22h55m (failed)
//     ├── del_9_1: delivery_time = -22h50m (failed)
//     ├── del_9_2: delivery_time = -22h45m (failed)
//     └── del_9_3: delivery_time = -22h40m (success)
//         └── 4 deliveries, 3 retries
//
// NAMING CONVENTION
// =================
//
// - Event ID: evt_{event_index}
//   Example: evt_3 is the 4th event (0-indexed)
//
// - Delivery ID: del_{event_index}_{delivery_index}
//   Example: del_3_2 is event 3's 3rd delivery attempt (0-indexed)
//
// This makes it easy to understand relationships when debugging:
// - del_3_2 → belongs to evt_3, is the 3rd attempt
//
// TOTALS
// ======
//
// - 10 events
// - 27 delivery events total (1+2+3+4+5+2+3+1+2+4)
// - Event times span: -1h to -23h
// - Delivery times span: -55m to -22h40m
//
// =============================================================================

// =============================================================================
// PAGINATION SUITE TYPES AND HELPERS
// =============================================================================

// PaginationTestCase defines a single filter/sort combination to test
type PaginationTestCase struct {
	Name     string
	Request  driver.ListDeliveryEventRequest // Base request (TenantID will be set by suite)
	Expected []*models.DeliveryEvent         // Expected results in exact order
}

// PaginationSuiteData contains everything needed to run the pagination suite
type PaginationSuiteData struct {
	// Name describes this test data set (e.g., "realistic_timestamps", "identical_timestamps")
	Name string

	// TenantID for all test data
	TenantID string

	// TestCases are the filter/sort combinations to test
	// Each test case runs through all pagination behaviors (forward, backward, zigzag, etc.)
	TestCases []PaginationTestCase
}

// PaginationDataGenerator creates test data and returns the suite configuration
// It receives the logStore to insert data and returns the suite data for verification
type PaginationDataGenerator func(t *testing.T, logStore driver.LogStore) *PaginationSuiteData

// Sorting helper functions for multi-column sorting (see SORTING LOGIC above)
// These are used by generators to compute expected results.

func compareByDeliveryTime(a, b *models.DeliveryEvent, desc bool) bool {
	// Primary: delivery_time
	if !a.Delivery.Time.Equal(b.Delivery.Time) {
		if desc {
			return a.Delivery.Time.After(b.Delivery.Time)
		}
		return a.Delivery.Time.Before(b.Delivery.Time)
	}
	// Secondary: delivery_id
	if desc {
		return a.Delivery.ID > b.Delivery.ID
	}
	return a.Delivery.ID < b.Delivery.ID
}

func compareByEventTime(a, b *models.DeliveryEvent, desc bool) bool {
	// Primary: event_time
	if !a.Event.Time.Equal(b.Event.Time) {
		if desc {
			return a.Event.Time.After(b.Event.Time)
		}
		return a.Event.Time.Before(b.Event.Time)
	}
	// Secondary: event_id
	if a.Event.ID != b.Event.ID {
		if desc {
			return a.Event.ID > b.Event.ID
		}
		return a.Event.ID < b.Event.ID
	}
	// Tertiary: delivery_time
	if !a.Delivery.Time.Equal(b.Delivery.Time) {
		if desc {
			return a.Delivery.Time.After(b.Delivery.Time)
		}
		return a.Delivery.Time.Before(b.Delivery.Time)
	}
	// Quaternary: delivery_id (for deterministic ordering when all above are equal)
	if desc {
		return a.Delivery.ID > b.Delivery.ID
	}
	return a.Delivery.ID < b.Delivery.ID
}

func sortDeliveryEvents(events []*models.DeliveryEvent, sortBy string, desc bool) []*models.DeliveryEvent {
	result := make([]*models.DeliveryEvent, len(events))
	copy(result, events)
	sort.Slice(result, func(i, j int) bool {
		if sortBy == "event_time" {
			return compareByEventTime(result[i], result[j], desc)
		}
		return compareByDeliveryTime(result[i], result[j], desc)
	})
	return result
}

// =============================================================================
// DATA GENERATORS
// =============================================================================

// generateRealisticTimestampData creates test data with varied, realistic timestamps.
// This is the primary test data set that exercises all filters and sort options.
//
// See TEST DATA STRUCTURE documentation above for details on the data layout.
func generateRealisticTimestampData(t *testing.T, logStore driver.LogStore) *PaginationSuiteData {
	t.Helper()

	ctx := context.Background()
	tenantID := idgen.String()
	destinationIDs := []string{
		idgen.Destination(),
		idgen.Destination(),
	}
	baseTime := time.Now().Truncate(time.Second)

	// Event configuration: [event_index] = {hours_ago, num_deliveries}
	eventConfigs := []struct {
		hoursAgo      int
		numDeliveries int
	}{
		{1, 1},  // evt_0: 1 delivery
		{2, 2},  // evt_1: 2 deliveries
		{3, 3},  // evt_2: 3 deliveries
		{5, 4},  // evt_3: 4 deliveries
		{6, 5},  // evt_4: 5 deliveries
		{8, 2},  // evt_5: 2 deliveries
		{12, 3}, // evt_6: 3 deliveries
		{18, 1}, // evt_7: 1 delivery
		{20, 2}, // evt_8: 2 deliveries
		{23, 4}, // evt_9: 4 deliveries
	}

	var allDeliveryEvents []*models.DeliveryEvent
	byDestination := make(map[string][]*models.DeliveryEvent)
	byStatus := make(map[string][]*models.DeliveryEvent)

	for eventIdx, cfg := range eventConfigs {
		eventTime := baseTime.Add(-time.Duration(cfg.hoursAgo) * time.Hour)
		destinationID := destinationIDs[eventIdx%len(destinationIDs)]
		topic := testutil.TestTopics[eventIdx%len(testutil.TestTopics)]

		event := &models.Event{
			ID:               fmt.Sprintf("evt_%d", eventIdx),
			TenantID:         tenantID,
			DestinationID:    destinationID,
			Topic:            topic,
			EligibleForRetry: cfg.numDeliveries > 1,
			Time:             eventTime,
			Metadata:         map[string]string{"event_index": strconv.Itoa(eventIdx)},
			Data:             map[string]interface{}{"test": true},
		}

		// Create deliveries: first delivery is 5 minutes after event,
		// subsequent deliveries are 5 minutes apart
		for delIdx := 0; delIdx < cfg.numDeliveries; delIdx++ {
			deliveryTime := eventTime.Add(5*time.Minute + time.Duration(delIdx)*5*time.Minute)

			// Last delivery succeeds, others fail
			status := "failed"
			if delIdx == cfg.numDeliveries-1 {
				status = "success"
			}

			delivery := &models.Delivery{
				ID:            fmt.Sprintf("del_%d_%d", eventIdx, delIdx),
				EventID:       event.ID,
				DestinationID: destinationID,
				Status:        status,
				Time:          deliveryTime,
				Code:          "200",
			}

			de := &models.DeliveryEvent{
				ID:            fmt.Sprintf("de_%d_%d", eventIdx, delIdx),
				DestinationID: destinationID,
				Event:         *event,
				Delivery:      delivery,
			}

			allDeliveryEvents = append(allDeliveryEvents, de)
			byDestination[destinationID] = append(byDestination[destinationID], de)
			byStatus[status] = append(byStatus[status], de)
		}
	}

	// Insert all delivery events
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, allDeliveryEvents))

	// Pre-compute sorted lists
	sortedByDeliveryTimeDesc := sortDeliveryEvents(allDeliveryEvents, "delivery_time", true)
	sortedByDeliveryTimeAsc := sortDeliveryEvents(allDeliveryEvents, "delivery_time", false)
	sortedByEventTimeDesc := sortDeliveryEvents(allDeliveryEvents, "event_time", true)
	sortedByEventTimeAsc := sortDeliveryEvents(allDeliveryEvents, "event_time", false)

	// Pre-compute filtered subsets
	sixHoursAgo := baseTime.Add(-6 * time.Hour)
	twelveHoursAgo := baseTime.Add(-12 * time.Hour)
	farPast := baseTime.Add(-48 * time.Hour)

	var eventsInLast6Hours []*models.DeliveryEvent
	var eventsFrom6hTo12h []*models.DeliveryEvent
	var deliveriesInLast6Hours []*models.DeliveryEvent
	var deliveriesFrom6hTo12h []*models.DeliveryEvent

	for _, de := range sortedByDeliveryTimeDesc {
		// Filter by event time (inclusive semantics)
		if !de.Event.Time.Before(sixHoursAgo) {
			eventsInLast6Hours = append(eventsInLast6Hours, de)
		}
		if !de.Event.Time.Before(twelveHoursAgo) && !de.Event.Time.After(sixHoursAgo) {
			eventsFrom6hTo12h = append(eventsFrom6hTo12h, de)
		}

		// Filter by delivery time (inclusive semantics)
		if !de.Delivery.Time.Before(sixHoursAgo) {
			deliveriesInLast6Hours = append(deliveriesInLast6Hours, de)
		}
		if !de.Delivery.Time.Before(twelveHoursAgo) && !de.Delivery.Time.After(sixHoursAgo) {
			deliveriesFrom6hTo12h = append(deliveriesFrom6hTo12h, de)
		}
	}

	// Sort byDestination and byStatus
	for destID := range byDestination {
		byDestination[destID] = sortDeliveryEvents(byDestination[destID], "delivery_time", true)
	}
	for status := range byStatus {
		byStatus[status] = sortDeliveryEvents(byStatus[status], "delivery_time", true)
	}
	successSortedByEventTime := sortDeliveryEvents(byStatus["success"], "event_time", true)

	// Build test cases
	return &PaginationSuiteData{
		Name:     "realistic_timestamps",
		TenantID: tenantID,
		TestCases: []PaginationTestCase{
			{
				Name:     "default sort (delivery_time DESC)",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast},
				Expected: sortedByDeliveryTimeDesc,
			},
			{
				Name:     "sort by event_time DESC",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast, SortBy: "event_time"},
				Expected: sortedByEventTimeDesc,
			},
			{
				Name:     "sort by delivery_time ASC",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast, SortOrder: "asc"},
				Expected: sortedByDeliveryTimeAsc,
			},
			{
				Name:     "sort by event_time ASC",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast, SortBy: "event_time", SortOrder: "asc"},
				Expected: sortedByEventTimeAsc,
			},
			{
				Name:     "filter by event time (last 6 hours)",
				Request:  driver.ListDeliveryEventRequest{EventStart: &sixHoursAgo},
				Expected: eventsInLast6Hours,
			},
			{
				Name:     "filter by event time range (6h to 12h ago)",
				Request:  driver.ListDeliveryEventRequest{EventStart: &twelveHoursAgo, EventEnd: &sixHoursAgo},
				Expected: eventsFrom6hTo12h,
			},
			{
				Name:     "filter by delivery time (last 6 hours)",
				Request:  driver.ListDeliveryEventRequest{DeliveryStart: &sixHoursAgo, EventStart: &farPast},
				Expected: deliveriesInLast6Hours,
			},
			{
				Name:     "filter by delivery time range (6h to 12h ago)",
				Request:  driver.ListDeliveryEventRequest{DeliveryStart: &twelveHoursAgo, DeliveryEnd: &sixHoursAgo, EventStart: &farPast},
				Expected: deliveriesFrom6hTo12h,
			},
			{
				Name:     "filter by destination",
				Request:  driver.ListDeliveryEventRequest{DestinationIDs: []string{destinationIDs[0]}, EventStart: &farPast},
				Expected: byDestination[destinationIDs[0]],
			},
			{
				Name:     "filter by status (success)",
				Request:  driver.ListDeliveryEventRequest{Status: "success", EventStart: &farPast},
				Expected: byStatus["success"],
			},
			{
				Name:     "filter by status with event_time sort",
				Request:  driver.ListDeliveryEventRequest{Status: "success", SortBy: "event_time", EventStart: &farPast},
				Expected: successSortedByEventTime,
			},
		},
	}
}

// generateIdenticalTimestampData creates test data where all events and deliveries
// have the SAME timestamp. This forces sorting to rely entirely on secondary/tertiary
// columns (event_id, delivery_id) and verifies deterministic ordering.
//
// This is a critical edge case for cursor-based pagination stability.
func generateIdenticalTimestampData(t *testing.T, logStore driver.LogStore) *PaginationSuiteData {
	t.Helper()

	ctx := context.Background()
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// All events and deliveries share the SAME timestamp
	sameTime := time.Now().Truncate(time.Second)
	farPast := sameTime.Add(-1 * time.Hour)

	// Create 10 events, each with 2 deliveries, all at the same timestamp
	var allDeliveryEvents []*models.DeliveryEvent

	for eventIdx := 0; eventIdx < 10; eventIdx++ {
		event := &models.Event{
			ID:               fmt.Sprintf("evt_%02d", eventIdx),
			TenantID:         tenantID,
			DestinationID:    destinationID,
			Topic:            "test.topic",
			EligibleForRetry: true,
			Time:             sameTime,
			Metadata:         map[string]string{},
			Data:             map[string]interface{}{},
		}

		for delIdx := 0; delIdx < 2; delIdx++ {
			status := "failed"
			if delIdx == 1 {
				status = "success"
			}

			delivery := &models.Delivery{
				ID:            fmt.Sprintf("del_%02d_%d", eventIdx, delIdx),
				EventID:       event.ID,
				DestinationID: destinationID,
				Status:        status,
				Time:          sameTime,
				Code:          "200",
			}

			de := &models.DeliveryEvent{
				ID:            fmt.Sprintf("de_%02d_%d", eventIdx, delIdx),
				DestinationID: destinationID,
				Event:         *event,
				Delivery:      delivery,
			}

			allDeliveryEvents = append(allDeliveryEvents, de)
		}
	}

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, allDeliveryEvents))

	// With identical timestamps, sorting falls back to ID columns
	sortedByDeliveryTimeDesc := sortDeliveryEvents(allDeliveryEvents, "delivery_time", true)
	sortedByDeliveryTimeAsc := sortDeliveryEvents(allDeliveryEvents, "delivery_time", false)
	sortedByEventTimeDesc := sortDeliveryEvents(allDeliveryEvents, "event_time", true)
	sortedByEventTimeAsc := sortDeliveryEvents(allDeliveryEvents, "event_time", false)

	return &PaginationSuiteData{
		Name:     "identical_timestamps",
		TenantID: tenantID,
		TestCases: []PaginationTestCase{
			{
				Name:     "delivery_time DESC (falls back to delivery_id)",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast},
				Expected: sortedByDeliveryTimeDesc,
			},
			{
				Name:     "delivery_time ASC (falls back to delivery_id)",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast, SortOrder: "asc"},
				Expected: sortedByDeliveryTimeAsc,
			},
			{
				Name:     "event_time DESC (falls back to event_id)",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast, SortBy: "event_time"},
				Expected: sortedByEventTimeDesc,
			},
			{
				Name:     "event_time ASC (falls back to event_id)",
				Request:  driver.ListDeliveryEventRequest{EventStart: &farPast, SortBy: "event_time", SortOrder: "asc"},
				Expected: sortedByEventTimeAsc,
			},
		},
	}
}

// testPaginationSuite runs the pagination test suite with multiple data generators.
// Each generator creates a different test data set (e.g., realistic timestamps,
// identical timestamps) and the suite runs all pagination behaviors against each.
func testPaginationSuite(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	// List of data generators to run
	generators := []PaginationDataGenerator{
		generateRealisticTimestampData,
		generateIdenticalTimestampData,
	}

	for _, generator := range generators {
		generator := generator // capture range variable

		// Each generator gets its own harness/logstore for isolation
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)

		logStore, err := h.MakeDriver(ctx)
		require.NoError(t, err)

		// Generate test data
		suiteData := generator(t, logStore)

		t.Run(suiteData.Name, func(t *testing.T) {
			for _, tc := range suiteData.TestCases {
				tc := tc // capture range variable
				t.Run(tc.Name, func(t *testing.T) {
					// Set tenant ID
					tc.Request.TenantID = suiteData.TenantID

					// Skip if no expected data
					if len(tc.Expected) == 0 {
						t.Skip("No expected data for this test case")
					}

					runPaginationTests(t, logStore, tc)
				})
			}
		})

		h.Close()
	}
}

// runPaginationTests runs all pagination behavior tests for a single test case
func runPaginationTests(t *testing.T, logStore driver.LogStore, tc PaginationTestCase) {
	t.Helper()
	ctx := context.Background()

	// Use a page size that creates multiple pages but not too many
	pageSize := 5
	if len(tc.Expected) <= pageSize {
		pageSize = 2 // Ensure at least 2 pages for small datasets
	}
	if len(tc.Expected) <= 2 {
		pageSize = 1
	}

	t.Run("forward traversal", func(t *testing.T) {
		var collected []*models.DeliveryEvent
		req := tc.Request
		req.Limit = pageSize

		response, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		collected = append(collected, response.Data...)

		for response.Next != "" {
			req.Next = response.Next
			req.Prev = ""
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
			collected = append(collected, response.Data...)
		}

		require.Len(t, collected, len(tc.Expected), "forward traversal should collect all items")
		for i, de := range collected {
			assert.Equal(t, tc.Expected[i].Delivery.ID, de.Delivery.ID,
				"forward traversal: mismatch at position %d", i)
		}
	})

	t.Run("backward traversal", func(t *testing.T) {
		// First go to the last page
		req := tc.Request
		req.Limit = pageSize

		response, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		for response.Next != "" {
			req.Next = response.Next
			req.Prev = ""
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
		}

		// Now traverse backward
		var collected []*models.DeliveryEvent
		collected = append(collected, response.Data...)

		for response.Prev != "" {
			req.Prev = response.Prev
			req.Next = ""
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
			collected = append(response.Data, collected...) // Prepend
		}

		require.Len(t, collected, len(tc.Expected), "backward traversal should collect all items")
		for i, de := range collected {
			assert.Equal(t, tc.Expected[i].Delivery.ID, de.Delivery.ID,
				"backward traversal: mismatch at position %d", i)
		}
	})

	t.Run("forward then backward", func(t *testing.T) {
		req := tc.Request
		req.Limit = pageSize

		// First page
		firstPage, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		if firstPage.Next == "" {
			t.Skip("Only one page, cannot test forward then backward")
		}

		// Second page
		req.Next = firstPage.Next
		secondPage, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		// Back to first page
		req.Next = ""
		req.Prev = secondPage.Prev
		backToFirst, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		// Verify we got the same first page
		require.Len(t, backToFirst.Data, len(firstPage.Data))
		for i, de := range backToFirst.Data {
			assert.Equal(t, firstPage.Data[i].Delivery.ID, de.Delivery.ID,
				"back to first page: mismatch at position %d", i)
		}
		assert.Empty(t, backToFirst.Prev, "first page should have no prev cursor")
	})

	t.Run("zigzag navigation", func(t *testing.T) {
		req := tc.Request
		req.Limit = pageSize

		// First page
		page1, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		if page1.Next == "" {
			t.Skip("Only one page, cannot test zigzag")
		}

		// Second page
		req.Next = page1.Next
		page2, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		// Back to first
		req.Next = ""
		req.Prev = page2.Prev
		page1Again, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		// Forward to second again
		req.Prev = ""
		req.Next = page1Again.Next
		page2Again, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		// Verify page 2 is consistent
		require.Len(t, page2Again.Data, len(page2.Data))
		for i, de := range page2Again.Data {
			assert.Equal(t, page2.Data[i].Delivery.ID, de.Delivery.ID,
				"zigzag page 2: mismatch at position %d", i)
		}

		// If there's a third page, go there
		if page2Again.Next != "" {
			req.Prev = ""
			req.Next = page2Again.Next
			page3, err := logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
			assert.NotEmpty(t, page3.Prev, "page 3 should have prev cursor")
		}
	})

	t.Run("cursor stability", func(t *testing.T) {
		req := tc.Request
		req.Limit = pageSize

		// Get first page twice
		first1, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		first2, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		// Same data
		require.Len(t, first1.Data, len(first2.Data))
		for i := range first1.Data {
			assert.Equal(t, first1.Data[i].Delivery.ID, first2.Data[i].Delivery.ID)
		}

		// Same cursors
		assert.Equal(t, first1.Next, first2.Next)
		assert.Equal(t, first1.Prev, first2.Prev)
	})

	t.Run("boundary conditions", func(t *testing.T) {
		req := tc.Request
		req.Limit = pageSize

		// First page
		firstPage, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		assert.Empty(t, firstPage.Prev, "first page should have no prev cursor")

		if len(tc.Expected) > pageSize {
			assert.NotEmpty(t, firstPage.Next, "first page should have next cursor when more data exists")
		}

		// Go to last page
		response := firstPage
		for response.Next != "" {
			req.Next = response.Next
			req.Prev = ""
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
		}

		assert.Empty(t, response.Next, "last page should have no next cursor")
		if len(tc.Expected) > pageSize {
			assert.NotEmpty(t, response.Prev, "last page should have prev cursor when more data exists")
		}
	})

	t.Run("single item pages", func(t *testing.T) {
		req := tc.Request
		req.Limit = 1

		var collected []*models.DeliveryEvent
		response, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		require.Len(t, response.Data, 1, "limit 1 should return exactly 1 item")
		collected = append(collected, response.Data...)

		for response.Next != "" {
			req.Next = response.Next
			req.Prev = ""
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
			require.LessOrEqual(t, len(response.Data), 1, "limit 1 should return at most 1 item")
			collected = append(collected, response.Data...)
		}

		require.Len(t, collected, len(tc.Expected), "single item pagination should collect all items")
	})
}

// =============================================================================
// EDGE CASES TEST SUITE
// =============================================================================
//
// This suite tests edge cases and boundary conditions that implementations
// must handle correctly:
// - Invalid/unknown sort values (should use defaults)
// - Empty vs nil filter semantics (should be equivalent)
// - Time boundary precision (inclusive semantics)
// - EventID filtering with pagination
// - Data immutability (returned data shouldn't affect stored data)
// =============================================================================

func testEdgeCases(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("invalid sort values use defaults", func(t *testing.T) {
		testInvalidSortValues(t, newHarness)
	})
	t.Run("empty vs nil filter semantics", func(t *testing.T) {
		testEmptyVsNilFilters(t, newHarness)
	})
	t.Run("time boundary precision", func(t *testing.T) {
		testTimeBoundaryPrecision(t, newHarness)
	})
	t.Run("eventID filtering with pagination", func(t *testing.T) {
		testEventIDPagination(t, newHarness)
	})
	t.Run("data immutability", func(t *testing.T) {
		testDataImmutability(t, newHarness)
	})
}

// testInvalidSortValues verifies that invalid sort values fall back to defaults
// rather than causing errors. This is important for API robustness.
func testInvalidSortValues(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Truncate(time.Second)

	// Insert test data with distinct delivery times
	var deliveryEvents []*models.DeliveryEvent
	for i := 0; i < 3; i++ {
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("evt_%d", i)),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%d", i)),
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_%d", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		})
	}
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

	startTime := baseTime.Add(-48 * time.Hour)

	t.Run("invalid SortBy uses default (delivery_time)", func(t *testing.T) {
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			SortBy:     "invalid_column",
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 3)

		// Should be sorted by delivery_time DESC (default)
		// del_0 is most recent, del_2 is oldest
		assert.Equal(t, "del_0", response.Data[0].Delivery.ID)
		assert.Equal(t, "del_1", response.Data[1].Delivery.ID)
		assert.Equal(t, "del_2", response.Data[2].Delivery.ID)
	})

	t.Run("invalid SortOrder uses default (desc)", func(t *testing.T) {
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			SortOrder:  "sideways",
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 3)

		// Should be sorted DESC (default)
		assert.Equal(t, "del_0", response.Data[0].Delivery.ID)
		assert.Equal(t, "del_2", response.Data[2].Delivery.ID)
	})

	t.Run("empty SortBy uses default", func(t *testing.T) {
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			SortBy:     "",
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 3)
		assert.Equal(t, "del_0", response.Data[0].Delivery.ID)
	})
}

// testEmptyVsNilFilters verifies that empty slices and nil slices are treated
// equivalently (both mean "no filter").
func testEmptyVsNilFilters(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	startTime := time.Now().Add(-1 * time.Hour)

	// Insert test data
	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("test.topic"),
	)
	delivery := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithEventID(event.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
	)
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
		{ID: idgen.DeliveryEvent(), DestinationID: destinationID, Event: *event, Delivery: delivery},
	}))

	t.Run("nil DestinationIDs equals empty DestinationIDs", func(t *testing.T) {
		responseNil, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:       tenantID,
			DestinationIDs: nil,
			EventStart:     &startTime,
			Limit:          10,
		})
		require.NoError(t, err)

		responseEmpty, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:       tenantID,
			DestinationIDs: []string{},
			EventStart:     &startTime,
			Limit:          10,
		})
		require.NoError(t, err)

		assert.Equal(t, len(responseNil.Data), len(responseEmpty.Data),
			"nil and empty DestinationIDs should return same count")
		assert.Equal(t, 1, len(responseNil.Data), "should return all data when no filter")
	})

	t.Run("nil Topics equals empty Topics", func(t *testing.T) {
		responseNil, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			Topics:     nil,
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)

		responseEmpty, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			Topics:     []string{},
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)

		assert.Equal(t, len(responseNil.Data), len(responseEmpty.Data),
			"nil and empty Topics should return same count")
	})

	t.Run("empty Status equals no status filter", func(t *testing.T) {
		responseEmpty, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			Status:     "",
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)

		responseNoFilter, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)

		assert.Equal(t, len(responseEmpty.Data), len(responseNoFilter.Data),
			"empty Status should return same as no Status filter")
	})
}

// testTimeBoundaryPrecision verifies that time filters use inclusive semantics:
// - EventStart: >= (events at exactly start time are included)
// - EventEnd: <= (events at exactly end time are included)
// - DeliveryStart: >= (deliveries at exactly start time are included)
// - DeliveryEnd: <= (deliveries at exactly end time are included)
func testTimeBoundaryPrecision(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Create events at precise times
	boundaryTime := time.Now().Truncate(time.Second)
	beforeBoundary := boundaryTime.Add(-1 * time.Second)
	afterBoundary := boundaryTime.Add(1 * time.Second)

	// Event exactly at boundary
	eventAtBoundary := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("evt_at_boundary"),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTime(boundaryTime),
	)
	deliveryAtBoundary := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID("del_at_boundary"),
		testutil.DeliveryFactory.WithEventID(eventAtBoundary.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithTime(boundaryTime),
	)

	// Event before boundary
	eventBefore := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("evt_before"),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTime(beforeBoundary),
	)
	deliveryBefore := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID("del_before"),
		testutil.DeliveryFactory.WithEventID(eventBefore.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithTime(beforeBoundary),
	)

	// Event after boundary
	eventAfter := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("evt_after"),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTime(afterBoundary),
	)
	deliveryAfter := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID("del_after"),
		testutil.DeliveryFactory.WithEventID(eventAfter.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithTime(afterBoundary),
	)

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
		{ID: "de_at", DestinationID: destinationID, Event: *eventAtBoundary, Delivery: deliveryAtBoundary},
		{ID: "de_before", DestinationID: destinationID, Event: *eventBefore, Delivery: deliveryBefore},
		{ID: "de_after", DestinationID: destinationID, Event: *eventAfter, Delivery: deliveryAfter},
	}))

	t.Run("EventStart is inclusive (>=)", func(t *testing.T) {
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &boundaryTime,
			Limit:      10,
		})
		require.NoError(t, err)

		// Should include event at boundary and after, but not before
		ids := extractEventIDs(response.Data)
		assert.Contains(t, ids, "evt_at_boundary", "EventStart should include events at exact boundary")
		assert.Contains(t, ids, "evt_after", "EventStart should include events after boundary")
		assert.NotContains(t, ids, "evt_before", "EventStart should exclude events before boundary")
	})

	t.Run("EventEnd is inclusive (<=)", func(t *testing.T) {
		farPast := boundaryTime.Add(-1 * time.Hour)
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &farPast,
			EventEnd:   &boundaryTime,
			Limit:      10,
		})
		require.NoError(t, err)

		// Should include event at boundary and before, but not after
		ids := extractEventIDs(response.Data)
		assert.Contains(t, ids, "evt_at_boundary", "EventEnd should include events at exact boundary")
		assert.Contains(t, ids, "evt_before", "EventEnd should include events before boundary")
		assert.NotContains(t, ids, "evt_after", "EventEnd should exclude events after boundary")
	})

	t.Run("DeliveryStart is inclusive (>=)", func(t *testing.T) {
		farPast := boundaryTime.Add(-1 * time.Hour)
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:      tenantID,
			EventStart:    &farPast,
			DeliveryStart: &boundaryTime,
			Limit:         10,
		})
		require.NoError(t, err)

		// Should include delivery at boundary and after, but not before
		ids := extractDeliveryIDs(response.Data)
		assert.Contains(t, ids, "del_at_boundary", "DeliveryStart should include deliveries at exact boundary")
		assert.Contains(t, ids, "del_after", "DeliveryStart should include deliveries after boundary")
		assert.NotContains(t, ids, "del_before", "DeliveryStart should exclude deliveries before boundary")
	})

	t.Run("DeliveryEnd is inclusive (<=)", func(t *testing.T) {
		farPast := boundaryTime.Add(-1 * time.Hour)
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:    tenantID,
			EventStart:  &farPast,
			DeliveryEnd: &boundaryTime,
			Limit:       10,
		})
		require.NoError(t, err)

		// Should include delivery at boundary and before, but not after
		ids := extractDeliveryIDs(response.Data)
		assert.Contains(t, ids, "del_at_boundary", "DeliveryEnd should include deliveries at exact boundary")
		assert.Contains(t, ids, "del_before", "DeliveryEnd should include deliveries before boundary")
		assert.NotContains(t, ids, "del_after", "DeliveryEnd should exclude deliveries after boundary")
	})

	t.Run("exact range includes boundary items", func(t *testing.T) {
		// Range from exactly beforeBoundary to exactly afterBoundary
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &beforeBoundary,
			EventEnd:   &afterBoundary,
			Limit:      10,
		})
		require.NoError(t, err)

		// Should include all three
		assert.Len(t, response.Data, 3, "range should include all items including boundaries")
	})
}

// testEventIDPagination verifies pagination works correctly when filtering by EventID.
// This is a common use case: viewing all delivery attempts for a specific event.
func testEventIDPagination(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := "evt_with_many_deliveries"
	baseTime := time.Now().Truncate(time.Second)

	// Create one event with many delivery attempts (simulating retries)
	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTime(baseTime.Add(-1*time.Hour)),
	)

	// Create 10 delivery attempts
	var deliveryEvents []*models.DeliveryEvent
	for i := 0; i < 10; i++ {
		status := "failed"
		if i == 9 {
			status = "success"
		}
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%02d", i)),
			testutil.DeliveryFactory.WithEventID(eventID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithStatus(status),
			testutil.DeliveryFactory.WithTime(baseTime.Add(-time.Duration(60-i)*time.Minute)),
		)
		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_%02d", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		})
	}

	// Also insert a different event to ensure EventID filter works
	otherEvent := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("other_event"),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
	)
	otherDelivery := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithEventID(otherEvent.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
	)
	deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
		ID:            "de_other",
		DestinationID: destinationID,
		Event:         *otherEvent,
		Delivery:      otherDelivery,
	})

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

	startTime := baseTime.Add(-2 * time.Hour)

	t.Run("forward pagination with EventID filter", func(t *testing.T) {
		var collected []*models.DeliveryEvent
		req := driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventID:    eventID,
			EventStart: &startTime,
			Limit:      3, // Small page size to test pagination
		}

		response, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)
		collected = append(collected, response.Data...)

		for response.Next != "" {
			req.Next = response.Next
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
			collected = append(collected, response.Data...)
		}

		assert.Len(t, collected, 10, "should collect exactly 10 deliveries for the event")

		// All should belong to the same event
		for _, de := range collected {
			assert.Equal(t, eventID, de.Event.ID, "all deliveries should be for the filtered event")
		}
	})

	t.Run("backward pagination with EventID filter", func(t *testing.T) {
		req := driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventID:    eventID,
			EventStart: &startTime,
			Limit:      3,
		}

		// Go to last page
		response, err := logStore.ListDeliveryEvent(ctx, req)
		require.NoError(t, err)

		for response.Next != "" {
			req.Next = response.Next
			req.Prev = ""
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
		}

		// Now go backward
		var collected []*models.DeliveryEvent
		collected = append(collected, response.Data...)

		for response.Prev != "" {
			req.Prev = response.Prev
			req.Next = ""
			response, err = logStore.ListDeliveryEvent(ctx, req)
			require.NoError(t, err)
			collected = append(response.Data, collected...)
		}

		assert.Len(t, collected, 10, "backward traversal should collect all deliveries")
	})
}

// testDataImmutability verifies that modifying returned data doesn't affect
// subsequent queries. This is important for data integrity.
func testDataImmutability(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := "immutable_event"
	startTime := time.Now().Add(-1 * time.Hour)

	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("original.topic"),
		testutil.EventFactory.WithMetadata(map[string]string{"key": "original"}),
	)
	delivery := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID("original_delivery"),
		testutil.DeliveryFactory.WithEventID(eventID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithStatus("success"),
	)

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
		{ID: "de_immutable", DestinationID: destinationID, Event: *event, Delivery: delivery},
	}))

	t.Run("modifying ListDeliveryEvent result doesn't affect subsequent queries", func(t *testing.T) {
		// First query
		response1, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)
		require.Len(t, response1.Data, 1)

		// Mutate the returned data
		response1.Data[0].Event.Topic = "MUTATED"
		response1.Data[0].Event.Metadata["key"] = "MUTATED"
		response1.Data[0].Delivery.Status = "MUTATED"

		// Second query should return original values
		response2, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:   tenantID,
			EventStart: &startTime,
			Limit:      10,
		})
		require.NoError(t, err)
		require.Len(t, response2.Data, 1)

		assert.Equal(t, "original.topic", response2.Data[0].Event.Topic,
			"Event.Topic should not be affected by mutation")
		assert.Equal(t, "original", response2.Data[0].Event.Metadata["key"],
			"Event.Metadata should not be affected by mutation")
		assert.Equal(t, "success", response2.Data[0].Delivery.Status,
			"Delivery.Status should not be affected by mutation")
	})

	t.Run("modifying RetrieveEvent result doesn't affect subsequent queries", func(t *testing.T) {
		// First retrieval
		event1, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: tenantID,
			EventID:  eventID,
		})
		require.NoError(t, err)
		require.NotNil(t, event1)

		// Mutate the returned event
		event1.Topic = "MUTATED"
		event1.Metadata["key"] = "MUTATED"

		// Second retrieval should return original values
		event2, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: tenantID,
			EventID:  eventID,
		})
		require.NoError(t, err)
		require.NotNil(t, event2)

		assert.Equal(t, "original.topic", event2.Topic,
			"RetrieveEvent should return fresh copy")
		assert.Equal(t, "original", event2.Metadata["key"],
			"RetrieveEvent metadata should not be affected by mutation")
	})
}

// Helper function to extract event IDs from delivery events
func extractEventIDs(des []*models.DeliveryEvent) []string {
	ids := make([]string, len(des))
	for i, de := range des {
		ids[i] = de.Event.ID
	}
	return ids
}

// Helper function to extract delivery IDs from delivery events
func extractDeliveryIDs(des []*models.DeliveryEvent) []string {
	ids := make([]string, len(des))
	for i, de := range des {
		ids[i] = de.Delivery.ID
	}
	return ids
}

// =============================================================================
// Cursor Validation Tests
// =============================================================================
// These tests verify that cursors encode sort parameters and that using a cursor
// with different sort parameters returns an error. This prevents confusing
// behavior when changing query params mid-pagination.
// =============================================================================

func testCursorValidation(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("cursor with mismatched sortBy returns error", func(t *testing.T) {
		testCursorMismatchedSortBy(t, newHarness)
	})
	t.Run("cursor with mismatched sortOrder returns error", func(t *testing.T) {
		testCursorMismatchedSortOrder(t, newHarness)
	})
	t.Run("malformed cursor returns error", func(t *testing.T) {
		testMalformedCursor(t, newHarness)
	})
	t.Run("cursor works with matching sort params", func(t *testing.T) {
		testCursorMatchingSortParams(t, newHarness)
	})
}

// testCursorMismatchedSortBy verifies that using a cursor generated with one
// sortBy value with a different sortBy value returns ErrInvalidCursor.
func testCursorMismatchedSortBy(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-48 * time.Hour)

	// Insert enough data to get a next cursor
	var deliveryEvents []*models.DeliveryEvent
	for i := 0; i < 5; i++ {
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("evt_cursor_%d", i)),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(fmt.Sprintf("del_cursor_%d", i)),
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_cursor_%d", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		})
	}
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

	// Get a cursor with sortBy=delivery_time
	response1, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
		TenantID:   tenantID,
		SortBy:     "delivery_time",
		SortOrder:  "desc",
		EventStart: &startTime,
		Limit:      2,
	})
	require.NoError(t, err)
	require.NotEmpty(t, response1.Next, "expected next cursor")

	// Try to use the cursor with sortBy=event_time - should fail
	_, err = logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
		TenantID:   tenantID,
		SortBy:     "event_time", // Different from cursor
		SortOrder:  "desc",
		EventStart: &startTime,
		Next:       response1.Next,
		Limit:      2,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, driver.ErrInvalidCursor),
		"expected ErrInvalidCursor, got: %v", err)
}

// testCursorMismatchedSortOrder verifies that using a cursor generated with one
// sortOrder value with a different sortOrder value returns ErrInvalidCursor.
func testCursorMismatchedSortOrder(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-48 * time.Hour)

	// Insert enough data to get a next cursor
	var deliveryEvents []*models.DeliveryEvent
	for i := 0; i < 5; i++ {
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("evt_order_%d", i)),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(fmt.Sprintf("del_order_%d", i)),
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_order_%d", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		})
	}
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

	// Get a cursor with sortOrder=desc
	response1, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
		TenantID:   tenantID,
		SortBy:     "delivery_time",
		SortOrder:  "desc",
		EventStart: &startTime,
		Limit:      2,
	})
	require.NoError(t, err)
	require.NotEmpty(t, response1.Next, "expected next cursor")

	// Try to use the cursor with sortOrder=asc - should fail
	_, err = logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
		TenantID:   tenantID,
		SortBy:     "delivery_time",
		SortOrder:  "asc", // Different from cursor
		EventStart: &startTime,
		Next:       response1.Next,
		Limit:      2,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, driver.ErrInvalidCursor),
		"expected ErrInvalidCursor, got: %v", err)
}

// testMalformedCursor verifies that a malformed cursor string returns ErrInvalidCursor.
func testMalformedCursor(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	startTime := time.Now().Add(-1 * time.Hour)

	testCases := []struct {
		name   string
		cursor string
	}{
		{"completely invalid base62", "!!!invalid!!!"},
		{"random string", "abcdef123456"},
		{"empty after decode", cursor.Encode(cursor.Cursor{})}, // Empty cursor should be fine, but let's test edge cases
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:   tenantID,
				SortBy:     "delivery_time",
				SortOrder:  "desc",
				EventStart: &startTime,
				Next:       tc.cursor,
				Limit:      10,
			})
			// Some of these might not error (e.g., if cursor decodes to valid format)
			// but completely invalid base62 should error
			if tc.name == "completely invalid base62" {
				require.Error(t, err)
				assert.True(t, errors.Is(err, driver.ErrInvalidCursor),
					"expected ErrInvalidCursor for %s, got: %v", tc.name, err)
			}
		})
	}
}

// testCursorMatchingSortParams verifies that cursors work correctly when
// sort parameters match between cursor generation and usage.
func testCursorMatchingSortParams(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-48 * time.Hour)

	// Insert data
	var deliveryEvents []*models.DeliveryEvent
	for i := 0; i < 5; i++ {
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("evt_match_%d", i)),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(fmt.Sprintf("del_match_%d", i)),
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
		)
		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_match_%d", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		})
	}
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

	sortConfigs := []struct {
		sortBy    string
		sortOrder string
	}{
		{"delivery_time", "desc"},
		{"delivery_time", "asc"},
		{"event_time", "desc"},
		{"event_time", "asc"},
	}

	for _, sc := range sortConfigs {
		t.Run(fmt.Sprintf("%s_%s", sc.sortBy, sc.sortOrder), func(t *testing.T) {
			// Get first page with cursor
			response1, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:   tenantID,
				SortBy:     sc.sortBy,
				SortOrder:  sc.sortOrder,
				EventStart: &startTime,
				Limit:      2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, response1.Next, "expected next cursor for %s %s", sc.sortBy, sc.sortOrder)

			// Use cursor with same sort params - should succeed
			response2, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:   tenantID,
				SortBy:     sc.sortBy,
				SortOrder:  sc.sortOrder,
				EventStart: &startTime,
				Next:       response1.Next,
				Limit:      2,
			})
			require.NoError(t, err, "cursor should work with matching sort params for %s %s", sc.sortBy, sc.sortOrder)
			require.NotEmpty(t, response2.Data, "should return data for second page")

			// Verify we got different data (not the same page)
			if len(response1.Data) > 0 && len(response2.Data) > 0 {
				assert.NotEqual(t, response1.Data[0].ID, response2.Data[0].ID,
					"second page should have different data")
			}
		})
	}
}
