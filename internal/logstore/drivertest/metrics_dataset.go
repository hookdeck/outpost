package drivertest

import (
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

// ============================================================================
// Metrics Test Dataset
// ============================================================================
//
// Time range: 2000-01-01 00:00 UTC  →  2000-02-01 00:00 UTC  (full January)
//
// Two tenants:
//   - Tenant 1: 300 events/attempts (the main dataset)
//   - Tenant 2:   5 events/attempts (isolation checks only)
//
// Destinations:
//   - Tenant 1: dest_1.1, dest_1.2
//   - Tenant 2: dest_2.1
//
// IDs:
//   - Tenants:      "m_tenant_1", "m_tenant_2"
//   - Destinations: "m_dest_1.1", "m_dest_1.2", "m_dest_2.1"
//   - Events:       "m_evt_1_{idx}", "m_evt_2_{idx}"
//   - Attempts:     "m_att_1_{idx}", "m_att_2_{idx}"
//
// ── Tenant 1 Distribution ────────────────────────────────────────────────
//
// Sparse days (50 events total, 5 days × 10 events each):
//   Jan 3   — 10 events, one per hour 09:00–18:00
//   Jan 7   — 10 events, one per hour 09:00–18:00
//   Jan 11  — 10 events, one per hour 09:00–18:00
//   Jan 22  — 10 events, one per hour 09:00–18:00
//   Jan 28  — 10 events, one per hour 09:00–18:00
//
// Dense day — Jan 15 (250 events):
//   Bell-curve distribution across 5 hours:
//     10:00–10:59 →  25 events
//     11:00–11:59 →  50 events
//     12:00–12:59 → 100 events
//     13:00–13:59 →  50 events
//     14:00–14:59 →  25 events
//
//   Within each hour, events are spread evenly using:
//     offset = i * 3600s / countForHour  (seconds from top of hour)
//
// ── Dimension Cycling (Tenant 1) ─────────────────────────────────────────
//
// All 300 events are numbered 0–299 in insertion order. Dimensions cycle:
//
//   topic:              i % 3  → 0=user.created, 1=user.deleted, 2=user.updated
//   destination:        i % 2  → 0=dest_1.1, 1=dest_1.2
//   status:             i % 5  → 0,1,2=success, 3,4=failed
//   code:               success → i%2==0 ? "200" : "201"
//                       failed  → i%2==0 ? "500" : "422"
//   attempt_number:     i % 4  → 0,1,2,3
//   manual:             i % 10 == 9
//   eligible_for_retry: i % 3 != 2
//
// ── Derived Totals (Tenant 1, all 300) ───────────────────────────────────
//
// Event metrics:
//   count:                        300
//   by topic:                     user.created=100, user.deleted=100, user.updated=100
//   by destination:               dest_1.1=150, dest_1.2=150
//   by eligible_for_retry:        true=200, false=100
//
// Attempt metrics:
//   count:                        300
//   successful (i%5 in {0,1,2}):  180
//   failed (i%5 in {3,4}):        120
//   error_rate:                    120/300 = 0.4
//   by code:                       200=90, 201=90, 500=60, 422=60
//   first_attempt (i%4==0):        75
//   retry (i%4>0):                225
//   manual (i%10==9):              30
//   avg_attempt_number:            450/300 = 1.5
//
// Dense day — Jan 15 (250 events, indices 50..299):
//   hourly buckets:  10:00→25, 11:00→50, 12:00→100, 13:00→50, 14:00→25
//
// ── Tenant 2 ─────────────────────────────────────────────────────────────
//
//   5 events, all topic=user.created, dest=dest_2.1, status=success, code=200,
//   attempt_number=0, manual=false, eligible_for_retry=true
//
//   Jan 5 09:00, Jan 10 09:00, Jan 15 12:15, Jan 22 09:00, Jan 27 09:00
//
// ============================================================================

const (
	mTenant1 = "m_tenant_1"
	mTenant2 = "m_tenant_2"
	mDest1_1 = "m_dest_1.1"
	mDest1_2 = "m_dest_1.2"
	mDest2_1 = "m_dest_2.1"
)

// metricsDataset holds the seeded data and pre-computed constants for assertions.
type metricsDataset struct {
	tenant1 string
	tenant2 string
	dest1_1 string // tenant 1's first destination
	dest1_2 string // tenant 1's second destination
	dest2_1 string // tenant 2's destination
	entries []*models.LogEntry

	// Full date range covering all data.
	dateRange dateRange

	// Dense day date range (Jan 15 only).
	denseDayRange dateRange
}

type dateRange struct {
	start time.Time
	end   time.Time
}

func (d dateRange) toDriver() driver.DateRange {
	return driver.DateRange{Start: d.start, End: d.end}
}

var (
	// January 2000
	dsStart = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	dsEnd   = time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC)

	// Dense day
	dsDenseDay = time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC)
)

