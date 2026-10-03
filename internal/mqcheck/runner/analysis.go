package runner

import (
	"math"
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqcheck/sut"
)

// Inv is one handler invocation.
type Inv struct {
	W       string
	ID      string
	Start   time.Time
	End     time.Time // zero: never settled by this invocation
	Outcome string
	Attempt int       // broker attempt, 0 = unknown
	Recv    time.Time // zero: not seen by Receive
	Pub     time.Time
	Size    int
}

// Extra classes: why a message was handled more than once, or handled on a
// later delivery attempt without an earlier invocation.
const (
	ExtraAfterNack     = "after-nack"            // allowed
	ExtraAfterTimeout  = "after-handler-timeout" // allowed: the handler ran past the visibility timeout
	ExtraAfterShutdown = "after-shutdown"        // allowed: a consumer stopped or was killed
	ExtraHeldTooLong   = "held-past-timeout"     // consumer-added: received, not handled, timer expired
	ExtraBroker        = "unexplained"           // no consumer cause found: reported as a broker duplicate
)

// Extra is one invocation beyond the first, or a first invocation on a
// later delivery attempt.
type Extra struct {
	ID    string
	Class string
	Inv   Inv
}

// Analysis is a case's recordings, indexed.
type Analysis struct {
	Invs    []Inv
	ByID    map[string][]Inv
	Samples map[string][]sut.Event
	Exits   map[string]*sut.Event
	Ready   map[string]time.Time
	Broker  []provider.Sample
	Msgs    map[string]*Msg
	TermAt  map[string]time.Time
	Killed  map[string]bool
	T       time.Duration
}

// Analyze indexes what was recorded so far.
func (e *Env) Analyze() *Analysis {
	recs, samples, msgs := e.Snapshot()
	a := &Analysis{
		ByID: map[string][]Inv{}, Samples: map[string][]sut.Event{}, Exits: map[string]*sut.Event{},
		Ready: map[string]time.Time{}, Broker: samples, Msgs: msgs, TermAt: map[string]time.Time{}, Killed: map[string]bool{}, T: e.T,
	}
	e.mu.Lock()
	for _, w := range e.workers {
		w.mu.Lock()
		if !w.termAt.IsZero() {
			a.TermAt[w.ID] = w.termAt
		}
		a.Killed[w.ID] = w.killed
		w.mu.Unlock()
	}
	e.mu.Unlock()
	open := map[string][]int{} // worker|id → indexes of invocations without end
	for _, r := range recs {
		switch r.K {
		case sut.KindStart:
			inv := Inv{W: r.W, ID: r.ID, Start: ns(r.T), Attempt: r.Attempt, Recv: ns(r.Recv), Pub: ns(r.Pub), Size: r.Size}
			a.Invs = append(a.Invs, inv)
			k := r.W + "|" + r.ID
			open[k] = append(open[k], len(a.Invs)-1)
		case sut.KindEnd:
			k := r.W + "|" + r.ID
			if q := open[k]; len(q) > 0 {
				a.Invs[q[0]].End = ns(r.T)
				a.Invs[q[0]].Outcome = r.Outcome
				open[k] = q[1:]
			}
		case sut.KindSample:
			a.Samples[r.W] = append(a.Samples[r.W], r.Event)
		case sut.KindExit:
			ev := r.Event
			a.Exits[r.W] = &ev
		case sut.KindReady:
			if _, ok := a.Ready[r.W]; !ok {
				a.Ready[r.W] = ns(r.T)
			}
		}
	}
	sort.SliceStable(a.Invs, func(i, j int) bool { return a.Invs[i].Start.Before(a.Invs[j].Start) })
	for _, inv := range a.Invs {
		a.ByID[inv.ID] = append(a.ByID[inv.ID], inv)
	}
	return a
}

