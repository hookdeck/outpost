package drivertest

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Harness interface {
	MakeDriver(ctx context.Context) (driver.LogStore, error)

	Close()
}

type HarnessMaker func(ctx context.Context, t *testing.T) (Harness, error)

func RunConformanceTests(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("TestIntegrationLogStore_EventCRUD", func(t *testing.T) {
		testIntegrationLogStore_EventCRUD(t, newHarness)
	})
	t.Run("TestIntegrationLogStore_DeliveryCRUD", func(t *testing.T) {
		testIntegrationLogStore_DeliveryCRUD(t, newHarness)
	})
}

func testIntegrationLogStore_EventCRUD(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := uuid.New().String()
	destinationIDs := []string{
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	}
	destinationEvents := map[string][]*models.Event{}
	statusEvents := map[string][]*models.Event{}
	destinationStatusEvents := map[string]map[string][]*models.Event{}
	topicEvents := map[string][]*models.Event{}
	timeEvents := map[string][]*models.Event{} // key is like "1h", "24h", etc.
	deliveryEvents := []*models.DeliveryEvent{}
	events := []*models.Event{}

	startTime := time.Now().Add(-48 * time.Hour) // before ALL events
	start := &startTime
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		destinationID := destinationIDs[i%len(destinationIDs)]
		topic := testutil.TestTopics[i%len(testutil.TestTopics)]
		shouldSucceed := i%2 == 0
		shouldRetry := i%3 == 0

		var eventTime time.Time
		switch {
		case i < 5:
			eventTime = baseTime.Add(-time.Duration(i) * time.Minute)
			// i=0: now-0m  (newest)
			// i=1: now-1m
			// i=2: now-2m
			// i=3: now-3m
			// i=4: now-4m  (all within first hour)

		case i < 10:
			eventTime = baseTime.Add(-time.Duration(2*60+i) * time.Minute)
			// i=5: now-125m  (2h5m ago)
			// i=6: now-126m  (2h6m ago)
			// i=7: now-127m  (2h7m ago)
			// i=8: now-128m  (2h8m ago)
			// i=9: now-129m  (2h9m ago)

		case i < 15:
			eventTime = baseTime.Add(-time.Duration(5*60+i) * time.Minute)
			// i=10: now-310m  (5h10m ago)
			// i=11: now-311m  (5h11m ago)
			// i=12: now-312m  (5h12m ago)
			// i=13: now-313m  (5h13m ago)
			// i=14: now-314m  (5h14m ago)

		default:
			eventTime = baseTime.Add(-time.Duration(23*60+i) * time.Minute)
			// i=15: now-1395m  (23h15m ago)
			// i=16: now-1396m  (23h16m ago)
			// i=17: now-1397m  (23h17m ago)
			// i=18: now-1398m  (23h18m ago)
			// i=19: now-1399m  (23h19m ago)
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
		fmt.Printf("Creating event %d: id=%s, time=%s, index=%d\n", i, event.ID, event.Time.Format(time.RFC3339), i)
		events = append(events, event)
		destinationEvents[destinationID] = append(destinationEvents[destinationID], event)
		if _, ok := destinationStatusEvents[destinationID]; !ok {
			destinationStatusEvents[destinationID] = map[string][]*models.Event{}
		}
		topicEvents[topic] = append(topicEvents[topic], event)
		switch {
		case i < 5:
			timeEvents["1h"] = append(timeEvents["1h"], event)
		case i < 10:
			timeEvents["3h"] = append(timeEvents["3h"], event)
		case i < 15:
			timeEvents["6h"] = append(timeEvents["6h"], event)
		default:
			timeEvents["24h"] = append(timeEvents["24h"], event)
		}

		var delivery *models.Delivery
		if shouldRetry {
			delivery = testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%02d_init", i)),
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus("failed"),
			)
			deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
				ID:            fmt.Sprintf("de_%02d_init", i),
				DestinationID: destinationID,
				Event:         *event,
				Delivery:      delivery,
			})
		}
		// NOTE: Do NOT else if here; if shouldRetry is true,
		// we need to append the failed delivery first.
		// Then, we'll append a 2nd delivery after.

		if shouldSucceed {
			statusEvents["success"] = append(statusEvents["success"], event)
			destinationStatusEvents[destinationID]["success"] = append(destinationStatusEvents[destinationID]["success"], event)
			delivery = testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%02d_final", i)),
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus("success"),
			)
		} else {
			statusEvents["failed"] = append(statusEvents["failed"], event)
			destinationStatusEvents[destinationID]["failed"] = append(destinationStatusEvents[destinationID]["failed"], event)
			delivery = testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%02d_final", i)),
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus("failed"),
			)
		}

		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_%02d_final", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		})
	}

	// Setup | Insert
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

	// Queries
	t.Run("list event empty", func(t *testing.T) {
		response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID: "unknown",
			Limit:    5,
			Next:     "",
			Start:    start,
		})
		require.NoError(t, err)
		assert.Empty(t, response.Data)
		assert.Empty(t, response.Next)
		assert.Empty(t, response.Prev, "prev cursor should be empty when no data")
		assert.Equal(t, int64(0), response.Count, "count should be 0 for unknown tenant")
	})

	t.Run("comprehensive list & pagination test", func(t *testing.T) {
		// First page (0-6)
		response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID: tenantID,
			Limit:    7,
			Start:    start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 7, "first page should have 7 items")
		firstPageNext := response.Next

		// Second page (7-13)
		response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID: tenantID,
			Limit:    7,
			Next:     response.Next,
			Start:    start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 7, "second page should have 7 items")
		secondPageNext := response.Next
		secondPagePrev := response.Prev

		// Last page (14-19, partial)
		response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID: tenantID,
			Limit:    7,
			Next:     response.Next,
			Start:    start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 6, "last page should have 6 items")

		// Go back to second page (7-13)
		response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID: tenantID,
			Limit:    7,
			Prev:     response.Prev,
			Start:    start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 7, "going back to second page should have 7 items")
		require.Equal(t, int64(20), response.Count)
		require.NotEmpty(t, response.Prev, "prev cursor should be present")
		require.NotEmpty(t, response.Next, "next cursor should be present")
		require.Equal(t, secondPageNext, response.Next, "next cursor should match original second page next")
		require.Equal(t, secondPagePrev, response.Prev, "prev cursor should match original second page prev")

		// Back to first page (0-6)
		response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID: tenantID,
			Limit:    7,
			Prev:     response.Prev,
			Start:    start,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 7, "back to first page should have 7 items")
		for i := 0; i < 7; i++ {
			require.Equal(t, events[i].ID, response.Data[i].ID)
		}
		require.Equal(t, int64(20), response.Count)
		require.Empty(t, response.Prev, "prev cursor should be empty on first page")
		require.NotEmpty(t, response.Next, "next cursor should be present")
		require.Equal(t, firstPageNext, response.Next, "next cursor should match original first page next")
	})

	t.Run("query by destinations", func(t *testing.T) {
		t.Run("list event with destination filter", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Limit:          3,
				Next:           "",
				Start:          start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 3)
			for i := 0; i < 3; i++ {
				require.Equal(t, destinationEvents[destinationIDs[0]][i].ID, response.Data[i].ID)
			}
			assert.Equal(t, int64(len(destinationEvents[destinationIDs[0]])), response.Count, "count should match events for destination")

			// Step 2: list with cursor
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Limit:          3,
				Next:           response.Next,
				Start:          start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 3)
			for i := 0; i < 3; i++ {
				require.Equal(t, destinationEvents[destinationIDs[0]][3+i].ID, response.Data[i].ID)
			}
		})

		t.Run("list event with destination array filter", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0], destinationIDs[1]},
				Limit:          3,
				Next:           "",
				Start:          start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 3)

			// should equal events index 0, 1, 3
			require.Equal(t, events[0].ID, response.Data[0].ID)
			require.Equal(t, events[1].ID, response.Data[1].ID)
			require.Equal(t, events[3].ID, response.Data[2].ID)

			// Step 2: list with cursor
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0], destinationIDs[1]},
				Limit:          3,
				Next:           response.Next,
				Start:          start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 3)

			// should equal events index 4, 6, 7
			require.Equal(t, events[4].ID, response.Data[0].ID)
			require.Equal(t, events[6].ID, response.Data[1].ID)
			require.Equal(t, events[7].ID, response.Data[2].ID)
		})
	})

	t.Run("query by status", func(t *testing.T) {
		t.Run("list event with status filter (success)", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Status:   "success",
				Limit:    5,
				Start:    start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 5)
			for i := 0; i < 5; i++ {
				require.Equal(t, statusEvents["success"][i].ID, response.Data[i].ID)
				require.Equal(t, "success", response.Data[i].Status)
			}
			assert.Equal(t, int64(len(statusEvents["success"])), response.Count, "count should match successful events")

			// Step 2: list with cursor
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Status:   "success",
				Limit:    5,
				Next:     response.Next,
				Start:    start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 5)
			for i := 0; i < 5; i++ {
				require.Equal(t, statusEvents["success"][5+i].ID, response.Data[i].ID)
				require.Equal(t, "success", response.Data[i].Status)
			}
		})

		t.Run("list event with status filter (failed)", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Status:   "failed",
				Start:    start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, len(statusEvents["failed"]))
			for i := 0; i < len(statusEvents["failed"]); i++ {
				require.Equal(t, statusEvents["failed"][i].ID, response.Data[i].ID)
				require.Equal(t, "failed", response.Data[i].Status)
			}
		})

		t.Run("retrieve event status", func(t *testing.T) {
			// Test success case
			event, err := logStore.RetrieveEvent(ctx, tenantID, statusEvents["success"][0].ID)
			require.NoError(t, err)
			require.Equal(t, "success", event.Status)

			// Test failed case
			event, err = logStore.RetrieveEvent(ctx, tenantID, statusEvents["failed"][0].ID)
			require.NoError(t, err)
			require.Equal(t, "failed", event.Status)
		})
	})

	t.Run("query by status and destination", func(t *testing.T) {
		t.Run("list event with status and destination filter (success)", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Status:         "success",
				Limit:          2,
				Start:          start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for i := 0; i < 2; i++ {
				require.Equal(t, destinationStatusEvents[destinationIDs[0]]["success"][i].ID, response.Data[i].ID)
			}

			// Step 2: list with cursor
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Status:         "success",
				Limit:          2,
				Next:           response.Next,
				Start:          start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for i := 0; i < 2; i++ {
				require.Equal(t, destinationStatusEvents[destinationIDs[0]]["success"][2+i].ID, response.Data[i].ID)
			}
		})
	})

	t.Run("query by topic", func(t *testing.T) {

		t.Run("list events with single topic", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Topics:   []string{testutil.TestTopics[0]},
				Limit:    2,
				Start:    start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for index, e := range response.Data {
				require.Equal(t, testutil.TestTopics[0], e.Topic)
				require.Equal(t, topicEvents[e.Topic][index].ID, e.ID)
			}

			// Step 2: list with cursor
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Topics:   []string{testutil.TestTopics[0]},
				Limit:    2,
				Next:     response.Next,
				Start:    start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for index, e := range response.Data {
				require.Equal(t, testutil.TestTopics[0], e.Topic)
				require.Equal(t, topicEvents[e.Topic][2+index].ID, e.ID)
			}
		})

		t.Run("list events with multiple topics", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Topics:   testutil.TestTopics[:2], // first two topics
				Limit:    2,
				Start:    start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for _, e := range response.Data {
				require.Contains(t, testutil.TestTopics[:2], e.Topic)
				require.Equal(t, topicEvents[e.Topic][0].ID, e.ID) // first event of each topic
			}

			// Step 2: list with cursor
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Topics:   testutil.TestTopics[:2],
				Limit:    2,
				Next:     response.Next,
				Start:    start,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for _, e := range response.Data {
				require.Contains(t, testutil.TestTopics[:2], e.Topic)
				require.Equal(t, topicEvents[e.Topic][1].ID, e.ID) // second event of each topic
			}
		})
	})

	t.Run("query by time", func(t *testing.T) {
		t.Run("list events with no time params (defaults to last hour)", func(t *testing.T) {

			// First page
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Limit:    2, // Smaller limit to test pagination
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for i, e := range response.Data {
				require.Equal(t, timeEvents["1h"][i].ID, e.ID)
			}
			assert.Equal(t, int64(len(timeEvents["1h"])), response.Count, "count should match events in last hour")

			// second window
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Limit:    2,
				Next:     response.Next,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			for i, e := range response.Data {
				require.Equal(t, timeEvents["1h"][i+2].ID, e.ID)
			}
			// Should still have one more event in the 1h bucket

			// Final page
			response, err = logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Limit:    2,
				Next:     response.Next,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1) // Last event in the 1h bucket
			require.Equal(t, timeEvents["1h"][4].ID, response.Data[0].ID)
			// require.Empty(t, response.Next) // No more events
		})

		t.Run("list events from 3 hours ago", func(t *testing.T) {
			threeHoursAgo := baseTime.Add(-3 * time.Hour)
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Start:    &threeHoursAgo,
			})
			require.NoError(t, err)
			// Should include events from 1h and 3h buckets
			expectedCount := len(timeEvents["1h"]) + len(timeEvents["3h"])
			require.Len(t, response.Data, expectedCount)
		})

		t.Run("list events with explicit window", func(t *testing.T) {
			sevenHoursAgo := baseTime.Add(-7 * time.Hour)
			fiveHoursAgo := baseTime.Add(-5 * time.Hour)
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Start:    &sevenHoursAgo,
				End:      &fiveHoursAgo,
				Limit:    5,
			})
			require.NoError(t, err)
			// Should only include events from 6h bucket (5-6h ago)
			require.Len(t, response.Data, len(timeEvents["6h"]))
			for i, e := range response.Data {
				require.Equal(t, timeEvents["6h"][i].ID, e.ID)
			}
		})

		t.Run("list events with end time only (defaults start to end-1h)", func(t *testing.T) {
			twoHoursAgo := baseTime.Add(-2 * time.Hour)
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				End:      &twoHoursAgo,
				Limit:    5,
			})
			require.NoError(t, err)
			// Should include events from 3h bucket only (2-3h ago)
			require.Len(t, response.Data, len(timeEvents["3h"]))
		})

		t.Run("list events with combined time and other filters", func(t *testing.T) {
			threeHoursAgo := baseTime.Add(-3 * time.Hour)
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				Start:          &threeHoursAgo,
				Topics:         []string{testutil.TestTopics[0]},
				DestinationIDs: []string{destinationIDs[0]},
				Status:         "success",
				Limit:          5,
			})
			require.NoError(t, err)
			for _, e := range response.Data {
				require.Equal(t, testutil.TestTopics[0], e.Topic)
				require.Equal(t, destinationIDs[0], e.DestinationID)
				require.Equal(t, "success", e.Status)
				require.True(t, e.Time.After(threeHoursAgo))
			}
		})
	})
}

