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
	"github.com/stretchr/testify/assert"
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

	t.Run("ListAttempt", func(t *testing.T) {
		var tenantID, destinationID, idPrefix string

		suite := paginationtest.Suite[*driver.AttemptRecord]{
			Name: "ListAttempt",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				destinationID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *driver.AttemptRecord {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)
				attemptTime := eventTime.Add(100 * time.Millisecond)

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

				attempt := &models.Attempt{
					ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
					TenantID:      tenantID,
					EventID:       event.ID,
					DestinationID: destinationID,
					Status:        "success",
					Time:          attemptTime,
					Code:          "200",
				}

				return &driver.AttemptRecord{
					Event:   event,
					Attempt: attempt,
				}
			},

			InsertMany: func(ctx context.Context, items []*driver.AttemptRecord) error {
				entries := make([]*models.LogEntry, len(items))
				for i, dr := range items {
					entries[i] = &models.LogEntry{Event: dr.Event, Attempt: dr.Attempt}
				}
				return logStore.InsertMany(ctx, entries)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*driver.AttemptRecord], error) {
				res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
					TenantID:   tenantID,
					Limit:      opts.Limit,
					SortOrder:  opts.Order,
					Next:       opts.Next,
					Prev:       opts.Prev,
					TimeFilter: driver.TimeFilter{GTE: &farPast},
				})
				if err != nil {
					return paginationtest.ListResult[*driver.AttemptRecord]{}, err
				}
				return paginationtest.ListResult[*driver.AttemptRecord]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(dr *driver.AttemptRecord) string {
				return dr.Attempt.ID
			},

			AfterInsert: func(ctx context.Context) error {
				return h.FlushWrites(ctx)
			},
		}

		suite.Run(t)
	})

	t.Run("ListAttempt_WithDestinationFilter", func(t *testing.T) {
		var tenantID, targetDestID, otherDestID, idPrefix string

		suite := paginationtest.Suite[*driver.AttemptRecord]{
			Name: "ListAttempt_WithDestinationFilter",

			Cleanup: func(ctx context.Context) error {
				tenantID = idgen.String()
				targetDestID = idgen.Destination()
				otherDestID = idgen.Destination()
				idPrefix = idgen.String()[:8]
				return nil
			},

			NewItem: func(i int) *driver.AttemptRecord {
				eventTime := baseTime.Add(time.Duration(i) * time.Second)
				attemptTime := eventTime.Add(100 * time.Millisecond)

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

				attempt := &models.Attempt{
					ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
					TenantID:      tenantID,
					EventID:       event.ID,
					DestinationID: destID,
					Status:        "success",
					Time:          attemptTime,
					Code:          "200",
				}

				return &driver.AttemptRecord{
					Event:   event,
					Attempt: attempt,
				}
			},

			InsertMany: func(ctx context.Context, items []*driver.AttemptRecord) error {
				entries := make([]*models.LogEntry, len(items))
				for i, dr := range items {
					entries[i] = &models.LogEntry{Event: dr.Event, Attempt: dr.Attempt}
				}
				return logStore.InsertMany(ctx, entries)
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[*driver.AttemptRecord], error) {
				res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
					TenantID:       tenantID,
					DestinationIDs: []string{targetDestID},
					Limit:          opts.Limit,
					SortOrder:      opts.Order,
					Next:           opts.Next,
					Prev:           opts.Prev,
					TimeFilter:     driver.TimeFilter{GTE: &farPast},
				})
				if err != nil {
					return paginationtest.ListResult[*driver.AttemptRecord]{}, err
				}
				return paginationtest.ListResult[*driver.AttemptRecord]{
					Items: res.Data,
					Next:  res.Next,
					Prev:  res.Prev,
				}, nil
			},

			GetID: func(dr *driver.AttemptRecord) string {
				return dr.Attempt.ID
			},

			Matches: func(dr *driver.AttemptRecord) bool {
				return dr.Attempt.DestinationID == targetDestID
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
				entries := make([]*models.LogEntry, len(items))
				for i, evt := range items {
					attemptTime := evt.Time.Add(100 * time.Millisecond)
					entries[i] = &models.LogEntry{
						Event: evt,
						Attempt: &models.Attempt{
							ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
							TenantID:      evt.TenantID,
							EventID:       evt.ID,
							DestinationID: evt.DestinationID,
							Status:        "success",
							Time:          attemptTime,
							Code:          "200",
						},
					}
				}
				return logStore.InsertMany(ctx, entries)
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

	// ListEvent with DestinationIDs filter returns unimplemented error.
	// Events are destination-agnostic. The destination_id on events represents the
	// publish input, not matched destinations. To filter by destination, use
	// ListAttempt which queries actual delivery attempts.
	t.Run("ListEvent_WithDestinationFilter_ReturnsError", func(t *testing.T) {
		tenantID := idgen.String()
		destID := idgen.Destination()

		_, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantID:       tenantID,
			DestinationIDs: []string{destID},
			Limit:          10,
			TimeFilter:     driver.TimeFilter{GTE: &farPast},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not implemented")
	})

	t.Run("ListEvent_WithDestinationFilter", func(t *testing.T) {
		// TODO(list-event-destination-filter): Re-enable once we implement proper destination tracking for events.
		t.Skip("ListEvent with DestinationIDs filter is not implemented")

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
				entries := make([]*models.LogEntry, len(items))
				for i, evt := range items {
					attemptTime := evt.Time.Add(100 * time.Millisecond)
					entries[i] = &models.LogEntry{
						Event: evt,
						Attempt: &models.Attempt{
							ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
							TenantID:      evt.TenantID,
							EventID:       evt.ID,
							DestinationID: evt.DestinationID,
							Status:        "success",
							Time:          attemptTime,
							Code:          "200",
						},
					}
				}
				return logStore.InsertMany(ctx, entries)
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
	// IMPORTANT: ListAttempt filters by ATTEMPT time, ListEvent filters by EVENT time.
	// In this test, attempt_time = event_time + 100ms.
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
		// Attempt times are 1 second after event times (not sub-second)
		// to ensure GT/LT tests work consistently across databases.
		eventWindowStart := baseTime.Add(-10 * time.Minute)
		eventWindowEnd := baseTime.Add(10 * time.Minute)
		// Attempt window accounts for the 1 second offset
		attemptWindowStart := eventWindowStart.Add(time.Second)
		attemptWindowEnd := eventWindowEnd.Add(time.Second)

		var allRecords []*driver.AttemptRecord
		var allEvents []*models.Event
		var allAttempts []*models.Attempt
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

			attemptTime := eventTime.Add(time.Second)

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
			attempt := &models.Attempt{
				ID:            fmt.Sprintf("%s_del_%03d", idPrefix, i),
				TenantID:      tenantID,
				EventID:       event.ID,
				DestinationID: destinationID,
				Status:        "success",
				Time:          attemptTime,
				Code:          "200",
			}
			allRecords = append(allRecords, &driver.AttemptRecord{
				Event:   event,
				Attempt: attempt,
			})
			allEvents = append(allEvents, event)
			allAttempts = append(allAttempts, attempt)
		}

		entries := make([]*models.LogEntry, len(allEvents))
		for i := range allEvents {
			entries[i] = &models.LogEntry{Event: allEvents[i], Attempt: allAttempts[i]}
		}
		require.NoError(t, logStore.InsertMany(ctx, entries))
		require.NoError(t, h.FlushWrites(ctx))

		t.Run("paginate within time-bounded window", func(t *testing.T) {
			// Paginate through attempts within the window with limit=3
			// ListAttempt filters by ATTEMPT time, not event time.
			// Should only see attempts 5-14 (10 total), not 0-4 or 15-19
			var collectedIDs []string
			var nextCursor string
			pageCount := 0

			for {
				res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
					TenantID:   tenantID,
					Limit:      3,
					SortOrder:  "asc",
					Next:       nextCursor,
					TimeFilter: driver.TimeFilter{GTE: &attemptWindowStart, LTE: &attemptWindowEnd},
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

			// Should have collected exactly attempts 5-14
			require.Len(t, collectedIDs, 10, "should have 10 attempts in window")
			for i, id := range collectedIDs {
				expectedID := fmt.Sprintf("%s_evt_%03d", idPrefix, i+5)
				require.Equal(t, expectedID, id, "attempt %d mismatch", i)
			}
			require.Equal(t, 4, pageCount, "should take 4 pages (3+3+3+1)")
		})

		t.Run("cursor excludes attempts outside time filter", func(t *testing.T) {
			// First page with no time filter gets all attempts
			resAll, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      5,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &farPast},
			})
			require.NoError(t, err)
			require.Len(t, resAll.Data, 5)

			// Use the cursor but add a time filter that excludes some results
			// The cursor points to position after attempt 4 (far past attempts)
			// But with attemptWindowStart filter, we should start from attempt 5
			res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      5,
				SortOrder:  "asc",
				Next:       resAll.Next,
				TimeFilter: driver.TimeFilter{GTE: &attemptWindowStart, LTE: &attemptWindowEnd},
			})
			require.NoError(t, err)

			// Results should respect the time filter (on attempt time)
			for _, dr := range res.Data {
				require.True(t, !dr.Attempt.Time.Before(attemptWindowStart), "attempt time should be >= attemptWindowStart")
				require.True(t, !dr.Attempt.Time.After(attemptWindowEnd), "attempt time should be <= attemptWindowEnd")
			}
		})

		t.Run("attempt time filter with GT/LT operators", func(t *testing.T) {
			// Test exclusive bounds (GT/LT instead of GTE/LTE) on attempt time
			// Use attempt times slightly after attempt 5 and slightly before attempt 14
			gtTime := allRecords[5].Attempt.Time.Add(time.Second)   // After attempt 5, before attempt 6
			ltTime := allRecords[14].Attempt.Time.Add(-time.Second) // Before attempt 14, after attempt 13

			var collectedIDs []string
			var nextCursor string

			for {
				res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
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
			// Important: ListAttempt filters by ATTEMPT time, not event time.

			// First, retrieve all attempts to find attempt 10's time
			res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:  tenantID,
				Limit:     100,
				SortOrder: "asc",
				TimeFilter: driver.TimeFilter{
					GTE: &farPast,
				},
			})
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(res.Data), 11, "need at least 11 attempts")

			// Find attempt 10's stored attempt time, truncated to seconds
			var storedAttempt10Time time.Time
			for _, dr := range res.Data {
				if dr.Event.ID == allRecords[10].Event.ID {
					storedAttempt10Time = dr.Attempt.Time.Truncate(time.Second)
					break
				}
			}
			require.False(t, storedAttempt10Time.IsZero(), "should find attempt 10")

			// GT with exact time should exclude attempt 10
			resGT, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GT: &storedAttempt10Time},
			})
			require.NoError(t, err)

			for _, dr := range resGT.Data {
				drTimeTrunc := dr.Attempt.Time.Truncate(time.Second)
				require.True(t, drTimeTrunc.After(storedAttempt10Time),
					"GT filter should exclude attempt with exact timestamp, got attempt %s with time %v (filter time: %v)",
					dr.Attempt.ID, drTimeTrunc, storedAttempt10Time)
			}

			// LT with exact time should exclude attempt 10
			resLT, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{LT: &storedAttempt10Time},
			})
			require.NoError(t, err)

			for _, dr := range resLT.Data {
				drTimeTrunc := dr.Attempt.Time.Truncate(time.Second)
				require.True(t, drTimeTrunc.Before(storedAttempt10Time),
					"LT filter should exclude attempt with exact timestamp, got attempt %s with time %v (filter time: %v)",
					dr.Attempt.ID, drTimeTrunc, storedAttempt10Time)
			}

			// Verify attempt 10 is included with GTE/LTE (inclusive bounds)
			resGTE, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      100,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &storedAttempt10Time, LTE: &storedAttempt10Time},
			})
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(resGTE.Data), 1, "GTE/LTE with same time should include attempt at that second")
		})

		t.Run("prev cursor respects time filter", func(t *testing.T) {
			// Get first page (ListAttempt filters by attempt time)
			res1, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      3,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &attemptWindowStart, LTE: &attemptWindowEnd},
			})
			require.NoError(t, err)
			require.NotEmpty(t, res1.Next)

			// Get second page
			res2, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      3,
				SortOrder:  "asc",
				Next:       res1.Next,
				TimeFilter: driver.TimeFilter{GTE: &attemptWindowStart, LTE: &attemptWindowEnd},
			})
			require.NoError(t, err)
			require.NotEmpty(t, res2.Prev)

			// Go back to first page using prev cursor
			resPrev, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      3,
				SortOrder:  "asc",
				Prev:       res2.Prev,
				TimeFilter: driver.TimeFilter{GTE: &attemptWindowStart, LTE: &attemptWindowEnd},
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
