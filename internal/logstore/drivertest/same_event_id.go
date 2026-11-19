package drivertest

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sameIDEntry builds a first-attempt log entry for an event with the given
// tenant, ID, topic and time.
func sameIDEntry(tenantID, eventID, destinationID, topic string, eventTime time.Time) *models.LogEntry {
	return &models.LogEntry{
		Event: testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(eventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithMatchedDestinationIDs([]string{destinationID}),
			testutil.EventFactory.WithTopic(topic),
			testutil.EventFactory.WithTime(eventTime),
		),
		Attempt: testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(idgen.Attempt()),
			testutil.AttemptFactory.WithEventID(eventID),
			testutil.AttemptFactory.WithDestinationID(destinationID),
			testutil.AttemptFactory.WithStatus("success"),
			testutil.AttemptFactory.WithTime(eventTime),
		),
	}
}

// testSameEventIDAcrossTenants covers two tenants that hold an event with the
// same ID. Event IDs are only unique within a tenant.
func testSameEventIDAcrossTenants(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	eventID := "shared-" + idgen.String()
	tenantA := "tenant-a-" + idgen.String()
	tenantB := "tenant-b-" + idgen.String()
	destinationID := idgen.Destination()
	baseTime := time.Now().Truncate(time.Second).Add(-30 * time.Minute)
	startTime := baseTime.Add(-1 * time.Hour)
	endTime := baseTime.Add(1 * time.Hour)

	t.Run("different time", func(t *testing.T) {
		entryA := sameIDEntry(tenantA, eventID, destinationID, "user.created", baseTime)
		entryB := sameIDEntry(tenantB, eventID, destinationID, "user.deleted", baseTime.Add(time.Minute))
		require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{entryA, entryB}))
		require.NoError(t, h.FlushWrites(ctx))

		for _, tc := range []struct {
			tenantID string
			entry    *models.LogEntry
		}{{tenantA, entryA}, {tenantB, entryB}} {
			event, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{TenantID: tc.tenantID, EventID: eventID})
			require.NoError(t, err)
			require.NotNil(t, event, "tenant %s", tc.tenantID)
			assert.Equal(t, tc.tenantID, event.TenantID)
			assert.Equal(t, tc.entry.Event.Topic, event.Topic)

			events, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				TenantIDs:  []string{tc.tenantID},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, events.Data, 1, "tenant %s", tc.tenantID)
			assert.Equal(t, tc.tenantID, events.Data[0].TenantID)
			assert.Equal(t, tc.entry.Event.Topic, events.Data[0].Topic)

			attempts, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
				TenantIDs:  []string{tc.tenantID},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			require.Len(t, attempts.Data, 1, "tenant %s", tc.tenantID)
			assert.Equal(t, tc.entry.Attempt.ID, attempts.Data[0].Attempt.ID)
			require.NotNil(t, attempts.Data[0].Event)
			assert.Equal(t, tc.tenantID, attempts.Data[0].Event.TenantID)
			assert.Equal(t, tc.entry.Event.Topic, attempts.Data[0].Event.Topic)
		}

		t.Run("unfiltered list returns both", func(t *testing.T) {
			events, err := logStore.ListEvent(ctx, driver.ListEventRequest{
				EventIDs:   []string{eventID},
				Limit:      100,
				TimeFilter: driver.TimeFilter{GTE: &startTime},
			})
			require.NoError(t, err)
			assert.Len(t, events.Data, 2)
		})

		t.Run("event metrics count both", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {tenantA, tenantB}},
				TimeRange: driver.TimeRange{Start: startTime, End: endTime},
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 2, *resp.Data[0].Count)
		})
	})
}

// SameKeyEvents says which events a store keeps when one batch holds events of
// two tenants with the same ID and time.
type SameKeyEvents int

const (
	// KeepsBothEvents: each tenant keeps its own event.
	KeepsBothEvents SameKeyEvents = iota
	// KeepsOneEvent: exactly one of the two events is kept; which one is not defined.
	KeepsOneEvent
)

// SameKeyEventsHarness is implemented by harnesses whose store keys events by
// (time, id) and so keeps one event for tenants that collide on both.
type SameKeyEventsHarness interface {
	SameKeyEvents() SameKeyEvents
}

// testSameEventIDAndTimeAcrossTenants pins that two tenants' events with the
// same ID and the same time can be written in one batch without an error, that
// both tenants' attempts are kept, that no tenant ever reads another tenant's
// event, and which event each store keeps (see SameKeyEventsHarness).
func testSameEventIDAndTimeAcrossTenants(t *testing.T, ctx context.Context, logStore driver.LogStore, h Harness) {
	eventID := "same-time-" + idgen.String()
	tenantA := "tenant-a-" + idgen.String()
	tenantB := "tenant-b-" + idgen.String()
	destinationID := idgen.Destination()
	eventTime := time.Now().Truncate(time.Second).Add(-20 * time.Minute)
	startTime := eventTime.Add(-1 * time.Hour)

	topics := map[string]string{tenantA: "user.created", tenantB: "user.deleted"}

	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		sameIDEntry(tenantA, eventID, destinationID, topics[tenantA], eventTime),
		sameIDEntry(tenantB, eventID, destinationID, topics[tenantB], eventTime),
	}))
	require.NoError(t, h.FlushWrites(ctx))

	keeps := KeepsBothEvents
	if sh, ok := h.(SameKeyEventsHarness); ok {
		keeps = sh.SameKeyEvents()
	}

	kept := map[string]bool{}
	for tenantID, topic := range topics {
		event, err := logStore.RetrieveEvent(ctx, driver.RetrieveEventRequest{TenantID: tenantID, EventID: eventID})
		require.NoError(t, err)
		if event != nil {
			assert.Equal(t, tenantID, event.TenantID)
			assert.Equal(t, topic, event.Topic)
		}
		kept[tenantID] = event != nil

		events, err := logStore.ListEvent(ctx, driver.ListEventRequest{
			TenantIDs:  []string{tenantID},
			Limit:      100,
			TimeFilter: driver.TimeFilter{GTE: &startTime},
		})
		require.NoError(t, err)
		if kept[tenantID] {
			require.Len(t, events.Data, 1, "tenant %s", tenantID)
			assert.Equal(t, tenantID, events.Data[0].TenantID)
			assert.Equal(t, topic, events.Data[0].Topic)
		} else {
			assert.Empty(t, events.Data, "tenant %s", tenantID)
		}

		attempts, err := logStore.ListAttempt(ctx, driver.ListAttemptRequest{
			TenantIDs:  []string{tenantID},
			Limit:      100,
			TimeFilter: driver.TimeFilter{GTE: &startTime},
		})
		require.NoError(t, err)
		require.Len(t, attempts.Data, 1, "tenant %s keeps its attempt", tenantID)
		require.NotNil(t, attempts.Data[0].Event, "attempt of tenant %s", tenantID)
		assert.Equal(t, tenantID, attempts.Data[0].Event.TenantID, "attempt of tenant %s", tenantID)
		assert.Equal(t, topic, attempts.Data[0].Event.Topic, "attempt of tenant %s", tenantID)
	}

	if keeps == KeepsBothEvents {
		assert.True(t, kept[tenantA] && kept[tenantB], "each tenant keeps its event")
	} else {
		assert.NotEqual(t, kept[tenantA], kept[tenantB], "exactly one event is kept")
	}
}
