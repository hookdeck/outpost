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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testListLatestAttempts(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	tenantID := idgen.String()
	destA := idgen.Destination()
	destB := idgen.Destination()
	t0 := time.Now().Truncate(time.Second).Add(-30*time.Minute + 123456789*time.Nanosecond)

	// bounds carry the skew the retry lookup applies for stored time precision
	lo := t0.Add(-time.Second)

	newEvent := func(id string) *models.Event {
		return testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(id),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destA),
			testutil.EventFactory.WithMatchedDestinationIDs([]string{destA, destB}),
			testutil.EventFactory.WithTime(t0),
		)
	}
	newAttempt := func(id string, event *models.Event, destID string, n int, at time.Time) *models.LogEntry {
		return &models.LogEntry{
			Event: event,
			Attempt: testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID(id),
				testutil.AttemptFactory.WithTenantID(tenantID),
				testutil.AttemptFactory.WithEventID(event.ID),
				testutil.AttemptFactory.WithDestinationID(destID),
				testutil.AttemptFactory.WithAttemptNumber(n),
				testutil.AttemptFactory.WithStatus("failed"),
				testutil.AttemptFactory.WithTime(at),
			),
		}
	}

	multi := newEvent("latest_multi")
	multiB := newEvent("latest_multi_b")
	single := newEvent("latest_single")
	var entries []*models.LogEntry
	for i := 0; i < 3; i++ {
		entries = append(entries, newAttempt(fmt.Sprintf("latest_multi_%d", i), multi, destA, i+1, t0.Add(time.Duration(i)*time.Minute)))
	}
	entries = append(entries,
		newAttempt("latest_multi_other_dest", multi, destB, 1, t0.Add(10*time.Minute)),
		newAttempt("latest_multi_b_0", multiB, destA, 1, t0),
		newAttempt("latest_single_0", single, destA, 1, t0.Add(5*time.Minute)),
		// same attempt written twice: must come back once
		newAttempt("latest_single_0", single, destA, 1, t0.Add(5*time.Minute)),
	)
	require.NoError(t, logStore.InsertMany(ctx, entries))
	require.NoError(t, h.FlushWrites(ctx))

	key := func(eventID, destID string) driver.AttemptKey {
		return driver.AttemptKey{TenantID: tenantID, EventID: eventID, DestinationID: destID}
	}
	lookup := func(k driver.AttemptKey, notBefore time.Time) driver.LatestAttemptLookup {
		return driver.LatestAttemptLookup{Key: k, NotBefore: notBefore}
	}

	t.Run("ListLatestAttempts", func(t *testing.T) {
		t.Run("latest per key, destinations not cross-matched", func(t *testing.T) {
			got, err := logStore.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{
				lookup(key(multi.ID, destA), lo),
				lookup(key(multi.ID, destB), lo),
				lookup(key(multiB.ID, destA), lo),
				lookup(key(single.ID, destA), lo),
			})
			require.NoError(t, err)
			require.Len(t, got, 4)
			assert.Equal(t, "latest_multi_2", got[key(multi.ID, destA)].Attempt.ID)
			assert.Equal(t, "latest_multi_other_dest", got[key(multi.ID, destB)].Attempt.ID)
			assert.Equal(t, "latest_multi_b_0", got[key(multiB.ID, destA)].Attempt.ID)
			assert.Equal(t, "latest_single_0", got[key(single.ID, destA)].Attempt.ID)
			assert.Equal(t, multi.ID, got[key(multi.ID, destA)].Event.ID)
		})

		t.Run("attempt newer than the carried time is found", func(t *testing.T) {
			got, err := logStore.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{
				lookup(key(multi.ID, destA), t0.Add(time.Minute-time.Second)),
			})
			require.NoError(t, err)
			require.Contains(t, got, key(multi.ID, destA))
			assert.Equal(t, "latest_multi_2", got[key(multi.ID, destA)].Attempt.ID)
		})

		t.Run("attempts older than the bound are excluded", func(t *testing.T) {
			got, err := logStore.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{
				lookup(key(multi.ID, destA), t0.Add(3*time.Minute)),
			})
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("each key uses its own bound", func(t *testing.T) {
			got, err := logStore.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{
				lookup(key(multi.ID, destA), t0.Add(3*time.Minute)),
				lookup(key(single.ID, destA), lo),
			})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Contains(t, got, key(single.ID, destA))
		})

		t.Run("absent key is missing", func(t *testing.T) {
			got, err := logStore.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{
				lookup(key("latest_unknown", destA), lo),
				lookup(key(single.ID, destA), lo),
				lookup(key(single.ID, destB), lo),
			})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Contains(t, got, key(single.ID, destA))
		})

		t.Run("zero bound is unbounded", func(t *testing.T) {
			got, err := logStore.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{
				lookup(key(multi.ID, destA), time.Time{}),
			})
			require.NoError(t, err)
			require.Contains(t, got, key(multi.ID, destA))
		})

		t.Run("other tenant does not match", func(t *testing.T) {
			other := driver.AttemptKey{TenantID: idgen.String(), EventID: multi.ID, DestinationID: destA}
			got, err := logStore.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{lookup(other, lo)})
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("no keys", func(t *testing.T) {
			got, err := logStore.ListLatestAttempts(ctx, nil)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})
}
