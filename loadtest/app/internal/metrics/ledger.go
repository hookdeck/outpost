package metrics

import (
	"fmt"
	"sync"
)

// Counts is the exact event accounting for one profile in one phase.
//
// The invariant the report depends on: every published event resolves to
// exactly one of completed, failed, or cutoff. There is no "missing" bucket in
// a result — an event that cannot be placed is a harness bug, and the run says
// so rather than quietly reporting a smaller denominator.
type Counts struct {
	Published     int64 `json:"published"`      // accepted by the Publish API
	PublishErrors int64 `json:"publish_errors"` // attempt did not return 2xx
	Deliveries    int64 `json:"deliveries"`     // individual destination hits
	Completed     int64 `json:"completed"`      // every expected destination delivered
	Missing       int64 `json:"missing"`        // swept past grace, never arrived
	Duplicates    int64 `json:"duplicates"`     // delivery for an already-complete event
	Cutoff        int64 `json:"cutoff"`         // still in flight when the run ended
	PayloadBytes  int64 `json:"payload_bytes"`
}

// Balanced reports whether published events are fully accounted for.
func (c Counts) Balanced() bool { return c.Remainder() == 0 }

// Remainder is what the ledger cannot explain. Non-zero means the harness lost
// track of events, which voids the run.
func (c Counts) Remainder() int64 {
	return c.Published - (c.Completed + c.Missing + c.Cutoff)
}

func (c Counts) String() string {
	return fmt.Sprintf("published=%d completed=%d missing=%d cutoff=%d errors=%d remainder=%d",
		c.Published, c.Completed, c.Missing, c.Cutoff, c.PublishErrors, c.Remainder())
}

type ledgerKey struct{ runID, profile, phase string }

// Ledger holds exact counts independent of Prometheus. Scrapes can be missed;
// the accounting cannot.
type Ledger struct {
	mu sync.Mutex
	m  map[ledgerKey]*Counts
}

// EventLedger is the process-wide ledger. One app runs one benchmark at a
// time, and keying by run_id keeps a previous run's counts readable after the
// next one starts.
var EventLedger = NewLedger()

func NewLedger() *Ledger {
	return &Ledger{m: make(map[ledgerKey]*Counts)}
}

func (l *Ledger) add(e Emit, f func(*Counts)) {
	k := ledgerKey{e.RunID, e.Profile, e.Phase}
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.m[k]
	if !ok {
		c = &Counts{}
		l.m[k] = c
	}
	f(c)
}

// Get returns the counts for one profile in one phase of one run.
func (l *Ledger) Get(runID, profile, phase string) Counts {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c, ok := l.m[ledgerKey{runID, profile, phase}]; ok {
		return *c
	}
	return Counts{}
}

// ByProfile returns every profile's counts for one phase of one run.
func (l *Ledger) ByProfile(runID, phase string) map[string]Counts {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]Counts)
	for k, c := range l.m {
		if k.runID == runID && k.phase == phase {
			out[k.profile] = *c
		}
	}
	return out
}

// Total sums every profile in one phase of one run.
func (l *Ledger) Total(runID, phase string) Counts {
	var t Counts
	for _, c := range l.ByProfile(runID, phase) {
		t.Published += c.Published
		t.PublishErrors += c.PublishErrors
		t.Deliveries += c.Deliveries
		t.Completed += c.Completed
		t.Missing += c.Missing
		t.Duplicates += c.Duplicates
		t.Cutoff += c.Cutoff
		t.PayloadBytes += c.PayloadBytes
	}
	return t
}

func (l *Ledger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m = make(map[ledgerKey]*Counts)
}
