package drivertest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/cursor"
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
	attempt1 := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithID("tenant1-delivery"),
		testutil.AttemptFactory.WithEventID(event1.ID),
		testutil.AttemptFactory.WithDestinationID(destinationID),
		testutil.AttemptFactory.WithStatus("success"),
		testutil.AttemptFactory.WithTime(baseTime.Add(-10*time.Minute)),
	)

	event2 := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("tenant2-event"),
		testutil.EventFactory.WithTenantID(tenant2ID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("test.topic"),
		testutil.EventFactory.WithTime(baseTime.Add(-5*time.Minute)),
	)
	attempt2 := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithID("tenant2-delivery"),
		testutil.AttemptFactory.WithEventID(event2.ID),
		testutil.AttemptFactory.WithDestinationID(destinationID),
		testutil.AttemptFactory.WithStatus("failed"),
		testutil.AttemptFactory.WithTime(baseTime.Add(-5*time.Minute)),
	)

	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		{Event: event1, Attempt: attempt1},
		{Event: event2, Attempt: attempt2},
	}))
	require.NoError(t, h.FlushWrites(ctx))

	t.Run("TenantIsolation", func(t *testing.T) {
		t.Run("ListAttempt isolates by tenant", func(t *testing.T) {
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenant1ID,
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.Equal(t, "tenant1-event", response.Data[0].Event.ID)

			response, err = logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenant2ID,
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
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
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
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

		t.Run("ListAttempt returns all tenants when TenantID empty", func(t *testing.T) {
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:       "",
				DestinationIDs: []string{destinationID},
				Limit:          100,
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
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

		t.Run("RetrieveAttempt finds attempt across tenants when TenantID empty", func(t *testing.T) {
			retrieved1, err := logStore.RetrieveAttempt(ctx, driver.RetrieveAttemptRequest{
				TenantID:  "",
				AttemptID: "tenant1-delivery",
			})
			require.NoError(t, err)
			require.NotNil(t, retrieved1)
			assert.Equal(t, tenant1ID, retrieved1.Event.TenantID)

			retrieved2, err := logStore.RetrieveAttempt(ctx, driver.RetrieveAttemptRequest{
				TenantID:  "",
				AttemptID: "tenant2-delivery",
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

		var entries []*models.LogEntry
		for i := range 3 {
			event := testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID(fmt.Sprintf("sort_evt_%d", i)),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithDestinationID(destinationID),
				testutil.EventFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
			)
			attempt := testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID(fmt.Sprintf("sort_del_%d", i)),
				testutil.AttemptFactory.WithTenantID(tenantID),
				testutil.AttemptFactory.WithEventID(event.ID),
				testutil.AttemptFactory.WithDestinationID(destinationID),
				testutil.AttemptFactory.WithTime(baseTime.Add(-time.Duration(i)*time.Hour)),
			)
			entries = append(entries, &models.LogEntry{Event: event, Attempt: attempt})
		}
		require.NoError(t, logStore.InsertMany(ctx, entries))

		startTime := baseTime.Add(-48 * time.Hour)

		t.Run("invalid SortOrder uses default (desc)", func(t *testing.T) {
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				SortOrder:  "sideways",
				TimeFilter: driver.TimeFilter{GTE: &startTime},
				Limit:      10,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 3)
			assert.Equal(t, "sort_del_0", response.Data[0].Attempt.ID)
			assert.Equal(t, "sort_del_2", response.Data[2].Attempt.ID)
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
		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithTenantID(tenantID),
			testutil.AttemptFactory.WithEventID(event.ID),
			testutil.AttemptFactory.WithDestinationID(destinationID),
		)
		require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{{Event: event, Attempt: attempt}}))

		t.Run("nil DestinationIDs equals empty DestinationIDs", func(t *testing.T) {
			responseNil, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:       tenantID,
				DestinationIDs: nil,
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
				Limit:          10,
			})
			require.NoError(t, err)

			responseEmpty, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:       tenantID,
				DestinationIDs: []string{},
				TimeFilter:     driver.TimeFilter{GTE: &startTime},
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
			attempt := testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID(fmt.Sprintf("del_%s", evt.ID)),
				testutil.AttemptFactory.WithTenantID(tenantID),
				testutil.AttemptFactory.WithEventID(evt.ID),
				testutil.AttemptFactory.WithDestinationID(destinationID),
				testutil.AttemptFactory.WithTime(evt.Time),
			)
			require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{{Event: evt, Attempt: attempt}}))
		}

		t.Run("GTE is inclusive (>=)", func(t *testing.T) {
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				TimeFilter: driver.TimeFilter{GTE: &boundaryTime},
				Limit:      10,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
		})

		t.Run("LTE is inclusive (<=)", func(t *testing.T) {
			farPast := boundaryTime.Add(-1 * time.Hour)
			response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				TimeFilter: driver.TimeFilter{GTE: &farPast, LTE: &boundaryTime},
				Limit:      10,
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
		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithTenantID(tenantID),
			testutil.AttemptFactory.WithEventID(event.ID),
			testutil.AttemptFactory.WithDestinationID(destinationID),
		)
		require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{{Event: event, Attempt: attempt}}))

		t.Run("modifying ListAttempt result doesn't affect subsequent queries", func(t *testing.T) {
			response1, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      10,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, response1.Data, 1)

			originalID := response1.Data[0].Event.ID
			response1.Data[0].Event.ID = "MODIFIED"

			response2, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				Limit:      10,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
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
		attemptTime := eventTime.Add(1 * time.Second)
		startTime := eventTime.Add(-1 * time.Hour)

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(eventTime),
		)
		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithTenantID(tenantID),
			testutil.AttemptFactory.WithEventID(event.ID),
			testutil.AttemptFactory.WithDestinationID(destinationID),
			testutil.AttemptFactory.WithStatus("success"),
			testutil.AttemptFactory.WithTime(attemptTime),
		)
		entries := []*models.LogEntry{{Event: event, Attempt: attempt}}

		// Race N goroutines all inserting the same record
		const numGoroutines = 10
		var wg sync.WaitGroup
		for range numGoroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = logStore.InsertMany(ctx, entries)
			}()
		}
		wg.Wait()
		require.NoError(t, h.FlushWrites(ctx))

		// Assert: still exactly 1 record
		response, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
			TenantID:   tenantID,
			Limit:      100,
			TimeFilter: driver.TimeFilter{GTE: &startTime},
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
				_, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
					TenantID:   tenantID,
					SortOrder:  "desc",
					Next:       tc.cursor,
					TimeFilter: driver.TimeFilter{GTE: &startTime},
					Limit:      10,
				})
				require.Error(t, err)
				assert.True(t, errors.Is(err, cursor.ErrInvalidCursor), "expected cursor.ErrInvalidCursor, got: %v", err)
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
			attempt := testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID(fmt.Sprintf("cursor_del_%d", i)),
				testutil.AttemptFactory.WithTenantID(tenantID),
				testutil.AttemptFactory.WithEventID(event.ID),
				testutil.AttemptFactory.WithDestinationID(destinationID),
				testutil.AttemptFactory.WithTime(baseTime.Add(time.Duration(i)*time.Second)),
			)
			require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{{Event: event, Attempt: attempt}}))
		}
		require.NoError(t, h.FlushWrites(ctx))

		t.Run("delivery_time desc", func(t *testing.T) {
			page1, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				SortOrder:  "desc",
				TimeFilter: driver.TimeFilter{GTE: &startTime},
				Limit:      2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page1.Next)

			page2, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				SortOrder:  "desc",
				Next:       page1.Next,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
				Limit:      2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page2.Data)
		})

		t.Run("delivery_time asc", func(t *testing.T) {
			page1, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				SortOrder:  "asc",
				TimeFilter: driver.TimeFilter{GTE: &startTime},
				Limit:      2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page1.Next)

			page2, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantID:   tenantID,
				SortOrder:  "asc",
				Next:       page1.Next,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
				Limit:      2,
			})
			require.NoError(t, err)
			require.NotEmpty(t, page2.Data)
		})
	})
}
