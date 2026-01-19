package drivertest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMisc tests isolation, edge cases, and cursor validation with a single shared harness.
func testMisc(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	t.Run("Isolation", func(t *testing.T) {
		testIsolation(t, ctx, logStore, h)
	})
	t.Run("EdgeCases", func(t *testing.T) {
		testEdgeCases(t, ctx, logStore, h)
	})
	t.Run("CursorValidation", func(t *testing.T) {
		testCursorValidation(t, ctx, logStore, h)
	})
}

func testIsolation(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	tenant1ID := idgen.String()
	tenant2ID := idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Truncate(time.Second)
	startTime := baseTime.Add(-1 * time.Hour)

	event1 := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("tenant1-event"),
		testutil.EventFactory.WithTenantID(tenant1ID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("test.topic"),
		testutil.EventFactory.WithTime(baseTime.Add(-10*time.Minute)),
	)
	delivery1 := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID("tenant1-delivery"),
		testutil.DeliveryFactory.WithEventID(event1.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithStatus("success"),
		testutil.DeliveryFactory.WithTime(baseTime.Add(-10*time.Minute)),
	)

	event2 := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("tenant2-event"),
		testutil.EventFactory.WithTenantID(tenant2ID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("test.topic"),
		testutil.EventFactory.WithTime(baseTime.Add(-5*time.Minute)),
	)
	delivery2 := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID("tenant2-delivery"),
		testutil.DeliveryFactory.WithEventID(event2.ID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithStatus("failed"),
		testutil.DeliveryFactory.WithTime(baseTime.Add(-5*time.Minute)),
	)

	require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
		{ID: idgen.DeliveryEvent(), DestinationID: destinationID, Event: *event1, Delivery: delivery1},
		{ID: idgen.DeliveryEvent(), DestinationID: destinationID, Event: *event2, Delivery: delivery2},
	}))
	require.NoError(t, h.FlushWrites(ctx))

	t.Run("TenantIsolation", func(t *testing.T) {
		t.Run("ListDeliveryEvent isolates by tenant", func(t *testing.T) {
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID: tenant1ID,
				Limit:    100,
				Start:    &startTime,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.Equal(t, "tenant1-event", response.Data[0].Event.ID)

			response, err = logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID: tenant2ID,
				Limit:    100,
				Start:    &startTime,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.Equal(t, "tenant2-event", response.Data[0].Event.ID)
		})

		t.Run("RetrieveEvent isolates by tenant", func(t *testing.T) {
			retrieved, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: tenant1ID,
				EventID:  "tenant2-event",
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)

			retrieved, err = logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: tenant2ID,
				EventID:  "tenant1-event",
			})
			require.NoError(t, err)
			assert.Nil(t, retrieved)
		})
	})

	t.Run("CrossTenantQueries", func(t *testing.T) {
		t.Run("ListEvent returns all tenants when TenantID empty", func(t *testing.T) {
			response, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:       "",
				DestinationIDs: []string{destinationID},
				Limit:          100,
				EventStart:     &startTime,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)

			tenantsSeen := map[string]bool{}
			for _, event := range response.Data {
				tenantsSeen[event.TenantID] = true
			}
			assert.True(t, tenantsSeen[tenant1ID])
			assert.True(t, tenantsSeen[tenant2ID])
		})

		t.Run("ListDeliveryEvent returns all tenants when TenantID empty", func(t *testing.T) {
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:       "",
				DestinationIDs: []string{destinationID},
				Limit:          100,
				Start:          &startTime,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
		})

		t.Run("RetrieveEvent finds event across tenants when TenantID empty", func(t *testing.T) {
			retrieved1, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: "",
				EventID:  "tenant1-event",
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved1)
			assert.Equal(t, tenant1ID, retrieved1.TenantID)

			retrieved2, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{
				TenantID: "",
				EventID:  "tenant2-event",
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved2)
			assert.Equal(t, tenant2ID, retrieved2.TenantID)
		})

		t.Run("RetrieveDeliveryEvent finds delivery across tenants when TenantID empty", func(t *testing.T) {
			retrieved1, err := logStore.RetrieveDeliveryEvent(ctx, driver.RetrieveDeliveryEventRequest{
				TenantID:   "",
				DeliveryID: "tenant1-delivery",
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved1)
			assert.Equal(t, tenant1ID, retrieved1.Event.TenantID)

			retrieved2, err := logStore.RetrieveDeliveryEvent(ctx, driver.RetrieveDeliveryEventRequest{
				TenantID:   "",
				DeliveryID: "tenant2-delivery",
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved2)
			assert.Equal(t, tenant2ID, retrieved2.Event.TenantID)
		})
	})
}

