package drivertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// testPaginationTimePrecision walks records whose times are closer together
// than a millisecond. A cursor has to tell them apart as precisely as the
// store orders them, or pages skip or repeat records.
func testPaginationTimePrecision(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	tenantID := idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Add(-time.Hour).Truncate(time.Second)
	farPast := baseTime.Add(-48 * time.Hour)

	// Offsets from baseTime. The IDs are in a different order than the times, so
	// a store cannot get the order right from the ID alone.
	records := []struct {
		id     string
		offset time.Duration
	}{
		// alone in its millisecond, not on a millisecond boundary
		{"e", 1300 * time.Microsecond},
		// three records inside one millisecond
		{"h", 3100 * time.Microsecond},
		{"a", 3500 * time.Microsecond},
		{"f", 3900 * time.Microsecond},
		// three records at exactly the same time
		{"c", 5250 * time.Microsecond},
		{"g", 5250 * time.Microsecond},
		{"b", 5250 * time.Microsecond},
		// alone in its millisecond, not on a millisecond boundary
		{"d", 7700 * time.Microsecond},
	}

	entries := make([]*models.LogEntry, len(records))
	for i, r := range records {
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("evt_%s_%s", tenantID, r.id)),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTime(baseTime.Add(r.offset)),
		)
		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(fmt.Sprintf("att_%s_%s", tenantID, r.id)),
			testutil.AttemptFactory.WithTenantID(tenantID),
			testutil.AttemptFactory.WithEventID(event.ID),
			testutil.AttemptFactory.WithDestinationID(destinationID),
			testutil.AttemptFactory.WithTime(baseTime.Add(time.Second+r.offset)),
		)
		entries[i] = &models.LogEntry{Event: event, Attempt: attempt}
	}
	require.NoError(t, logStore.InsertMany(ctx, entries))
	require.NoError(t, h.FlushWrites(ctx))

	type page struct {
		ids        []string
		next, prev string
	}
	type listFunc func(order string, limit int, next, prev string) page

	listEvents := func(order string, limit int, next, prev string) page {
		res, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantIDs:  []string{tenantID},
			Limit:      limit,
			SortOrder:  order,
			Next:       next,
			Prev:       prev,
			TimeFilter: driver.TimeFilter{GTE: &farPast},
		})
		require.NoError(t, err)
		p := page{next: res.Next, prev: res.Prev}
		for _, e := range res.Data {
			p.ids = append(p.ids, e.ID)
		}
		return p
	}
	listAttempts := func(order string, limit int, next, prev string) page {
		res, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
			TenantIDs:  []string{tenantID},
			Limit:      limit,
			SortOrder:  order,
			Next:       next,
			Prev:       prev,
			TimeFilter: driver.TimeFilter{GTE: &farPast},
		})
		require.NoError(t, err)
		p := page{next: res.Next, prev: res.Prev}
		for _, ar := range res.Data {
			p.ids = append(p.ids, ar.Attempt.ID)
		}
		return p
	}

	// walk pages through every record with Next, then back to the first page
	// with Prev. Both ways have to give the order of a single unpaged list:
	// every record once, none skipped.
	walk := func(t *testing.T, list listFunc, order string, limit int) {
		want := list(order, 100, "", "").ids
		require.Len(t, want, len(records), "unpaged list should return every record")

		// More pages than this means the walk is not advancing.
		maxPages := len(records) + 1

		var forward []string
		p := list(order, limit, "", "")
		last := p
		for n := 1; ; n++ {
			require.LessOrEqual(t, n, maxPages, "walking with next does not end; got %v", forward)
			forward = append(forward, p.ids...)
			last = p
			if p.next == "" {
				break
			}
			p = list(order, limit, p.next, "")
		}
		require.Equal(t, want, forward, "walking with next")

		backward := last.ids
		p = last
		for n := 1; p.prev != ""; n++ {
			require.LessOrEqual(t, n, maxPages, "walking with prev does not end; got %v", backward)
			p = list(order, limit, "", p.prev)
			backward = append(append([]string{}, p.ids...), backward...)
		}
		require.Equal(t, want, backward, "walking back with prev")
	}

	resources := []struct {
		name string
		list listFunc
	}{
		{"ListEvent", listEvents},
		{"ListAttempt", listAttempts},
	}
	for _, r := range resources {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 2, 3} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", r.name, order, limit), func(t *testing.T) {
					walk(t, r.list, order, limit)
				})
			}
		}
	}
}
