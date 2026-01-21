package drivertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination/paginationtest"
	"github.com/stretchr/testify/require"
)

// testPagination tests cursor-based pagination using paginationtest.Suite.
func testPagination(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	baseTime := time.Now().Truncate(time.Second)
	farPast := baseTime.Add(-48 * time.Hour)

	t.Run("ListDelivery", func(t *testing.T) {
		var tenantID, destinationID, idPrefix string

		suite := paginationtest.Suite[*driver.DeliveryRecord]{
			Name: "ListDelivery",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				destinationID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *driver.DeliveryRecord {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)
				deliveryTime := eventTime.Add(100 * time.Millisecond)

				event := &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         tenantID,
					DestinationID:    destinationID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}

				delivery := &models.Delivery{
					ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
					TenantID:      tenantID,
					EventID:       event.ID,
					DestinationID: destinationID,
					Status:        "success",
					Time:          deliveryTime,
					Code:          "200",
				}

				return &driver.DeliveryRecord{
					Event:    event,
					Delivery: delivery,
				}
			},

			InsertMany: func(ctx context.Context, items []*driver.DeliveryRecord) error {
				events := make([]*models.Event, len(items))
				deliveries := make([]*models.Delivery, len(items))
				for i, dr := range items {
					events[i] = dr.Event
					deliveries[i] = dr.Delivery
				}
				return logStore.InsertMany(ctx, events, deliveries)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*driver.DeliveryRecord], error) {
				res, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
					TenantID:   tenantID,
					Limit:      opts.Limit,
					SortOrder:  opts.Order,
					Next:       opts.Next,
					Prev:       opts.Prev,
					TimeFilter: driver.TimeFilter{GTE: &farPast},
				})
				if err != nil {
					return paginationtest.ListResult[*driver.DeliveryRecord]{}, err
				}
				return paginationtest.ListResult[*driver.DeliveryRecord]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(dr *driver.DeliveryRecord) string {
				return dr.Delivery.ID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	t.Run("ListDelivery_WithDestinationFilter", func(t *testing.T) {
		var tenantID, targetDestID, otherDestID, idPrefix string

		suite := paginationtest.Suite[*driver.DeliveryRecord]{
			Name: "ListDelivery_WithDestinationFilter",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				targetDestID = idgen.Destination()
				otherDestID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *driver.DeliveryRecord {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)
				deliveryTime := eventTime.Add(100 * time.Millisecond)

				destID := targetDestID
				if i%2 == 1 {
					destID = otherDestID
				}

				event := &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         tenantID,
					DestinationID:    destID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}

				delivery := &models.Delivery{
					ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
					TenantID:      tenantID,
					EventID:       event.ID,
					DestinationID: destID,
					Status:        "success",
					Time:          deliveryTime,
					Code:          "200",
				}

				return &driver.DeliveryRecord{
					Event:    event,
					Delivery: delivery,
				}
			},

			InsertMany: func(ctx context.Context, items []*driver.DeliveryRecord) error {
				events := make([]*models.Event, len(items))
				deliveries := make([]*models.Delivery, len(items))
				for i, dr := range items {
					events[i] = dr.Event
					deliveries[i] = dr.Delivery
				}
				return logStore.InsertMany(ctx, events, deliveries)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*driver.DeliveryRecord], error) {
				res, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
					TenantID:       tenantID,
					DestinationIDs: []string{targetDestID},
					Limit:          opts.Limit,
					SortOrder:      opts.Order,
					Next:           opts.Next,
					Prev:           opts.Prev,
					TimeFilter:     driver.TimeFilter{GTE: &farPast},
				})
				if err != nil {
					return paginationtest.ListResult[*driver.DeliveryRecord]{}, err
				}
				return paginationtest.ListResult[*driver.DeliveryRecord]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(dr *driver.DeliveryRecord) string {
				return dr.Delivery.ID
			},

			Matches: func(dr *driver.DeliveryRecord) bool {
				return dr.Delivery.DestinationID == targetDestID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	t.Run("ListEvent", func(t *testing.T) {
		var eventTenantID, eventDestID, idPrefix string

		suite := paginationtest.Suite[*models.Event]{
			Name: "ListEvent",

			Cleanup: func(ctx context.Context) error {
				eventTenantID = idgen.String()
				eventDestID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *models.Event {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)

				return &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         eventTenantID,
					DestinationID:    eventDestID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}
			},

			InsertMany: func(ctx context.Context, items []*models.Event) error {
				deliveries := make([]*models.Delivery, len(items))
				for i, evt := range items {
					deliveryTime := evt.Time.Add(100 * time.Millisecond)
					deliveries[i] = &models.Delivery{
						ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
						TenantID:      evt.TenantID,
						EventID:       evt.ID,
						DestinationID: evt.DestinationID,
						Status:        "success",
						Time:          deliveryTime,
						Code:          "200",
					}
				}
				return logStore.InsertMany(ctx, items, deliveries)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*models.Event], error) {
				res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
					TenantID:   eventTenantID,
					Limit:      opts.Limit,
					SortOrder:  opts.Order,
					Next:       opts.Next,
					Prev:       opts.Prev,
					TimeFilter: driver.TimeFilter{GTE: &farPast},
				})
				if err != nil {
					return paginationtest.ListResult[*models.Event]{}, err
				}
				return paginationtest.ListResult[*models.Event]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(e *models.Event) string {
				return e.ID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	t.Run("ListEvent_WithDestinationFilter", func(t *testing.T) {
		var tenantID, targetDestID, otherDestID, idPrefix string

		suite := paginationtest.Suite[*models.Event]{
			Name: "ListEvent_WithDestinationFilter",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				targetDestID = idgen.Destination()
				otherDestID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *models.Event {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)

				destID := targetDestID
				if i%2 == 1 {
					destID = otherDestID
				}

				return &models.Event{
					ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
					TenantID:         tenantID,
					DestinationID:    destID,
					Topic:            "test.topic",
					EligibleForRetry: true,
					Time:             eventTime,
					Metadata:         map[string]string{},
					Data:             map[string]any{},
				}
			},

			InsertMany: func(ctx context.Context, items []*models.Event) error {
				deliveries := make([]*models.Delivery, len(items))
				for i, evt := range items {
					deliveryTime := evt.Time.Add(100 * time.Millisecond)
					deliveries[i] = &models.Delivery{
						ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
						TenantID:      evt.TenantID,
						EventID:       evt.ID,
						DestinationID: evt.DestinationID,
						Status:        "success",
						Time:          deliveryTime,
						Code:          "200",
					}
				}
				return logStore.InsertMany(ctx, items, deliveries)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*models.Event], error) {
				res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
					TenantID:       tenantID,
					DestinationIDs: []string{targetDestID},
					Limit:          opts.Limit,
					SortOrder:      opts.Order,
					Next:           opts.Next,
					Prev:           opts.Prev,
					TimeFilter:     driver.TimeFilter{GTE: &farPast},
				})
				if err != nil {
					return paginationtest.ListResult[*models.Event]{}, err
				}
				return paginationtest.ListResult[*models.Event]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(e *models.Event) string {
				return e.ID
			},

			Matches: func(e *models.Event) bool {
				return e.DestinationID == targetDestID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	// Test cursor pagination combined with time filters.
	// These tests verify that cursors work correctly when used alongside
	// time-based filters (GTE, LTE, GT, LT), which is critical for
	// "paginate within a time window" use cases.
	//
	// IMPORTANT: ListDelivery filters by DELIVERY time, ListEvent filters by EVENT time.
	// In this test, delivery_time = event_time + 100ms.
	t.Run("TimeFilterWithCursor", func(t *testing.T) {
		tenantID := idgen.String()
		destinationID := idgen.Destination()
		idPrefix := idgen.String()[:8]

		// Create 20 events with times spread across different ranges:
		// - Events 0-4: far past (should be excluded by GTE filter)
		// - Events 5-14: within time window (should be included)
		// - Events 15-19: far future (should be excluded by LTE filter)
		//
		// Event times are spaced 2 minutes apart within the window.
		// Delivery times are 1 second after event times (not sub-second)
		// to ensure GT/LT tests work consistently across databases.
		eventWindowStart := baseTime.Add(-10 * time.Minute)
		eventWindowEnd := baseTime.Add(10 * time.Minute)
		// Delivery window accounts for the 1 second offset
		deliveryWindowStart := eventWindowStart.Add(time.Second)
		deliveryWindowEnd := eventWindowEnd.Add(time.Second)

		var allRecords []*driver.DeliveryRecord
		var allEvents []*models.Event
		var allDeliveries []*models.Delivery
		for i := range 20 {
			var eventTime time.Time
			switch {
			case i < 5:
				// Far past: outside window (before eventWindowStart)
				eventTime = eventWindowStart.Add(-time.Duration(5-i) * time.Hour)
			case i < 15:
				// Within window: eventWindowStart to eventWindowEnd
				offset := time.Duration(i-5) * 2 * time.Minute
				eventTime = eventWindowStart.Add(offset)
			default:
				// Far future: outside window (after eventWindowEnd)
				eventTime = eventWindowEnd.Add(time.Duration(i-14) * time.Hour)
			}

			deliveryTime := eventTime.Add(time.Second)

			event := &models.Event{
				ID:               fmt.Sprintf("%s_evt_%03d", idPrefix, i),
				TenantID:         tenantID,
				DestinationID:    destinationID,
				Topic:            "test.topic",
				EligibleForRetry: true,
				Time:             eventTime,
				Metadata:         map[string]string{},
				Data:             map[string]any{},
			}
			delivery := &models.Delivery{
				ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
				TenantID:      tenantID,
				EventID:       event.ID,
				DestinationID: destinationID,
				Status:        "success",
				Time:          deliveryTime,
				Code:          "200",
			}
			allRecords = append(allRecords, &driver.DeliveryRecord{
				Event:    event,
				Delivery: delivery,
			})
			allEvents = append(allEvents, event)
			allDeliveries = append(allDeliveries, delivery)
		}

		require.NoError(t, logStore.InsertMany(ctx, allEvents, allDeliveries))
		require.NoError(t, h.FlushWrites(ctx))

		t.Run("paginate within time-bounded window", func(t *testing.T) {
			// Paginate through deliveries within the window with limit=3
			// ListDelivery filters by DELIVERY time, not event time.
			// Should only see deliveries 5-14 (10 total), not 0-4 or 15-19
			var collectedIDs []string
			var nextCursor string
			pageCount := 0

			for {
				res, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
					TenantID:   tenantID,
					Limit:      3,
					SortOrder:  "asc",
					Next:       nextCursor,
					TimeFilter: driver.TimeFilter{GTE: &deliveryWindowStart, LTE: &deliveryWindowEnd},
				})
				require.NoError(t, err)

				for _, dr := range res.Data {
					collectedIDs = append(collectedIDs, dr.Event.ID)
				}

				pageCount++
				if res.Next == "" {
					break
				}
				nextCursor = res.Next

				// Safety: prevent infinite loop
				if pageCount > 10 {
					t.Fatal("too many pages")
				}
			}

			// Should have collected exactly deliveries 5-14
			require.Len(t, collectedIDs, 10, "should have 10 deliveries in window")
			for i, id := range collectedIDs {
				expectedID := fmt.Sprintf("%s_evt_%03d", idPrefix, i+5)
				require.Equal(t, expectedID, id, "delivery %d mismatch", i)
			}
			require.Equal(t, 4, pageCount, "should take 4 pages (3+3+3+1)")
		})

		t.Run("cursor excludes deliveries outside time filter", func(t *testing.T) {
			// First page with no time filter gets all deliveries
			resAll, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      5,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &farPast},
			})
			require.NoError(t, err)
			require.Len(t, resAll.Data, 5)

			// Use the cursor but add a time filter that excludes some results
			// The cursor points to position after delivery 4 (far past deliveries)
			// But with deliveryWindowStart filter, we should start from delivery 5
			res, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      5,
				SortOrder:  "asc",
				Next:       resAll.Next,
				TimeFilter: driver.TimeFilter{GTE: &deliveryWindowStart, LTE: &deliveryWindowEnd},
			})
			require.NoError(t, err)

			// Results should respect the time filter (on delivery time)
			for _, dr := range res.Data {
				require.True(t, !dr.Delivery.Time.Before(deliveryWindowStart), "delivery time should be >= deliveryWindowStart")
				require.True(t, !dr.Delivery.Time.After(deliveryWindowEnd), "delivery time should be <= deliveryWindowEnd")
			}
		})

		t.Run("delivery time filter with GT/LT operators", func(t *testing.T) {
			// Test exclusive bounds (GT/LT instead of GTE/LTE) on delivery time
			// Use delivery times slightly after delivery 5 and slightly before delivery 14
			gtTime := allRecords[5].Delivery.Time.Add(time.Second)   // After delivery 5, before delivery 6
			ltTime := allRecords[14].Delivery.Time.Add(-time.Second) // Before delivery 14, after delivery 13

			var collectedIDs []string
			var nextCursor string

			for {
				res, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
					TenantID:   tenantID,
					Limit:      3,
					SortOrder:  "asc",
					Next:       nextCursor,
					TimeFilter: driver.TimeFilter{GT: &gtTime, LT: &ltTime},
				})
				require.NoError(t, err)

				for _, dr := range res.Data {
					collectedIDs = append(collectedIDs, dr.Event.ID)
				}

				if res.Next == "" {
					break
				}
				nextCursor = res.Next
			}

			// Should have events 6-13 (8 events)
			require.Len(t, collectedIDs, 8, "should have 8 events in GT/LT range")
			for i, id := range collectedIDs {
				expectedID := fmt.Sprintf("%s_evt_%03d", idPrefix, i+6)
				require.Equal(t, expectedID, id, "event %d mismatch", i)
			}
		})

		t.Run("GT/LT exclude exact timestamp", func(t *testing.T) {
			// Verify that GT excludes the exact timestamp (not >=)
			// and LT excludes the exact timestamp (not <=).
			//
			// We truncate times to second precision to ensure consistent
			// comparison across databases with different timestamp precision
			// (PostgreSQL microseconds, ClickHouse DateTime64, etc.).
			//
			// Important: ListDelivery filters by DELIVERY time, not event time.

			// First, retrieve all deliveries to find delivery 10's time
			res, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:  tenantID,
				Limit:     100,
				SortOrder: "asc",
				TimeFilter: driver.TimeFilter{
					GTE: &farPast,
				},
			})
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(res.Data), 11, "need at least 11 deliveries")

			// Find delivery 10's stored delivery time, truncated to seconds
			var storedDelivery10Time time.Time
			for _, dr := range res.Data {
				if dr.Event.ID == allRecords[10].Event.ID {
					storedDelivery10Time = dr.Delivery.Time.Truncate(time.Second)
					break
				}
			}
			require.False(t, storedDelivery10Time.IsZero(), "should find delivery 10")

			// GT with exact time should exclude delivery 10
			resGT, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GT: &storedDelivery10Time},
			})
			require.NoError(t, err)

			for _, dr := range resGT.Data {
				drTimeTrunc := dr.Delivery.Time.Truncate(time.Second)
				require.True(t, drTimeTrunc.After(storedDelivery10Time),
					"GT filter should exclude delivery with exact timestamp, got delivery %s with time %v (filter time: %v)",
					dr.Delivery.ID, drTimeTrunc, storedDelivery10Time)
			}

			// LT with exact time should exclude delivery 10
			resLT, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{LT: &storedDelivery10Time},
			})
			require.NoError(t, err)

			for _, dr := range resLT.Data {
				drTimeTrunc := dr.Delivery.Time.Truncate(time.Second)
				require.True(t, drTimeTrunc.Before(storedDelivery10Time),
					"LT filter should exclude delivery with exact timestamp, got delivery %s with time %v (filter time: %v)",
					dr.Delivery.ID, drTimeTrunc, storedDelivery10Time)
			}

			// Verify delivery 10 is included with GTE/LTE (inclusive bounds)
			resGTE, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &storedDelivery10Time, LTE: &storedDelivery10Time},
			})
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(resGTE.Data), 1, "GTE/LTE with same time should include delivery at that second")
		})

		t.Run("prev cursor respects time filter", func(t *testing.T) {
			// Get first page (ListDelivery filters by delivery time)
			res1, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      3,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &deliveryWindowStart, LTE: &deliveryWindowEnd},
			})
			require.NoError(t, err)
			require.NotEmpty(t, res1.Next)

			// Get second page
			res2, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      3,
				SortOrder:  "asc",
				Next:       res1.Next,
				TimeFilter: driver.TimeFilter{GTE: &deliveryWindowStart, LTE: &deliveryWindowEnd},
			})
			require.NoError(t, err)
			require.NotEmpty(t, res2.Prev)

			// Go back to first page using prev cursor
			resPrev, err := logStore.ListDelivery(ctx, driver.ListDeliveryRequest{
				TenantID:   tenantID,
				Limit:      3,
				SortOrder:  "asc",
				Prev:       res2.Prev,
				TimeFilter: driver.TimeFilter{GTE: &deliveryWindowStart, LTE: &deliveryWindowEnd},
			})
			require.NoError(t, err)

			// Should get same results as first page
			require.Len(t, resPrev.Data, len(res1.Data))
			for i := range res1.Data {
				require.Equal(t, res1.Data[i].Event.ID, resPrev.Data[i].Event.ID)
			}
		})

		t.Run("ListEvent with time filter pagination", func(t *testing.T) {
			// Same test pattern for ListEvent
			var collectedIDs []string
			var nextCursor string
			pageCount := 0

			for {
				res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
					TenantID:   tenantID,
					Limit:      3,
					SortOrder:  "asc",
					Next:       nextCursor,
					TimeFilter: driver.TimeFilter{GTE: &eventWindowStart, LTE: &eventWindowEnd},
				})
				require.NoError(t, err)

				for _, e := range res.Data {
					collectedIDs = append(collectedIDs, e.ID)
				}

				pageCount++
				if res.Next == "" {
					break
				}
				nextCursor = res.Next

				if pageCount > 10 {
					t.Fatal("too many pages")
				}
			}

			// Should have collected exactly events 5-14
			require.Len(t, collectedIDs, 10, "should have 10 events in window")
			for i, id := range collectedIDs {
				expectedID := fmt.Sprintf("%s_evt_%03d", idPrefix, i+5)
				require.Equal(t, expectedID, id, "event %d mismatch", i)
			}
			require.Equal(t, 4, pageCount, "should take 4 pages (3+3+3+1)")
		})

		t.Run("ListEvent GT/LT exclude exact timestamp", func(t *testing.T) {
			// Verify that GT excludes the exact timestamp (not >=)
			// and LT excludes the exact timestamp (not <=).
			//
			// We truncate times to second precision to ensure consistent
			// comparison across databases with different timestamp precision.
			//
			// ListEvent filters by EVENT time.

			// First, retrieve event 10's stored time from the database
			res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:  tenantID,
				Limit:     100,
				SortOrder: "asc",
				TimeFilter: driver.TimeFilter{
					GTE: &farPast,
				},
			})
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(res.Data), 11, "need at least 11 events")

			// Find event 10's stored event time, truncated to seconds
			var storedEvent10Time time.Time
			for _, e := range res.Data {
				if e.ID == allRecords[10].Event.ID {
					storedEvent10Time = e.Time.Truncate(time.Second)
					break
				}
			}
			require.False(t, storedEvent10Time.IsZero(), "should find event 10")

			// GT with exact time should exclude event 10
			resGT, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GT: &storedEvent10Time},
			})
			require.NoError(t, err)

			for _, e := range resGT.Data {
				eTimeTrunc := e.Time.Truncate(time.Second)
				require.True(t, eTimeTrunc.After(storedEvent10Time),
					"GT filter should exclude event with exact timestamp, got event %s with time %v (filter time: %v)",
					e.ID, eTimeTrunc, storedEvent10Time)
			}

			// LT with exact time should exclude event 10
			resLT, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{LT: &storedEvent10Time},
			})
			require.NoError(t, err)

			for _, e := range resLT.Data {
				eTimeTrunc := e.Time.Truncate(time.Second)
				require.True(t, eTimeTrunc.Before(storedEvent10Time),
					"LT filter should exclude event with exact timestamp, got event %s with time %v (filter time: %v)",
					e.ID, eTimeTrunc, storedEvent10Time)
			}

			// Verify event 10 is included with GTE/LTE (inclusive bounds)
			resGTE, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &storedEvent10Time, LTE: &storedEvent10Time},
			})
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(resGTE.Data), 1, "GTE/LTE with same time should include event at that second")
		})
	})
}