func testIntegrationLogStore_DeliveryCRUD(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	destinationID := uuid.New().String()
	event := testutil.EventFactory.Any()
	deliveryEvents := []*models.DeliveryEvent{}
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		delivery := &models.Delivery{
			ID:              uuid.New().String(),
			EventID:         event.ID,
			DeliveryEventID: uuid.New().String(),
			DestinationID:   destinationID,
			Status:          "success",
			Time:            baseTime.Add(-time.Duration(i) * time.Second),
		}
		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            delivery.ID,
			DestinationID: delivery.DestinationID,
			Event:         event,
			Delivery:      delivery,
		})
	}

	t.Run("insert many delivery", func(t *testing.T) {
		require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))
	})

	t.Run("list delivery empty", func(t *testing.T) {
		queriedDeliveries, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
			EventID:       "unknown",
			DestinationID: "",
		})
		require.NoError(t, err)
		assert.Empty(t, queriedDeliveries)
	})

	t.Run("list delivery", func(t *testing.T) {
		queriedDeliveries, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
			EventID:       event.ID,
			DestinationID: destinationID,
		})
		require.NoError(t, err)
		assert.Len(t, queriedDeliveries, len(deliveryEvents))
		for i := 0; i < len(deliveryEvents); i++ {
			assert.Equal(t, deliveryEvents[i].Delivery.ID, queriedDeliveries[i].ID)
		}
	})
}