func testEdgeCases(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	t.Run("invalid sort values use defaults", func(t *testing.T) {
		tenantID := idgen.String()
		destinationID := idgen.Destination()
		baseTime := time.Now().Truncate(time.Second)

		var deliveryEvents []*models.DeliveryEvent
		for i := range 3 {
			event := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID(fmt.Sprintf("sort_evt_%d", i)),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithDestinationID(destinationID),
				testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
			)
			delivery := testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("sort_del_%d", i)),
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
			)
			deliveryEvents = append(deliveryEvents, &models.DeliveryEvent{
				ID:            fmt.Sprintf("sort_de_%d", i),
				DestinationID: destinationID,
				Event:         *event,
				Delivery:      delivery,
			})
		}
		require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, deliveryEvents))

		startTime := baseTime.Add(-48 * time.Hour)

		t.Run("invalid SortOrder uses default (desc)", func(t *testing.T) {
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:  tenantID,
				SortOrder: "sideways",
				Start:     &startTime,
				Limit:     10,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 3)
			assert.Equal(t, "sort_del_0", response.Data[0].Delivery.ID)
			assert.Equal(t, "sort_del_2", response.Data[2].Delivery.ID)
		})
	})

	t.Run("empty vs nil filter semantics", func(t *testing.T) {
		tenantID := idgen.String()
		destinationID := idgen.Destination()
		startTime := time.Now().Add(-1 * time.Hour)

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
				Start:          &startTime,
				Limit:          10,
			})
			require.NoError(t, err)

			responseEmpty, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{},
				Start:          &startTime,
				Limit:          10,
			})
			require.NoError(t, err)

			assert.Equal(t, len(responseNil.Data), len(responseEmpty.Data))
			assert.Equal(t, 1, len(responseNil.Data))
		})
	})

	t.Run("time boundary precision", func(t *testing.T) {
		tenantID := idgen.String()
		destinationID := idgen.Destination()
		boundaryTime := time.Now().Truncate(time.Second)
		beforeBoundary := boundaryTime.Add(-1 * time.Second)
		afterBoundary := boundaryTime.Add(1 * time.Second)

		eventBefore := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID("time_evt_before"),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(beforeBoundary),
		)
		eventAt := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID("time_evt_at"),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(boundaryTime),
		)
		eventAfter := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID("time_evt_after"),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(afterBoundary),
		)

		for _, evt := range []*models.Event{eventBefore, eventAt, eventAfter} {
			delivery := testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("del_%s", evt.ID)),
				testutil.DeliveryFactory.WithEventID(evt.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithTime(evt.Time),
			)
			require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
				{ID: idgen.DeliveryEvent(), DestinationID: destinationID, Event: *evt, Delivery: delivery},
			}))
		}

		t.Run("Start is inclusive (>=)", func(t *testing.T) {
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID: tenantID,
				Start:    &boundaryTime,
				Limit:    10,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
		})

		t.Run("End is inclusive (<=)", func(t *testing.T) {
			farPast := boundaryTime.Add(-1 * time.Hour)
			response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID: tenantID,
				Start:    &farPast,
				End:      &boundaryTime,
				Limit:    10,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
		})
	})

	t.Run("data immutability", func(t *testing.T) {
		tenantID := idgen.String()
		destinationID := idgen.Destination()
		startTime := time.Now().Add(-1 * time.Hour)

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
		)
		require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
			{ID: idgen.DeliveryEvent(), DestinationID: destinationID, Event: *event, Delivery: delivery},
		}))

		t.Run("modifying ListDeliveryEvent result doesn't affect subsequent queries", func(t *testing.T) {
			response1, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID: tenantID,
				Limit:    10,
				Start:    &startTime,
			})
			require.NoError(t, err)
			require.Len(t, response1.Data, 1)

			originalID := response1.Data[0].Event.ID
			response1.Data[0].Event.ID = "MODIFIED"

			response2, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID: tenantID,
				Limit:    10,
				Start:    &startTime,
			})
			require.NoError(t, err)
			require.Len(t, response2.Data, 1)
			assert.Equal(t, originalID, response2.Data[0].Event.ID)
		})
	})

	t.Run("concurrent duplicate inserts are idempotent", func(t *testing.T) {
		tenantID := idgen.String()
		destinationID := idgen.Destination()
		eventTime := time.Now().Add(-30 * time.Minute).Truncate(time.Second)
		deliveryTime := eventTime.Add(1 * time.Second)
		startTime := eventTime.Add(-1 * time.Hour)

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(eventTime),
		)
		delivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithEventID(event.ID),
			testutil.DeliveryFactory.WithDestinationID(destinationID),
			testutil.DeliveryFactory.WithStatus("success"),
			testutil.DeliveryFactory.WithTime(deliveryTime),
		)
		de := &models.DeliveryEvent{
			ID:            idgen.DeliveryEvent(),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		}
		batch := []*models.DeliveryEvent{de}

		// Race N goroutines all inserting the same record
		const numGoroutines = 10
		var wg sync.WaitGroup
		for range numGoroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = logStore.InsertManyDeliveryEvent(ctx, batch)
			}()
		}
		wg.Wait()
		require.NoError(t, h.FlushWrites(ctx))

		// Assert: still exactly 1 record
		response, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
			TenantID: tenantID,
			Limit:    100,
			Start:    &startTime,
		})
		require.NoError(t, err)
		require.Len(t, response.Data, 1, "concurrent duplicate inserts should result in exactly 1 record")
	})
}

