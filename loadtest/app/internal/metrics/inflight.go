package metrics

import (
	"sync"
	"time"
)

// EventMeta is what the tracker remembers about an in-flight event. It travels
// to the delivery/missing/cutoff callbacks so an event is always attributed to
// the group and phase that published it, no matter when it lands.
type EventMeta struct {
	GroupName   string
	Phase       string
	ScheduledAt time.Time // t0 — when the event was scheduled to be published
}

type inflightEntry struct {
	meta               EventMeta
	expectedDeliveries int
	receivedDeliveries int
}

// outstanding is how many of this event's deliveries have yet to arrive. Under
// fan-out an event is one entry but many deliveries, and the delivery is the
// unit the concurrency budget is denominated in.
func (e *inflightEntry) outstanding() int {
	if n := e.expectedDeliveries - e.receivedDeliveries; n > 0 {
		return n
	}
	return 0
}

type InFlightTracker struct {
	mu          sync.Mutex
	entries     map[string]*inflightEntry // eventID → entry
	swept       map[string]EventMeta      // eventID → meta (recently swept, for late delivery recovery)
	gracePeriod time.Duration
	onMissing   func(eventID string, m EventMeta)
	onDelivery  func(eventID string, m EventMeta, e2eLatency time.Duration, allDelivered bool)
	onRecovered func(eventID string, m EventMeta, e2eLatency time.Duration) // late delivery for swept event

	sweeping bool // during drain, sweeps are suspended: nothing is "missing" yet

	stopCh chan struct{}
	done   chan struct{}
}

func NewInFlightTracker(gracePeriod time.Duration) *InFlightTracker {
	t := &InFlightTracker{
		entries:     make(map[string]*inflightEntry),
		swept:       make(map[string]EventMeta),
		gracePeriod: gracePeriod,
		sweeping:    true,
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
	}
	go t.sweepLoop()
	return t
}

func (t *InFlightTracker) SetCallbacks(
	onDelivery func(string, EventMeta, time.Duration, bool),
	onMissing func(string, EventMeta),
	onRecovered func(string, EventMeta, time.Duration),
) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onDelivery = onDelivery
	t.onMissing = onMissing
	t.onRecovered = onRecovered
}

// SetSweeping suspends or resumes the missing sweep. A run suspends it during
// drain so slow-but-arriving deliveries are not misreported as missing.
func (t *InFlightTracker) SetSweeping(on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweeping = on
}

func (t *InFlightTracker) RecordPublish(eventID string, m EventMeta, expectedDeliveries int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[eventID] = &inflightEntry{
		meta:               m,
		expectedDeliveries: expectedDeliveries,
	}
}

// RemoveInFlight drops an in-flight entry without firing onMissing.
// Used when a publish fails after we pre-recorded the event.
func (t *InFlightTracker) RemoveInFlight(eventID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, eventID)
}

// RecordDelivery attributes a delivery to its event. The returned latency runs
// from t0 (scheduled send), not from the moment the request went out.
//
// The returned bool is whether the delivery was *matched* to a known event —
// either in flight or recoverable from the swept set. It is deliberately not
// "all deliveries arrived": under fan-out every delivery but the last is
// matched-but-incomplete, and treating those as unmatched counts four fifths
// of a fan-out profile's traffic as duplicates. Completion reaches callers
// through the onDelivery callback's allDelivered argument.
func (t *InFlightTracker) RecordDelivery(eventID string, receivedAt time.Time) (EventMeta, time.Duration, bool) {
	t.mu.Lock()

	entry, ok := t.entries[eventID]
	if !ok {
		// Check if this was swept as missing — if so, it's a late recovery
		if meta, wasMissing := t.swept[eventID]; wasMissing {
			delete(t.swept, eventID)
			// A recovered delivery carries a real latency and must be measured
			// like any other. Dropping it empties the histogram exactly when
			// the deployment is slow enough to miss the sweep window, and the
			// percentile that survives is drawn only from the fast deliveries.
			e2eLatency := receivedAt.Sub(meta.ScheduledAt)
			onRecovered := t.onRecovered
			t.mu.Unlock()
			if onRecovered != nil {
				onRecovered(eventID, meta, e2eLatency)
			}
			return meta, e2eLatency, true
		}
		t.mu.Unlock()
		return EventMeta{}, 0, false
	}

	entry.receivedDeliveries++
	meta := entry.meta
	e2eLatency := receivedAt.Sub(meta.ScheduledAt)
	allDelivered := entry.receivedDeliveries >= entry.expectedDeliveries
	matched := true

	if allDelivered {
		delete(t.entries, eventID)
	}
	onDelivery := t.onDelivery
	t.mu.Unlock()

	if onDelivery != nil {
		onDelivery(eventID, meta, e2eLatency, allDelivered)
	}

	return meta, e2eLatency, matched
}

// InFlightCount is outstanding *deliveries*, not outstanding events. The
// concurrency budget is defined on deliveries — rate × fan-out × response time
// — so counting events would report a fan-out profile at a fifth of the load it
// is actually placing, against a budget line drawn in the other unit.
func (t *InFlightTracker) InFlightCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, e := range t.entries {
		n += e.outstanding()
	}
	return n
}

func (t *InFlightTracker) InFlightCountFor(groupName string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, e := range t.entries {
		if e.meta.GroupName == groupName {
			n += e.outstanding()
		}
	}
	return n
}

// DrainInFlight removes everything still in flight and reports it, so a run can
// close its ledger with an explicit cutoff bucket rather than a remainder.
func (t *InFlightTracker) DrainInFlight() map[string]EventMeta {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]EventMeta, len(t.entries))
	for id, e := range t.entries {
		out[id] = e.meta
		delete(t.entries, id)
	}
	return out
}

func (t *InFlightTracker) Stop() {
	close(t.stopCh)
	<-t.done
}

func (t *InFlightTracker) sweepLoop() {
	defer close(t.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case now := <-ticker.C:
			t.sweep(now)
		}
	}
}

func (t *InFlightTracker) sweep(now time.Time) {
	t.mu.Lock()
	if !t.sweeping {
		t.mu.Unlock()
		return
	}
	var missing []struct {
		eventID string
		meta    EventMeta
	}
	for eventID, entry := range t.entries {
		if now.Sub(entry.meta.ScheduledAt) > t.gracePeriod {
			missing = append(missing, struct {
				eventID string
				meta    EventMeta
			}{eventID, entry.meta})
			t.swept[eventID] = entry.meta
			delete(t.entries, eventID)
		}
	}
	// Cap swept map to prevent unbounded growth
	if len(t.swept) > 10000 {
		for k := range t.swept {
			delete(t.swept, k)
			if len(t.swept) <= 5000 {
				break
			}
		}
	}
	onMissing := t.onMissing
	t.mu.Unlock()

	if onMissing != nil {
		for _, m := range missing {
			onMissing(m.eventID, m.meta)
		}
	}
}
