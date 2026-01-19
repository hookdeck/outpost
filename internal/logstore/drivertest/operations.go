package drivertest

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testOperations tests all CRUD operations with a single shared harness.
func testOperations(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	t.Run("Insert", func(t *testing.T) {
		testInsert(t, ctx, logStore, h)
	})
	t.Run("ListEvent", func(t *testing.T) {
		testListEvent(t, ctx, logStore)
	})
	t.Run("ListDeliveryEvent", func(t *testing.T) {
		testListDeliveryEvent(t, ctx, logStore)
	})
	t.Run("RetrieveEvent", func(t *testing.T) {
		testRetrieveEvent(t, ctx, logStore)
	})
	t.Run("RetrieveDeliveryEvent", func(t *testing.T) {
		testRetrieveDeliveryEvent(t, ctx, logStore, h)
	})
}

func testInsert(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	startTime := time.Now().Add(-1 * time.Hour)

	t.Run("single delivery event", func(t *testing.T) {
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

		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID: tenantID,
			EventID:  event.ID,
			Limit:    10,
			Start:    &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1)
		assert.Equal(t, event.ID, response.Data[0].Event.ID)
		assert.Equal(t, "success", response.Data[0].Delivery.Status)
	})

	t.Run("multiple delivery events", func(t *testing.T) {
		eventID := idgen.Event()
		baseDeliveryTime := time.Now().Truncate(time.Second)
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(eventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
		)

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

		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID: tenantID,
			EventID:  eventID,
			Limit:    10,
			Start:    &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 3)
	})

	t.Run("empty slice", func(t *testing.T) {
		err := logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{})
		require.NoError(t, err)
	})

	t.Run("duplicate insert is idempotent", func(t *testing.T) {
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

		require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, batch))
		require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, batch))
		require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, batch))
		require.NoError(t, h.FlushWrites(ctx))

		queryStart := eventTime.Add(-1 * time.Hour)
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID: idempotentTenantID,
			Limit:    100,
			Start:    &queryStart,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1, "duplicate inserts should not create multiple records")
	})
}

func testListEvent(t *testing.T, ctx context.Context, logStore driver.LogStore) {
	tenantID := idgen.String()
	destinationIDs := []string{idgen.Destination(), idgen.Destination(), idgen.Destination()}

	destinationEvents := map[string][]*models.Event{}
	topicEvents := map[string][]*models.Event{}

	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-48 * time.Hour)

	// Create 15 events spread across destinations and topics
	for i := 0; i < 15; i++ {
		destinationID := destinationIDs[i%len(destinationIDs)]
		topic := testutil.TestTopics[i%len(testutil.TestTopics)]
		eventTime := baseTime.Add(-time.Duration(i) * time.Minute)

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("evt_%02d", i)),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithTime(eventTime),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTopic(topic),
		)

		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%02d", i)),
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithStatus("success"),
			testutil.DeliveryFactory.WithTime(eventTime.Add(time.Millisecond)),
		)

		de := &models.DeliveryEvent{
			ID:            fmt.Sprintf("de_%02d", i),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		}

		require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{de}))
		destinationEvents[destinationID] = append(destinationEvents[destinationID], event)
		topicEvents[topic] = append(topicEvents[topic], event)
	}

	t.Run("filter by destination", func(t *testing.T) {
		destID := destinationIDs[0]
		response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID:       tenantID,
			DestinationIDs: []string{destID},
			Limit:          100,
			EventStart:     &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(destinationEvents[destID]))
		for _, event := range response.Data {
			assert.Equal(t, destID, event.DestinationID)
		}
	})

	t.Run("filter by multiple destinations", func(t *testing.T) {
		destIDs := []string{destinationIDs[0], destinationIDs[1]}
		response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID:       tenantID,
			DestinationIDs: destIDs,
			Limit:          100,
			EventStart:     &startTime,
		})
		require.NoError(t, err)
		expectedCount := len(destinationEvents[destIDs[0]]) + len(destinationEvents[destIDs[1]])
		require.Len(t, response.Data, expectedCount)
	})

	t.Run("filter by topic", func(t *testing.T) {
		topic := testutil.TestTopics[0]
		response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID:   tenantID,
			Topics:     []string{topic},
			Limit:      100,
			EventStart: &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(topicEvents[topic]))
	})

	t.Run("filter by time range", func(t *testing.T) {
		eventStart := baseTime.Add(-5 * time.Minute)
		eventEnd := baseTime.Add(time.Minute)
		response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID:   tenantID,
			EventStart: &eventStart,
			EventEnd:   &eventEnd,
			Limit:      100,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 6)
	})
}

