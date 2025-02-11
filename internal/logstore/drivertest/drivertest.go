package drivertest

import (
	"context"
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
	events := []*models.Event{}
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		events = append(events,
			testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Second)),
			),
		)
	}

	t.Run("insert many event", func(t *testing.T) {
		assert.NoError(t, logStore.InsertManyEvent(ctx, events))
	})

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
		assert.Equal(t, events[4].Time.UTC().Format(time.RFC3339), nextCursor)
		cursor = nextCursor
	})

	t.Run("list event with cursor", func(t *testing.T) {
		queriedEvents, nextCursor, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID: tenantID,
			Limit:    5,
			Cursor:   cursor,
		})
		require.NoError(t, err)
		require.Len(t, queriedEvents, 5)
		for i := 0; i < 5; i++ {
			require.Equal(t, events[5+i].ID, queriedEvents[i].ID)
		}
		assert.Equal(t, events[9].Time.UTC().Format(time.RFC3339), nextCursor)
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
	require.NoError(t, logStore.InsertManyEvent(ctx, []*models.Event{&event}))

	deliveries := []*models.Delivery{}
	baseTime := time.Now()
	for i := 0; i < 20; i++ {
		deliveries = append(deliveries, &models.Delivery{
			ID:              uuid.New().String(),
			EventID:         event.ID,
			DeliveryEventID: uuid.New().String(),
			DestinationID:   uuid.New().String(),
			Status:          "success",
			Time:            baseTime.Add(-time.Duration(i) * time.Second),
		})
	}

	t.Run("insert many delivery", func(t *testing.T) {
		require.NoError(t, logStore.InsertManyDelivery(ctx, deliveries))
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
		assert.Len(t, queriedDeliveries, len(deliveries))
		for i := 0; i < len(deliveries); i++ {
			assert.Equal(t, deliveries[i].ID, queriedDeliveries[i].ID)
		}
	})
}
