package metrics

import (
	"testing"
	"time"
)

// Under fan-out, deliveries 1..n-1 of an event are matched but incomplete.
// Reporting them as unmatched made the caller count them as duplicates, so a
// 5-destination profile reported 80% of its traffic as duplicate deliveries.
func TestRecordDeliveryMatchesEveryFanoutDelivery(t *testing.T) {
	tr := NewInFlightTracker(time.Minute)
	defer tr.Stop()

	var completions int
	tr.SetCallbacks(func(_ string, _ EventMeta, _ time.Duration, all bool) {
		if all {
			completions++
		}
	}, nil, nil)

	t0 := time.Now()
	tr.RecordPublish("evt-1", EventMeta{GroupName: "fanout-5", Phase: "steady", ScheduledAt: t0}, 5)

	for i := 1; i <= 5; i++ {
		_, latency, matched := tr.RecordDelivery("evt-1", t0.Add(time.Duration(i)*10*time.Millisecond))
		if !matched {
			t.Fatalf("delivery %d of 5 reported as unmatched — caller counts it as a duplicate", i)
		}
		if latency <= 0 {
			t.Fatalf("delivery %d: latency %v, want > 0", i, latency)
		}
	}
	if completions != 1 {
		t.Fatalf("completions = %d, want exactly 1", completions)
	}

	// The sixth is a genuine duplicate: the event is done and gone.
	if _, _, matched := tr.RecordDelivery("evt-1", t0.Add(time.Second)); matched {
		t.Fatal("delivery after completion reported as matched, want duplicate")
	}
}

// A late delivery for a swept event is a recovery, not a duplicate.
func TestRecordDeliveryMatchesRecovery(t *testing.T) {
	tr := NewInFlightTracker(time.Millisecond)
	defer tr.Stop()

	var recovered int
	var gotLatency time.Duration
	tr.SetCallbacks(nil, nil, func(_ string, _ EventMeta, l time.Duration) {
		recovered++
		gotLatency = l
	})

	t0 := time.Now().Add(-time.Second)
	tr.RecordPublish("evt-late", EventMeta{GroupName: "baseline", Phase: "steady", ScheduledAt: t0}, 1)
	tr.sweep(time.Now())

	receivedAt := t0.Add(3 * time.Second)
	m, latency, matched := tr.RecordDelivery("evt-late", receivedAt)
	if !matched {
		t.Fatal("recovered delivery reported as unmatched — counted as a duplicate")
	}
	if recovered != 1 {
		t.Fatalf("onRecovered fired %d times, want 1", recovered)
	}
	if m.GroupName != "baseline" {
		t.Fatalf("recovered meta lost attribution: %+v", m)
	}
	// The whole point of recovery: the latency is real and must survive. A run
	// slow enough to sweep is the run whose percentiles matter most.
	if latency != 3*time.Second {
		t.Fatalf("returned latency %v, want 3s measured from t0", latency)
	}
	if gotLatency != 3*time.Second {
		t.Fatalf("onRecovered latency %v, want 3s — recovered deliveries were dropped from the histogram", gotLatency)
	}
}

// An unknown event ID is the one case that is genuinely unmatched.
func TestRecordDeliveryUnknownEvent(t *testing.T) {
	tr := NewInFlightTracker(time.Minute)
	defer tr.Stop()

	if _, _, matched := tr.RecordDelivery("never-published", time.Now()); matched {
		t.Fatal("unknown event reported as matched")
	}
}

// The concurrency budget is denominated in deliveries, so the gauge read
// against it must be too. A fan-out event is one entry and many deliveries.
func TestInFlightCountsDeliveriesNotEvents(t *testing.T) {
	tr := NewInFlightTracker(time.Minute)
	defer tr.Stop()

	t0 := time.Now()
	for _, id := range []string{"a", "b"} {
		tr.RecordPublish(id, EventMeta{GroupName: "fanout-5", ScheduledAt: t0}, 5)
	}
	if got := tr.InFlightCount(); got != 10 {
		t.Fatalf("in-flight = %d, want 10 — two events × 5 destinations", got)
	}
	if got := tr.InFlightCountFor("fanout-5"); got != 10 {
		t.Fatalf("in-flight for group = %d, want 10", got)
	}

	// Each arriving delivery retires one unit of concurrency, not the event.
	tr.RecordDelivery("a", t0.Add(time.Second))
	tr.RecordDelivery("a", t0.Add(time.Second))
	if got := tr.InFlightCount(); got != 8 {
		t.Fatalf("in-flight after 2 of 10 deliveries = %d, want 8", got)
	}

	for i := 0; i < 3; i++ {
		tr.RecordDelivery("a", t0.Add(time.Second))
	}
	if got := tr.InFlightCount(); got != 5 {
		t.Fatalf("in-flight after event a completed = %d, want 5", got)
	}
}
