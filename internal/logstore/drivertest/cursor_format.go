package drivertest

import (
	"context"
	"fmt"
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

// RunCursorFormatTest is for log stores that use driver.Cursor. A store reads
// a cursor of every format, whichever one it issues: a cursor issued by another
// version of Outpost keeps working, and a client in the middle of a list does
// not have to start over.
func RunCursorFormatTest(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	baseTime := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	var entries []*models.LogEntry
	for i := range 3 {
		eventID := fmt.Sprintf("evt_%s_%d", tenantID, i)
		entries = append(entries, &models.LogEntry{
			Event: testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID(eventID),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithTime(baseTime.Add(time.Duration(i)*time.Second)),
			),
			Attempt: testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID(fmt.Sprintf("att_%s_%d", tenantID, i)),
				testutil.AttemptFactory.WithTenantID(tenantID),
				testutil.AttemptFactory.WithEventID(eventID),
				testutil.AttemptFactory.WithTime(baseTime.Add(time.Duration(i)*time.Second)),
			),
		})
	}
	require.NoError(t, logStore.InsertMany(ctx, entries))
	require.NoError(t, h.FlushWrites(ctx))

	second, third := entries[1], entries[2]
	formats := []struct {
		name    string
		version int
		time    func(time.Time) int64
	}{
		{"milliseconds", int(driver.CursorFormatTimeIDMillis), time.Time.UnixMilli},
		{"microseconds", int(driver.CursorFormatTimeIDMicros), time.Time.UnixMicro},
	}
	for _, format := range formats {
		// The position of the second record, written by hand in this format.
		eventCursor := cursor.Encode("evt", format.version, fmt.Sprintf("%d::%s", format.time(second.Event.Time), second.Event.ID))
		attemptCursor := cursor.Encode("att", format.version, fmt.Sprintf("%d::%s", format.time(second.Attempt.Time), second.Attempt.ID))

		t.Run("ListEvent/"+format.name, func(t *testing.T) {
			res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantIDs: []string{tenantID}, SortOrder: "asc", Limit: 10, Next: eventCursor,
			})
			require.NoError(t, err)
			require.Len(t, res.Data, 1)
			assert.Equal(t, third.Event.ID, res.Data[0].ID)
		})

		t.Run("ListAttempt/"+format.name, func(t *testing.T) {
			res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs: []string{tenantID}, SortOrder: "asc", Limit: 10, Next: attemptCursor,
			})
			require.NoError(t, err)
			require.Len(t, res.Data, 1)
			assert.Equal(t, third.Attempt.ID, res.Data[0].Attempt.ID)
		})
	}
}