func ns(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// MaxHandled is the most handlers running at once in worker w ("" = the
// largest over workers), from the worker's own gauge.
func (a *Analysis) MaxHandled(w string) int {
	m := 0
	for wid, ss := range a.Samples {
		if w != "" && wid != w {
			continue
		}
		for _, s := range ss {
			m = max(m, s.HandledMax)
		}
	}
	return m
}

// MaxHandledBytes is the most body bytes in running handlers at once.
func (a *Analysis) MaxHandledBytes(w string) int64 {
	var m int64
	for wid, ss := range a.Samples {
		if w != "" && wid != w {
			continue
		}
		for _, s := range ss {
			m = max(m, s.HandledBMax)
		}
	}
	return m
}

// MaxVisibleHeld is the most messages returned by Receive whose handler had
// not started, at once, and the most bytes.
func (a *Analysis) MaxVisibleHeld(w string) (int, int64) {
	var (
		n int
		b int64
	)
	for wid, ss := range a.Samples {
		if w != "" && wid != w {
			continue
		}
		for _, s := range ss {
			n = max(n, s.VisHeldMax)
			b = max(b, s.VisHeldBMax)
		}
	}
	return n, b
}

// MaxVisibleHeldIn is MaxVisibleHeld over worker samples taken in [from, to).
func (a *Analysis) MaxVisibleHeldIn(w string, from, to time.Time) int {
	n := 0
	for wid, ss := range a.Samples {
		if w != "" && wid != w {
			continue
		}
		for _, s := range ss {
			if t := ns(s.T); !t.Before(from) && t.Before(to) {
				n = max(n, s.VisHeldMax)
			}
		}
	}
	return n
}

// MaxRSS is the peak resident memory of worker w ("" = any).
func (a *Analysis) MaxRSS(w string) int64 {
	var m int64
	for wid, ss := range a.Samples {
		if w != "" && wid != w {
			continue
		}
		for _, s := range ss {
			m = max(m, s.RSS)
		}
	}
	for wid, ex := range a.Exits {
		if w == "" || wid == w {
			m = max(m, ex.PeakRSS)
		}
	}
	return m
}

// IdleRSS is worker w's resident memory in its first sample, which the
// worker takes before its consumer starts.
func (a *Analysis) IdleRSS(w string) int64 {
	if ss := a.Samples[w]; len(ss) > 0 {
		return ss[0].RSS
	}
	return 0
}

// Waits are, per invocation, the time from Receive returning the message to
// its handler starting.
func (a *Analysis) Waits() []time.Duration {
	var out []time.Duration
	for _, inv := range a.Invs {
		if !inv.Recv.IsZero() {
			out = append(out, inv.Start.Sub(inv.Recv))
		}
	}
	return out
}

// QueueLatency are publish → handler start per first invocation, for
// messages published in [from, to).
func (a *Analysis) QueueLatency(from, to time.Time) (queue, e2e []time.Duration) {
	for id, invs := range a.ByID {
		m := a.Msgs[id]
		if m == nil || m.Published.Before(from) || !m.Published.Before(to) {
			continue
		}
		first := invs[0]
		queue = append(queue, first.Start.Sub(m.Published))
		if !first.End.IsZero() {
			e2e = append(e2e, first.End.Sub(m.Published))
		}
	}
	return
}

// workerAt returns worker w's last sample at or before t.
func (a *Analysis) workerAt(w string, t time.Time) (sut.Event, bool) {
	ss := a.Samples[w]
	i := sort.Search(len(ss), func(i int) bool { return ss[i].T > t.UnixNano() })
	if i == 0 {
		return sut.Event{}, false
	}
	return ss[i-1], true
}

// handledAt counts invocations running at t in the given workers (all when
// none given).
func (a *Analysis) handledAt(t time.Time, ws map[string]bool) int64 {
	var n int64
	for _, inv := range a.Invs {
		if len(ws) > 0 && !ws[inv.W] {
			continue
		}
		if !inv.Start.After(t) && (inv.End.IsZero() || inv.End.After(t)) {
			n++
		}
	}
	return n
}

// HiddenHeld returns, from broker samples in [from, to), the messages the
// broker counts as delivered that are neither handled nor visibly held in a
// worker: held inside client libraries or limiters. sustained is the largest
// value that lasted at least window (filters ack latency); peak the largest
// single reading. ok is false when the broker has no in-flight count.
func (a *Analysis) HiddenHeld(from, to time.Time, window time.Duration) (sustained, peak int64, ok bool) {
	type pt struct {
		t time.Time
		v int64
	}
	var pts []pt
	for _, s := range a.Broker {
		if s.InFlight < 0 || s.At.Before(from) || !s.At.Before(to) {
			continue
		}
		v := s.InFlight - a.handledAt(s.At, nil)
		for w := range a.Samples {
			if ws, found := a.workerAt(w, s.At); found {
				v -= int64(ws.VisHeld)
			}
		}
		pts = append(pts, pt{s.At, v})
		peak = max(peak, v)
	}
	if len(pts) == 0 {
		return 0, 0, false
	}
	last := pts[len(pts)-1].t
	for i := range pts {
		end := pts[i].t.Add(window)
		if last.Before(end) {
			break
		}
		lo := pts[i].v
		for j := i; j < len(pts) && !pts[j].t.After(end); j++ {
			lo = min(lo, pts[j].v)
		}
		sustained = max(sustained, lo)
	}
	return max(sustained, 0), max(peak, 0), true
}

// Extras classifies every invocation beyond a message's first, and every
// first invocation on a delivery attempt > 1.
func (a *Analysis) Extras() []Extra {
	var out []Extra
	firstTerm := time.Time{}
	for _, t := range a.TermAt {
		if firstTerm.IsZero() || t.Before(firstTerm) {
			firstTerm = t
		}
	}
	for id, invs := range a.ByID {
		for i, inv := range invs {
			if i == 0 {
				if inv.Attempt > 1 {
					cls := ExtraHeldTooLong
					if !firstTerm.IsZero() && inv.Start.After(firstTerm) {
						cls = ExtraAfterShutdown
					}
					out = append(out, Extra{ID: id, Class: cls, Inv: inv})
				}
				continue
			}
			prev := invs[i-1]
			term := a.TermAt[prev.W]
			var cls string
			switch {
			case prev.Outcome == sut.OutcomeNack:
				cls = ExtraAfterNack
			case !prev.End.IsZero() && prev.End.Sub(prev.Start) >= a.T,
				prev.End.IsZero() && term.IsZero() && !a.Killed[prev.W] && inv.Start.Sub(prev.Start) >= a.T:
				cls = ExtraAfterTimeout
			case !term.IsZero() && inv.Start.After(term), a.Killed[prev.W] && prev.End.IsZero():
				cls = ExtraAfterShutdown
			case prev.Attempt > 0 && inv.Attempt > 0 && inv.Attempt <= prev.Attempt:
				// Same delivery attempt seen twice: the broker's duplicate.
				cls = ExtraBroker
			case a.Msgs[id] != nil && inv.Start.Sub(a.Msgs[id].Published) < a.T:
				// No lease can have expired yet: a timer copy appears at the
				// earliest T after publishing, so this is the broker's.
				cls = ExtraBroker
			case inv.Start.Sub(prev.Start) < a.T:
				// The copy appeared before the handler had run for a whole
				// timeout: the timer started before the handler did. Without
				// attempt numbers a fast broker duplicate also lands here.
				cls = ExtraHeldTooLong
			case prev.Attempt > 0 && inv.Attempt > prev.Attempt+1:
				// A delivery in between was never handled.
				cls = ExtraHeldTooLong
			default:
				cls = ExtraBroker
			}
			out = append(out, Extra{ID: id, Class: cls, Inv: inv})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Inv.Start.Before(out[j].Inv.Start) })
	return out
}

// CountExtras counts extras by class.
func CountExtras(xs []Extra) map[string]int {
	m := map[string]int{}
	for _, x := range xs {
		m[x.Class]++
	}
	return m
}

// Percentile returns the p-th percentile (0..100) of ds.
func Percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(math.Ceil(p/100*float64(len(s)))) - 1
	idx = max(0, min(len(s)-1, idx))
	return s[idx]
}

// MaxDur returns the largest of ds.
func MaxDur(ds []time.Duration) time.Duration {
	var m time.Duration
	for _, d := range ds {
		m = max(m, d)
	}
	return m
}