func testCursorValidation(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	t.Run("malformed cursor returns error", func(t *testing.T) {
		tenantID := idgen.String()
		startTime := time.Now().Add(-1 * time.Hour)

		testCases := []struct {
			name   string
			cursor string
		}{
			{"completely invalid base62", "!!!invalid!!!"},
			{"random string", "abcdef123456"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
					TenantID:  tenantID,
					SortOrder: "desc",
					Next:      tc.cursor,
					Start:     &startTime,
					Limit:     10,
				})
				require.Error(t, err)
				assert.True(t, errors.Is(err, driver.ErrInvalidCursor), "expected driver.ErrInvalidCursor, got: %v", err)
			})
		}
	})

	t.Run("cursor works with matching sort params", func(t *testing.T) {
		tenantID := idgen.String()
		destinationID := idgen.Destination()
		baseTime := time.Now().Truncate(time.Second)
		startTime := baseTime.Add(-48 * time.Hour)

		for i := range 5 {
			event := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID(fmt.Sprintf("cursor_evt_%d", i)),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithDestinationID(destinationID),
				testutil.EventFactory.WithTime(baseTime.Add(time.Duration(i)*time.Second)),
			)
			delivery := testutil.DeliveryFactory.AnyPointer(
				testutil.DeliveryFactory.WithID(fmt.Sprintf("cursor_del_%d", i)),
				testutil.DeliveryFactory.WithEventID(event.ID),
				testutil.DeliveryFactory.WithDestinationID(destinationID),
				testutil.DeliveryFactory.WithTime(baseTime.Add(time.Duration(i)*time.Second)),
			)
			require.NoError(t, logStore.InsertManyDeliveryEvent(ctx, []*models.DeliveryEvent{
				{ID: fmt.Sprintf("cursor_de_%d", i), DestinationID: destinationID, Event: *event, Delivery: delivery},
			}))
		}
		require.NoError(t, h.FlushWrites(ctx))

		t.Run("delivery_time desc", func(t *testing.T) {
			page1, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:  tenantID,
				SortOrder: "desc",
				Start:     &startTime,
				Limit:     2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page1.Next)

			page2, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:  tenantID,
				SortOrder: "desc",
				Next:      page1.Next,
				Start:     &startTime,
				Limit:     2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page2.Data)
		})

		t.Run("delivery_time asc", func(t *testing.T) {
			page1, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:  tenantID,
				SortOrder: "asc",
				Start:     &startTime,
				Limit:     2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page1.Next)

			page2, err := logStore.ListDeliveryEvent(ctx, driver.ListDeliveryEventRequest{
				TenantID:  tenantID,
				SortOrder: "asc",
				Next:      page1.Next,
				Start:     &startTime,
				Limit:     2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page2.Data)
		})
	})
}