func testListDeliveryEvent(t *testing.T, ctx context.Context, logStore driver.LogStore) {
	tenantID := idgen.String()
	destinationIDs := []string{idgen.Destination(), idgen.Destination(), idgen.Destination()}

	destinationDeliveryEvents := map[string][]*models.DeliveryEvent{}
	statusDeliveryEvents := map[string][]*models.DeliveryEvent{}
	topicDeliveryEvents := map[string][]*models.DeliveryEvent{}
	allDeliveryEvents := []*models.DeliveryEvent{}

	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-48 * time.Hour)

	for i := 0; i < 20; i++ {
		destinationID := destinationIDs[i%len(destinationIDs)]
		topic := testutil.TestTopics[i%len(testutil.TestTopics)]
		shouldSucceed := i%2 == 0
		shouldRetry := i%3 == 0

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
			testutil.EventFactory.WithMetadata(map[string]string{"index": strconv.Itoa(i)}),
		)

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
			deliveryTime = deliveryTime.Add(time.Millisecond)
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
	}

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, allDeliveryEvents))

	t.Run("filter by destination", func(t *testing.T) {
		destID := destinationIDs[0]
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID:       tenantID,
			DestinationIDs: []string{destID},
			Limit:          100,
			Start:          &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(destinationDeliveryEvents[destID]))
	})

	t.Run("filter by status", func(t *testing.T) {
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID: tenantID,
			Status:   "success",
			Limit:    100,
			Start:    &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(statusDeliveryEvents["success"]))
	})

	t.Run("filter by topic", func(t *testing.T) {
		topic := testutil.TestTopics[0]
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID: tenantID,
			Topics:   []string{topic},
			Limit:    100,
			Start:    &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, len(topicDeliveryEvents[topic]))
	})

	t.Run("filter by event ID", func(t *testing.T) {
		eventID := "evt_00" // Has retry, so 2 deliveries
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID: tenantID,
			EventID:  eventID,
			Limit:    100,
			Start:    &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 2)
	})
}

func testRetrieveEvent(t *testing.T, ctx context.Context, logStore driver.LogStore) {
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
		testutil.EventFactory.WithMetadata(map[string]string{"source": "test"}),
		testutil.EventFactory.WithData(map[string]interface{}{"user_id": "usr_123"}),
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

	t.Run("retrieve existing event", func(t *testing.T) {
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID: tenantID,
			EventID:  eventID,
		})
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.Equal(t, eventID, retrieved.ID)
		assert.Equal(t, tenantID, retrieved.TenantID)
		assert.Equal(t, "user.created", retrieved.Topic)
	})

	t.Run("retrieve with destination filter", func(t *testing.T) {
		retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
			TenantID:      tenantID,
			EventID:       eventID,
			DestinationID: destinationID,
		})
		require.NoError(t, err)
		require.NotNil(t, retrieved)
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
		assert.Nil(t, retrieved)
	})
}

func testRetrieveDeliveryEvent(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := idgen.Event()
	deliveryID := idgen.Delivery()
	eventTime := time.Now().Truncate(time.Millisecond)
	deliveryTime := eventTime.Add(100 * time.Millisecond)

	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithTime(eventTime),
	)

	delivery := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID(deliveryID),
		testutil.DeliveryFactory.WithEventID(eventID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithStatus("success"),
		testutil.DeliveryFactory.WithTime(deliveryTime),
	)
	delivery.Code = "200"

	de := &models.DeliveryEvent{
		ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
		DestinationID: destinationID,
		Event:         *event,
		Delivery:      delivery,
		Manual:        true,
		Attempt:       3,
	}

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{de}))
	require.NoError(t, h.FlushWrites(ctx))

	t.Run("retrieve existing delivery", func(t *testing.T) {
		retrieved, err := logStore.RetrieveDeliveryEvent(ctx, driver.RetrieveDeliveryEventRequest{
			TenantID:   tenantID,
			DeliveryID: deliveryID,
		})
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.Equal(t, deliveryID, retrieved.Delivery.ID)
		assert.Equal(t, "success", retrieved.Delivery.Status)
		assert.True(t, retrieved.Manual)
		assert.Equal(t, 3, retrieved.Attempt)
	})

	t.Run("retrieve non-existent delivery", func(t *testing.T) {
		retrieved, err := logStore.RetrieveDeliveryEvent(ctx, driver.RetrieveDeliveryEventRequest{
			TenantID:   tenantID,
			DeliveryID: "non-existent",
		})
		require.NoError(t, err)
		assert.Nil(t, retrieved)
	})

	t.Run("retrieve with wrong tenant", func(t *testing.T) {
		retrieved, err := logStore.RetrieveDeliveryEvent(ctx, driver.RetrieveDeliveryEventRequest{
			TenantID:   "wrong-tenant",
			DeliveryID: deliveryID,
		})
		require.NoError(t, err)
		assert.Nil(t, retrieved)
	})
}