// Sparse days: 5 days × 10 events each = 50 events.
// Each day has 10 events, one per hour from 09:00–18:00.
var sparseDays = []int{3, 7, 11, 22, 28}

// Dense day hourly distribution (bell curve, total=250).
var denseHours = []struct {
	hour  int
	count int
}{
	{10, 25},
	{11, 50},
	{12, 100},
	{13, 50},
	{14, 25},
}

func buildMetricsDataset() *metricsDataset {
	topics := testutil.TestTopics // [user.created, user.deleted, user.updated] — sorted
	codes := map[string][2]string{
		"success": {"200", "201"},
		"failed":  {"500", "422"},
	}

	var entries []*models.LogEntry
	idx := 0

	makeEntry := func(tenant string, eventTime time.Time) *models.LogEntry {
		dest := mDest1_1
		if idx%2 == 1 {
			dest = mDest1_2
		}
		topic := topics[idx%3]
		status := "success"
		if idx%5 == 3 || idx%5 == 4 {
			status = "failed"
		}
		code := codes[status][idx%2]
		attemptNum := idx % 4
		manual := idx%10 == 9
		eligible := idx%3 != 2

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("m_evt_1_%d", idx)),
			testutil.EventFactory.WithTenantID(tenant),
			testutil.EventFactory.WithDestinationID(dest),
			testutil.EventFactory.WithTopic(topic),
			testutil.EventFactory.WithTime(eventTime),
			testutil.EventFactory.WithEligibleForRetry(eligible),
		)
		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(fmt.Sprintf("m_att_1_%d", idx)),
			testutil.AttemptFactory.WithTenantID(tenant),
			testutil.AttemptFactory.WithEventID(event.ID),
			testutil.AttemptFactory.WithDestinationID(dest),
			testutil.AttemptFactory.WithStatus(status),
			testutil.AttemptFactory.WithCode(code),
			testutil.AttemptFactory.WithTime(eventTime.Add(time.Millisecond)),
			testutil.AttemptFactory.WithAttemptNumber(attemptNum),
			testutil.AttemptFactory.WithManual(manual),
		)

		idx++
		return &models.LogEntry{Event: event, Attempt: attempt}
	}

	// ── Sparse days (indices 0–49): 5 days × 10 events ──
	for _, day := range sparseDays {
		for j := range 10 {
			t := time.Date(2000, 1, day, 9+j, 0, 0, 0, time.UTC)
			entries = append(entries, makeEntry(mTenant1, t))
		}
	}

	// ── Dense day — Jan 15 (indices 50–299): 250 events ──
	for _, dh := range denseHours {
		for i := range dh.count {
			offsetSec := i * 3600 / dh.count
			t := time.Date(2000, 1, 15, dh.hour, 0, 0, 0, time.UTC).Add(
				time.Duration(offsetSec) * time.Second,
			)
			entries = append(entries, makeEntry(mTenant1, t))
		}
	}

	// ── Tenant 2 (5 events, independent — not using makeEntry/idx) ──
	tenant2Times := []time.Time{
		time.Date(2000, 1, 5, 9, 0, 0, 0, time.UTC),
		time.Date(2000, 1, 10, 9, 0, 0, 0, time.UTC),
		time.Date(2000, 1, 15, 12, 15, 0, 0, time.UTC),
		time.Date(2000, 1, 22, 9, 0, 0, 0, time.UTC),
		time.Date(2000, 1, 27, 9, 0, 0, 0, time.UTC),
	}
	for i, bt := range tenant2Times {
		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(fmt.Sprintf("m_evt_2_%d", i)),
			testutil.EventFactory.WithTenantID(mTenant2),
			testutil.EventFactory.WithDestinationID(mDest2_1),
			testutil.EventFactory.WithTopic(topics[0]),
			testutil.EventFactory.WithTime(bt),
			testutil.EventFactory.WithEligibleForRetry(true),
		)
		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(fmt.Sprintf("m_att_2_%d", i)),
			testutil.AttemptFactory.WithTenantID(mTenant2),
			testutil.AttemptFactory.WithEventID(event.ID),
			testutil.AttemptFactory.WithDestinationID(mDest2_1),
			testutil.AttemptFactory.WithStatus("success"),
			testutil.AttemptFactory.WithCode("200"),
			testutil.AttemptFactory.WithTime(bt.Add(time.Millisecond)),
			testutil.AttemptFactory.WithAttemptNumber(0),
			testutil.AttemptFactory.WithManual(false),
		)
		entries = append(entries, &models.LogEntry{Event: event, Attempt: attempt})
	}

	return &metricsDataset{
		tenant1: mTenant1,
		tenant2: mTenant2,
		dest1_1: mDest1_1,
		dest1_2: mDest1_2,
		dest2_1: mDest2_1,
		entries: entries,
		dateRange: dateRange{
			start: dsStart,
			end:   dsEnd,
		},
		denseDayRange: dateRange{
			start: dsDenseDay,
			end:   dsDenseDay.Add(24 * time.Hour),
		},
	}
}
