package drivertest

import (
	"context"
	"log"
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
	deliveryEvents := []*models.DeliveryEvent{}
	events := []*models.Event{}
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		destinationID := destinationIDs[i%len(destinationIDs)]
		shouldSucceed := i%2 == 0
		shouldRetry := i%3 == 0

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Second)),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithEligibleForRetry(shouldRetry),
			testutil.EventFactory.WithMetadata(map[string]string{
				"index": strconv.Itoa(i),
			}),
		)
		events = append(events, event)
		destinationEvents[destinationID] = append(destinationEvents[destinationID], event)
		if _, ok := destinationStatusEvents[destinationID]; !ok {
			destinationStatusEvents[destinationID] = map[string][]*models.Event{}
		}

		var delivery *models.Delivery
		if shouldRetry {
			delivery = testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus("failed"),
			)
			deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
				ID:            uuid.New().String(),
				DestinationID: destinationID,
				Event:         *event,
				Delivery:      delivery,
			})
		}

		if shouldSucceed {
			statusEvents["success"] = append(statusEvents["success"], event)
			destinationStatusEvents[destinationID]["success"] = append(destinationStatusEvents[destinationID]["success"], event)
			delivery = testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus("success"),
			)
		} else {
			statusEvents["failed"] = append(statusEvents["failed"], event)
			destinationStatusEvents[destinationID]["failed"] = append(destinationStatusEvents[destinationID]["failed"], event)
			delivery = testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithStatus("failed"),
			)
		}

		deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
			ID:            uuid.New().String(),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		})
	}

	// Setup | Insert
	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

	// Queries
	t.Run("base queries", func(t *testing.T) {
		t.Run("list event empty", func(t *testing.T) {
			queriedEvents, nextCursor, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: "unknown",
				Limit:    5,
				Cursor:   "",
			})
			require.NoError(t, err)
			assert.Empty(t, queriedEvents)
			assert.Empty(t, nextCursor)
		})

		var cursor string
		t.Run("list event", func(t *testing.T) {
			queriedEvents, nextCursor, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Limit:    5,
				Cursor:   "",
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 5)
			for i := 0; i < 5; i++ {
				require.Equal(t, events[i].ID, queriedEvents[i].ID)
			}
			cursor = nextCursor
		})

		t.Run("list event with cursor", func(t *testing.T) {
			queriedEvents, _, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Limit:    5,
				Cursor:   cursor,
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 5)
			for i := 0; i < 5; i++ {
				require.Equal(t, events[5+i].ID, queriedEvents[i].ID)
			}
		})
	})

	t.Run("query by destinations", func(t *testing.T) {
		var cursor string
		t.Run("list event with destination filter", func(t *testing.T) {
			log.Println("list event destination", "nextCursor")
			queriedEvents, nextCursor, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Limit:          3,
				Cursor:         "",
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 3)
			for i := 0; i < 3; i++ {
				require.Equal(t, destinationEvents[destinationIDs[0]][i].ID, queriedEvents[i].ID)
			}
			t.Logf("cursor: %s", nextCursor)
			cursor = nextCursor
		})

		t.Run("list event with destination filter and cursor", func(t *testing.T) {
			queriedEvents, _, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Limit:          3,
				Cursor:         cursor,
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 3)
			for i := 0; i < 3; i++ {
				require.Equal(t, destinationEvents[destinationIDs[0]][3+i].ID, queriedEvents[i].ID)
			}
		})

		t.Run("list event with destination array filter", func(t *testing.T) {
			queriedEvents, nextCursor, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0], destinationIDs[1]},
				Limit:          3,
				Cursor:         "",
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 3)

			// should equal events index 0, 1, 3
			require.Equal(t, events[0].ID, queriedEvents[0].ID)
			require.Equal(t, events[1].ID, queriedEvents[1].ID)
			require.Equal(t, events[3].ID, queriedEvents[2].ID)

			cursor = nextCursor
		})

		t.Run("list event with destination array filter and cursor", func(t *testing.T) {
			queriedEvents, _, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0], destinationIDs[1]},
				Limit:          3,
				Cursor:         cursor,
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 3)

			// should equal events index 4, 6, 7
			require.Equal(t, events[4].ID, queriedEvents[0].ID)
			require.Equal(t, events[6].ID, queriedEvents[1].ID)
			require.Equal(t, events[7].ID, queriedEvents[2].ID)
		})
	})

	t.Run("query by status", func(t *testing.T) {
		var cursor string
		t.Run("list event with status filter (success)", func(t *testing.T) {
			queriedEvents, nextCursor, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Status:   "success",
				Limit:    5,
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 5)
			for i := 0; i < 5; i++ {
				require.Equal(t, statusEvents["success"][i].ID, queriedEvents[i].ID)
				require.Equal(t, "success", queriedEvents[i].Status)
			}
			cursor = nextCursor
		})

		t.Run("list event with status filter and cursor", func(t *testing.T) {
			log.Println("list event success")
			queriedEvents, _, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Status:   "success",
				Limit:    5,
				Cursor:   cursor,
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 5)
			for i := 0; i < 5; i++ {
				require.Equal(t, statusEvents["success"][5+i].ID, queriedEvents[i].ID)
				require.Equal(t, "success", queriedEvents[i].Status)
			}
			log.Println("list event success works")
		})

		t.Run("list event with status filter (failed)", func(t *testing.T) {
			queriedEvents, _, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID: tenantID,
				Status:   "failed",
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, len(statusEvents["failed"]))
			for i := 0; i < len(statusEvents["failed"]); i++ {
				require.Equal(t, statusEvents["failed"][i].ID, queriedEvents[i].ID)
				require.Equal(t, "failed", queriedEvents[i].Status)
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
		var cursor string
		t.Run("list event with status and destination filter (success)", func(t *testing.T) {
			queriedEvents, nextCursor, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Status:         "success",
				Limit:          2,
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 2)
			for i := 0; i < 2; i++ {
				require.Equal(t, destinationStatusEvents[destinationIDs[0]]["success"][i].ID, queriedEvents[i].ID)
			}
			cursor = nextCursor
		})

		t.Run("list event with status and destination filter and cursor", func(t *testing.T) {
			queriedEvents, _, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{destinationIDs[0]},
				Status:         "success",
				Limit:          2,
				Cursor:         cursor,
			})
			require.NoError(t, err)
			require.Len(t, queriedEvents, 2)
			for i := 0; i < 2; i++ {
				require.Equal(t, destinationStatusEvents[destinationIDs[0]]["success"][2+i].ID, queriedEvents[i].ID)
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

	event := testutil.EventFactory.Any()
	deliveryEvents := []*models.DeliveryEvent{}
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		delivery := &models.Delivery{
			ID:              uuid.New().String(),
			EventID:         event.ID,
			DeliveryEventID: uuid.New().String(),
			DestinationID:   uuid.New().String(),
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
			EventID: "unknown",
		})
		require.NoError(t, err)
		assert.Empty(t, queriedDeliveries)
	})

	t.Run("list delivery", func(t *testing.T) {
		queriedDeliveries, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
			EventID: event.ID,
		})
		require.NoError(t, err)
		assert.Len(t, queriedDeliveries, len(deliveryEvents))
		for i := 0; i < len(deliveryEvents); i++ {
			assert.Equal(t, deliveryEvents[i].Delivery.ID, queriedDeliveries[i].ID)
		}
	})
}
