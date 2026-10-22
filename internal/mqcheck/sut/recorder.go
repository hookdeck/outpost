package sut

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
)

// recorder tracks, per process, messages returned by Receive whose handler
// has not started (visibly held) and messages whose handler runs (handled).
type recorder struct {
	mu       sync.Mutex
	pending  map[*mqs.Message]int64 // received, handler not started → receive time
	received int
	started  int

	handled, handledMax   int
	handledB, handledBMax int64
	held, heldMax         int
	heldB, heldBMax       int64
}

func newRecorder() *recorder {
	return &recorder{pending: map[*mqs.Message]int64{}}
}

func (r *recorder) received1(m *mqs.Message) {
	now := time.Now().UnixNano()
	r.mu.Lock()
	r.pending[m] = now
	r.received++
	r.held++
	r.heldB += int64(len(m.Body))
	r.heldMax = max(r.heldMax, r.held)
	r.heldBMax = max(r.heldBMax, r.heldB)
	r.mu.Unlock()
}

// start moves m from held to handled and returns when Receive returned it
// (0 if it did not pass through Receive).
func (r *recorder) start(m *mqs.Message) int64 {
	size := int64(len(m.Body))
	r.mu.Lock()
	defer r.mu.Unlock()
	recv, ok := r.pending[m]
	if ok {
		delete(r.pending, m)
		r.held--
		r.heldB -= size
	}
	r.started++
	r.handled++
	r.handledB += size
	r.handledMax = max(r.handledMax, r.handled)
	r.handledBMax = max(r.handledBMax, r.handledB)
	return recv
}

func (r *recorder) end(size int) {
	r.mu.Lock()
	r.handled--
	r.handledB -= int64(size)
	r.mu.Unlock()
}

// sample returns current values and the max since the last sample.
func (r *recorder) sample() Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := Event{
		Handled: r.handled, HandledMax: r.handledMax,
		HandledB: r.handledB, HandledBMax: r.handledBMax,
		VisHeld: r.held, VisHeldMax: r.heldMax,
		VisHeldB: r.heldB, VisHeldBMax: r.heldBMax,
	}
	r.handledMax, r.handledBMax = r.handled, r.handledB
	r.heldMax, r.heldBMax = r.held, r.heldB
	return ev
}

// summary returns counts and the harness ids of messages received but never
// handled.
func (r *recorder) summary() (received, started int, unstarted []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for m := range r.pending {
		var task models.DeliveryTask
		if json.Unmarshal(m.Body, &task) == nil {
			unstarted = append(unstarted, task.Event.ID)
		}
	}
	return r.received, r.started, unstarted
}

// recordingSubscription forwards to the queue's subscription and records
// when Receive returns each message. It forwards every optional interface the
// consumer checks (mqs.ConcurrentSubscription today), so the consumer takes
// the same path it takes without the wrapper. A change that adds an optional
// interface to mqs.Subscription adds it here too.
type recordingSubscription struct {
	mqs.Subscription
	rec *recorder
}

func wrapSubscription(s mqs.Subscription, rec *recorder) mqs.Subscription {
	return &recordingSubscription{Subscription: s, rec: rec}
}

func (s *recordingSubscription) Receive(ctx context.Context) (*mqs.Message, error) {
	m, err := s.Subscription.Receive(ctx)
	if err == nil && m != nil {
		s.rec.received1(m)
	}
	return m, err
}

func (s *recordingSubscription) SupportsConcurrency() bool {
	cs, ok := s.Subscription.(mqs.ConcurrentSubscription)
	return ok && cs.SupportsConcurrency()
}

var _ mqs.ConcurrentSubscription = (*recordingSubscription)(nil)

// Process stats. Linux reads /proc; elsewhere RSS is 0.

func rss() int64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	pages, _ := strconv.ParseInt(f[1], 10, 64)
	return pages * int64(os.Getpagesize())
}

func peakRSS() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmHWM:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				return kb << 10
			}
		}
	}
	return 0
}

func cpuNanos() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return ru.Utime.Nano() + ru.Stime.Nano()
}
