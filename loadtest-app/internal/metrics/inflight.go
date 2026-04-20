package metrics

import (
	"sync"
	"time"
)

type inflightEntry struct {
	groupName          string
	sentAt             time.Time
	expectedDeliveries int
	receivedDeliveries int
}

type InFlightTracker struct {
	mu          sync.Mutex
	entries     map[string]*inflightEntry // eventID → entry
	gracePeriod time.Duration
	onMissing   func(eventID, groupName string)
	onDelivery  func(eventID, groupName string, e2eLatency time.Duration, allDelivered bool)

	stopCh chan struct{}
	done   chan struct{}
}

func NewInFlightTracker(gracePeriod time.Duration) *InFlightTracker {
	t := &InFlightTracker{
		entries:     make(map[string]*inflightEntry),
		gracePeriod: gracePeriod,
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
	}
	go t.sweepLoop()
	return t
}

func (t *InFlightTracker) SetCallbacks(onDelivery func(string, string, time.Duration, bool), onMissing func(string, string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onDelivery = onDelivery
	t.onMissing = onMissing
}

func (t *InFlightTracker) RecordPublish(eventID, groupName string, expectedDeliveries int, sentAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[eventID] = &inflightEntry{
		groupName:          groupName,
		sentAt:             sentAt,
		expectedDeliveries: expectedDeliveries,
	}
}

func (t *InFlightTracker) RecordDelivery(eventID, groupName string, receivedAt time.Time) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[eventID]
	if !ok {
		// Delivery for unknown event (already swept or duplicate)
		return 0, false
	}

	entry.receivedDeliveries++
	e2eLatency := receivedAt.Sub(entry.sentAt)
	allDelivered := entry.receivedDeliveries >= entry.expectedDeliveries

	if allDelivered {
		delete(t.entries, eventID)
	}

	if t.onDelivery != nil {
		t.onDelivery(eventID, groupName, e2eLatency, allDelivered)
	}

	return e2eLatency, allDelivered
}

func (t *InFlightTracker) InFlightCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
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
	var missing []struct {
		eventID   string
		groupName string
	}
	for eventID, entry := range t.entries {
		if now.Sub(entry.sentAt) > t.gracePeriod {
			missing = append(missing, struct {
				eventID   string
				groupName string
			}{eventID, entry.groupName})
			delete(t.entries, eventID)
		}
	}
	onMissing := t.onMissing
	t.mu.Unlock()

	if onMissing != nil {
		for _, m := range missing {
			onMissing(m.eventID, m.groupName)
		}
	}
}
