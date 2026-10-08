// Package cases holds the validation cases: scenarios written once against
// the consumer contract and run on every provider. Each case drives workers
// through the runner and records checks tied to a requirement (R1-R17, see
// cmd/mqcheck/REQUIREMENTS.md). Case ids follow the requirement they test.
package cases

import (
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/payload"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqcheck/runner"
	"github.com/hookdeck/outpost/internal/mqcheck/sut"
)

var (
	both     = []string{"quick", "official"}
	flowCtl  = provider.ClassFlowControl
	leaseTmr = provider.ClassLeaseTiming
)

// All returns every case in report order.
func All() []runner.Case {
	return []runner.Case{
		{ID: "C1.1", Title: "Count limit holds over a backlog", Req: []int{1}, Tiers: both, Run: countBacklog},
		{ID: "C1.3", Title: "Count limit 1: never two at once", Req: []int{1}, Tiers: both, Run: countOne},
		{ID: "C2.1", Title: "Byte limit holds over a backlog of large messages", Req: []int{2}, Tiers: both, Bytes: true, Run: bytesBacklog},
		{ID: "C3.1", Title: "Message larger than the byte limit is processed alone", Req: []int{3}, Tiers: both, Bytes: true, Run: oversized},
		{ID: "C4.1", Title: "Waiting for a count limit doesn't run the visibility timeout or add duplicates (C4.1, C11.1)", Req: []int{4, 11}, Tiers: both, Needs: []provider.Class{flowCtl, leaseTmr}, Run: func(e *runner.Env) error { return waiting(e, false) }},
		{ID: "C4.2", Title: "Waiting for a byte limit doesn't run the visibility timeout or add duplicates (C4.2, C11.2)", Req: []int{4, 11}, Tiers: both, Bytes: true, Needs: []provider.Class{flowCtl, leaseTmr}, Run: func(e *runner.Env) error { return waiting(e, true) }},
		{ID: "C5.1", Title: "At the count limit the consumer stops taking messages; another consumer gets them", Req: []int{5}, Tiers: both, Needs: []provider.Class{flowCtl}, Run: func(e *runner.Env) error { return stopTaking(e, false) }},
		{ID: "C5.2", Title: "At the byte limit the consumer stops taking messages; another consumer gets them", Req: []int{5}, Tiers: both, Bytes: true, Needs: []provider.Class{flowCtl}, Run: func(e *runner.Env) error { return stopTaking(e, true) }},
		{ID: "C10.1", Title: "Every message is handled once and removed when acked", Req: []int{10}, Tiers: both, Run: allAck},
		{ID: "C10.2", Title: "A nacked message is redelivered", Req: []int{10}, Tiers: both, Run: nackOnce},
		{ID: "C10.3", Title: "A handler past the visibility timeout gets the message redelivered", Req: []int{10}, Tiers: both, Needs: []provider.Class{leaseTmr}, Run: exceedTimeout},
		{ID: "C10.4", Title: "Repeated failures end in the dead-letter queue", Req: []int{10}, Tiers: both, Needs: []provider.Class{provider.ClassDeadLetter}, Run: deadLetter},
		{ID: "C12.1", Title: "Shutdown finishes in-progress work and returns the rest at once", Req: []int{12}, Tiers: both, Run: shutdown},
		{ID: "C13.1", Title: "Restart mid-backlog loses nothing and resumes quickly", Req: []int{13}, Tiers: both, Run: restart},
		{ID: "C15.1", Title: "A missing queue at start surfaces as an error", Req: []int{15}, Tiers: both, Run: missingQueue},
	}
}

// ---- helpers ----

func small(e *runner.Env) payload.Spec {
	s, err := payload.ParseSpec(e.R.Profile.Payloads.Small)
	if err != nil {
		panic(err)
	}
	s, _ = e.R.Capped(s)
	return s
}

func large(e *runner.Env) payload.Spec {
	s, err := payload.ParseSpec(e.R.Profile.Payloads.Large)
	if err != nil {
		panic(err)
	}
	s, capped := e.R.Capped(s)
	if capped {
		e.Setting("large_payload_capped_to", payload.FormatBytes(int64(s.Size)))
	}
	return s
}

func hnd(lat, jit time.Duration) sut.HandlerSpec { return sut.HandlerSpec{Latency: lat, Jitter: jit} }

func start(e *runner.Env, o runner.WorkerOpts) (*runner.Worker, error) {
	w, err := e.StartWorker(o)
	if err != nil {
		return nil, err
	}
	return w, w.WaitReady(60 * time.Second)
}

// drainBudget is a generous time for n messages through a consumer handling
// conc at a time for d each, plus room for timeouts.
func drainBudget(e *runner.Env, n, conc int, d time.Duration) time.Duration {
	return time.Duration(n/max(1, conc)+1)*d*3 + 2*e.T + 30*time.Second
}

func grace(e *runner.Env) time.Duration { return e.R.Profile.GracePeriod.D() }

// waitUntil polls cond every 100 ms until true or timeout.
func waitUntil(e *runner.Env, timeout time.Duration, cond func(a *runner.Analysis) bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && e.Ctx.Err() == nil {
		if cond(e.Analyze()) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func startedBy(a *runner.Analysis, w string) int {
	n := 0
	for _, inv := range a.Invs {
		if inv.W == w {
			n++
		}
	}
	return n
}

// processed records "every message processed" as a requirement check.
func processed(e *runner.Env, ok bool, req int) {
	n, total := e.Settled()
	e.Check("all-processed", req, ok, "every message acked or dead-lettered", fmt.Sprintf("%d/%d", n, total))
}

// consumerExtras counts, per message, invocations beyond the first that the
// broker's own duplicates don't explain (R11 allows those).
func consumerExtras(a *runner.Analysis) map[string]int {
	m := map[string]int{}
	for _, x := range a.Extras() {
		if x.Class != runner.ExtraBroker {
			m[x.ID]++
		}
	}
	return m
}

func drained(e *runner.Env, ok bool, req int) {
	n, total := e.Settled()
	e.Precondition("drained", req, ok, "every message acked or dead-lettered", fmt.Sprintf("%d/%d", n, total))
}

func hiddenMetrics(e *runner.Env, a *runner.Analysis, from, to time.Time) (int64, bool) {
	sus, peak, ok := a.HiddenHeld(from, to, time.Second)
	if ok {
		e.Metric("hidden_held_sustained", sus)
		e.Metric("hidden_held_peak", peak)
	} else {
		e.Metric("hidden_held", "not observable: broker has no in-flight count")
	}
	vis, visB := a.MaxVisibleHeld("")
	e.Metric("visible_held_max", vis)
	e.Metric("visible_held_bytes_max", visB)
	return sus, ok
}

// ---- R1 count ----

func countBacklog(e *runner.Env) error {
	n := e.R.Profile.Limits.Count
	msgs := 20 * n
	if _, err := e.Publish(msgs, small(e), nil); err != nil {
		return err
	}
	h := hnd(50*time.Millisecond, 25*time.Millisecond)
	w, err := start(e, runner.WorkerOpts{Count: n, Handler: h})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, msgs, n, h.Latency), false)
	w.Stop(grace(e))
	a := e.Analyze()
	got := a.MaxHandled("")
	e.Check("handled<=N", 1, got <= n, fmt.Sprintf("≤ %d at once", n), fmt.Sprintf("max %d", got), flowCtl)
	drained(e, ok, 1)
	hiddenMetrics(e, a, time.Time{}, time.Now())
	e.Metric("rss_peak", a.MaxRSS(""))
	return nil
}

func countOne(e *runner.Env) error {
	if _, err := e.Publish(30, small(e), nil); err != nil {
		return err
	}
	h := hnd(100*time.Millisecond, 0)
	w, err := start(e, runner.WorkerOpts{Count: 1, Handler: h})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, 30, 1, h.Latency), false)
	w.Stop(grace(e))
	a := e.Analyze()
	got := a.MaxHandled("")
	e.Check("handled<=1", 1, got <= 1, "≤ 1 at once", fmt.Sprintf("max %d", got), flowCtl)
	drained(e, ok, 1)
	hiddenMetrics(e, a, time.Time{}, time.Now())
	return nil
}

// ---- R2, R3 bytes ----

func bytesBacklog(e *runner.Env) error {
	l := large(e)
	b := int64(16 * l.Size)
	msgs := 64
	e.Setting("byte_limit", payload.FormatBytes(b))
	e.Setting("payload", l.String())
	if _, err := e.Publish(msgs, l, nil); err != nil {
		return err
	}
	h := hnd(e.T/5, 0)
	w, err := start(e, runner.WorkerOpts{Count: 1000, Bytes: b, Handler: h})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, msgs, 16, h.Latency), false)
	w.Stop(grace(e))
	a := e.Analyze()
	bound := b + int64(l.Size)
	hb := a.MaxHandledBytes("")
	e.Check("handled-bytes<=B+1msg", 2, hb <= bound, fmt.Sprintf("≤ %s", payload.FormatBytes(bound)), payload.FormatBytes(hb), flowCtl)
	var vis int64
	for _, ss := range a.Samples {
		for _, s := range ss {
			vis = max(vis, s.HandledB+s.VisHeldB)
		}
	}
	e.Check("visible-held+handled-bytes<=B+1msg", 2, vis <= bound, fmt.Sprintf("≤ %s", payload.FormatBytes(bound)), payload.FormatBytes(vis), flowCtl)
	if sus, ok := hiddenMetrics(e, a, time.Time{}, time.Now()); ok {
		hidden := sus * int64(l.Size)
		e.Check("hidden-held-bytes", 2, vis+hidden <= bound, fmt.Sprintf("held in client + handled ≤ %s", payload.FormatBytes(bound)),
			fmt.Sprintf("%s hidden (%d msgs) + %s", payload.FormatBytes(hidden), sus, payload.FormatBytes(vis)), flowCtl)
	} else {
		e.Unobservable("hidden-held-bytes", 2, "held in client + handled ≤ B + 1 message", "broker has no in-flight count")
	}
	drained(e, ok, 2)
	e.Metric("rss_peak", a.MaxRSS(""))
	return nil
}

func oversized(e *runner.Env) error {
	l := large(e)
	b := int64(l.Size / 2)
	e.Setting("byte_limit", payload.FormatBytes(b))
	e.Setting("payload", l.String())
	if _, err := e.Publish(6, l, nil); err != nil {
		return err
	}
	h := hnd(time.Second, 0)
	w, err := start(e, runner.WorkerOpts{Count: 50, Bytes: b, Handler: h})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, 6, 1, h.Latency), false)
	w.Stop(grace(e))
	a := e.Analyze()
	got := a.MaxHandled("")
	e.Check("one-at-a-time", 3, got <= 1, "1 at once", fmt.Sprintf("max %d", got), flowCtl)
	n, total := e.Settled()
	e.Check("queue-moves", 3, ok, "all handled", fmt.Sprintf("%d/%d", n, total))
	hiddenMetrics(e, a, time.Time{}, time.Now())
	return nil
}

// ---- R4 + R11 waiting ----

// waiting: a backlog that keeps messages waiting for a free slot (count, or
// bytes) for several visibility timeouts. Each handler uses 60 % of the
// timeout, so any wait under the timer longer than 40 % of it ends in a
// redelivery.
func waiting(e *runner.Env, bytes bool) error {
	th := e.R.Profile.Thresholds
	h := hnd(e.T*3/5, 0)
	var (
		o    = runner.WorkerOpts{Handler: h}
		spec payload.Spec
		conc int
	)
	if bytes {
		spec = large(e)
		o.Count = 1000
		o.Bytes = int64(4 * spec.Size)
		conc = 4
		e.Setting("byte_limit", payload.FormatBytes(o.Bytes))
	} else {
		spec = small(e)
		o.Count = e.R.Profile.Limits.Count
		conc = o.Count
	}
	msgs := conc * 5 // 5 handler rounds: the last messages wait 2.4 x T
	e.Setting("messages", msgs)
	e.Setting("payload", spec.String())
	if _, err := e.Publish(msgs, spec, nil); err != nil {
		return err
	}
	w, err := start(e, o)
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, msgs, conc, h.Latency), true)
	w.Stop(grace(e))
	_ = e.PollDLQ()
	a := e.Analyze()

	waits := a.Waits()
	p99, mx := runner.Percentile(waits, 99), runner.MaxDur(waits)
	maxAllowed := time.Duration(float64(e.T) * th.WaitMaxFraction)
	if len(waits) == 0 {
		e.Precondition("waits-recorded", 4, false, "handler starts with a receive time", "none recorded")
	} else {
		e.Check("wait-p99", 4, p99 <= th.WaitP99.D(), "≤ "+th.WaitP99.D().String(), runner.FmtDur(p99))
		e.Check("wait-max", 4, mx <= maxAllowed, "≤ "+maxAllowed.String(), runner.FmtDur(mx))
	}
	xs := runner.CountExtras(a.Extras())
	e.Check("no-redelivery-from-waiting", 4, xs[runner.ExtraHeldTooLong] == 0, "0 messages redelivered or re-attempted because they waited",
		fmt.Sprintf("%d held past the timeout; extras %v", xs[runner.ExtraHeldTooLong], xs))
	dl := len(e.DLQ())
	e.Check("none-dead-lettered", 4, dl == 0, "0", fmt.Sprintf("%d", dl), provider.ClassDeadLetter)
	dups := 0
	for _, invs := range a.ByID {
		if len(invs) > 1 {
			dups += len(invs) - 1
		}
	}
	e.Check("no-consumer-duplicates", 11, xs[runner.ExtraHeldTooLong] == 0, "0 extra invocations caused by holding",
		fmt.Sprintf("%d extra invocations, %d from holding, %d unexplained (broker)", dups, xs[runner.ExtraHeldTooLong], xs[runner.ExtraBroker]))
	e.Metric("extras", xs)
	hiddenMetrics(e, a, time.Time{}, time.Now())
	drained(e, ok, 4)
	return nil
}

// ---- R5 stop taking ----

func stopTaking(e *runner.Env, bytes bool) error {
	th := e.R.Profile.Thresholds
	h := hnd(e.T/2, 0)
	o := runner.WorkerOpts{Handler: h}
	var spec payload.Spec
	if bytes {
		spec = large(e)
		o.Count = 1000
		o.Bytes = int64(4 * spec.Size)
		e.Setting("byte_limit", payload.FormatBytes(o.Bytes))
	} else {
		spec = small(e)
		o.Count = e.R.Profile.Limits.Count
	}
	msgs := 30
	if !bytes {
		msgs = 6 * o.Count
	}
	if _, err := e.Publish(msgs, spec, nil); err != nil {
		return err
	}
	a1, err := start(e, o)
	if err != nil {
		return err
	}
	// A alone: let it fill up, then watch for a few seconds.
	if !waitUntil(e, 30*time.Second, func(a *runner.Analysis) bool { return startedBy(a, a1.ID) > 0 }) {
		return fmt.Errorf("first consumer handled nothing in 30s")
	}
	e.Sleep(time.Second)
	from := time.Now()
	e.Sleep(3 * time.Second)
	to := time.Now()
	b1, err := start(e, o)
	if err != nil {
		return err
	}
	gotB := waitUntil(e, 10*time.Second, func(a *runner.Analysis) bool { return startedBy(a, b1.ID) > 0 })
	a := e.Analyze()
	a1.Kill()
	b1.Kill()

	vis := a.MaxVisibleHeldIn(a1.ID, from, to)
	sus, peak, ok := a.HiddenHeld(from, to, time.Second)
	want := fmt.Sprintf("≤ %d message beyond those handled", th.HeldBeyondLimit)
	if ok {
		held := int64(vis) + sus
		e.Check("held-beyond-limit", 5, held <= int64(th.HeldBeyondLimit), want,
			fmt.Sprintf("%d (visible %d, in client %d sustained, %d peak)", held, vis, sus, peak), flowCtl)
	} else {
		e.Check("visible-held-beyond-limit", 5, vis <= th.HeldBeyondLimit, want, fmt.Sprintf("%d visible", vis), flowCtl)
		e.Unobservable("held-in-client", 5, want, "broker has no in-flight count")
	}
	var first time.Duration
	if gotB {
		for _, inv := range a.Invs {
			if inv.W == b1.ID {
				first = inv.Start.Sub(b1.ReadyAt())
				break
			}
		}
	}
	e.Check("other-consumer-gets-work", 5, gotB && first <= 2*time.Second, "second consumer's first message ≤ 2s after it subscribed",
		map[bool]string{true: runner.FmtDur(first), false: "nothing in 10s"}[gotB], flowCtl)
	return nil
}

// ---- R10 at least once ----

func allAck(e *runner.Env) error {
	n := e.R.Profile.Limits.Count
	msgs := 10 * n
	if _, err := e.Publish(msgs, small(e), nil); err != nil {
		return err
	}
	w, err := start(e, runner.WorkerOpts{Count: n, Handler: hnd(20*time.Millisecond, 10*time.Millisecond)})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, msgs, n, 20*time.Millisecond), true)
	e.Sleep(3 * time.Second) // let acks reach the broker before reading its counters
	w.Stop(grace(e))
	_ = e.PollDLQ()
	a := e.Analyze()
	processed(e, ok, 10)
	extra := consumerExtras(a)
	multi := 0
	for _, n := range extra {
		if n > 0 {
			multi++
		}
	}
	e.Check("handled-once", 10, multi == 0, "every message handled once (broker duplicates aside)",
		fmt.Sprintf("%d handled more than once; broker duplicates %d", multi, runner.CountExtras(a.Extras())[runner.ExtraBroker]))
	if len(a.Broker) > 0 && a.Broker[len(a.Broker)-1].Ready >= 0 {
		last := a.Broker[len(a.Broker)-1]
		e.Check("broker-empty", 10, last.Ready == 0 && last.InFlight <= 0, "0 ready, 0 in flight",
			fmt.Sprintf("%d ready, %d in flight", last.Ready, last.InFlight))
	} else {
		e.Unobservable("broker-empty", 10, "0 ready, 0 in flight", "broker has no backlog count")
	}
	e.Check("none-dead-lettered", 10, len(e.DLQ()) == 0, "0", fmt.Sprint(len(e.DLQ())))
	return nil
}

func nackOnce(e *runner.Env) error {
	n := e.R.Profile.Limits.Count
	msgs := 10 * n
	marked := map[string]bool{}
	ms, err := e.Publish(msgs, small(e), func(i int) string {
		if i%10 == 0 {
			return payload.DoNackOnce
		}
		return ""
	})
	if err != nil {
		return err
	}
	for _, m := range ms {
		if m.Directive == payload.DoNackOnce {
			marked[m.ID] = true
		}
	}
	w, err := start(e, runner.WorkerOpts{Count: n, Handler: hnd(20*time.Millisecond, 10*time.Millisecond)})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, msgs, n, 20*time.Millisecond)+2*time.Minute, true)
	w.Stop(grace(e))
	a := e.Analyze()
	processed(e, ok, 10)
	extra := consumerExtras(a)
	badMarked, badOther, attOK, attKnown := 0, 0, 0, 0
	for id, invs := range a.ByID {
		if marked[id] {
			if extra[id] != 1 {
				badMarked++
			}
			if len(invs) >= 2 && invs[0].Attempt > 0 {
				attKnown++
				if invs[1].Attempt == invs[0].Attempt+1 {
					attOK++
				}
			}
		} else if extra[id] != 0 {
			badOther++
		}
	}
	e.Check("nacked-redelivered", 10, badMarked == 0, fmt.Sprintf("%d nacked messages handled exactly twice", len(marked)), fmt.Sprintf("%d not", badMarked))
	e.Check("others-once", 10, badOther == 0, "the rest handled once", fmt.Sprintf("%d not", badOther))
	if attKnown > 0 {
		e.Check("attempt+1", 10, attOK == attKnown, "redelivery has attempt + 1", fmt.Sprintf("%d/%d", attOK, attKnown))
	}
	return nil
}

func exceedTimeout(e *runner.Env) error {
	ms, err := e.Publish(5, small(e), func(int) string { return payload.DoExceedOnce })
	if err != nil {
		return err
	}
	w, err := start(e, runner.WorkerOpts{Count: e.R.Profile.Limits.Count, Handler: hnd(20*time.Millisecond, 0)})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(3*e.T+30*time.Second, true)
	w.Stop(grace(e) + e.T)
	a := e.Analyze()
	processed(e, ok, 10)
	redelivered, early := 0, 0
	var gaps []time.Duration
	for _, m := range ms {
		invs := a.ByID[m.ID]
		if len(invs) >= 2 {
			redelivered++
			gap := invs[1].Start.Sub(invs[0].Start)
			gaps = append(gaps, gap)
			if gap < e.T*9/10 {
				early++
			}
		}
	}
	e.Check("redelivered-after-timeout", 10, redelivered == len(ms), "each redelivered once its timeout expired",
		fmt.Sprintf("%d/%d redelivered, gaps %v", redelivered, len(ms), gaps))
	e.Check("not-before-timeout", 10, early == 0, "no redelivery before the timeout", fmt.Sprintf("%d early", early))
	return nil
}

func deadLetter(e *runner.Env) error {
	ms, err := e.Publish(10, small(e), func(int) string { return payload.DoNackAlways })
	if err != nil {
		return err
	}
	w, err := start(e, runner.WorkerOpts{Count: e.R.Profile.Limits.Count, Handler: hnd(10*time.Millisecond, 0)})
	if err != nil {
		return err
	}
	// Brokers may back off between redeliveries (Pub/Sub: 10 s minimum).
	ok := e.WaitSettled(time.Duration(e.MaxAttempts)*(e.T+15*time.Second), true)
	w.Stop(grace(e))
	a := e.Analyze()
	dlq := e.DLQ()
	in, wrong := 0, 0
	for _, m := range ms {
		if dlq[m.ID] {
			in++
		}
		if len(a.ByID[m.ID]) != e.MaxAttempts {
			wrong++
		}
	}
	e.Check("dead-lettered", 10, ok && in == len(ms), fmt.Sprintf("all %d in the dead-letter queue", len(ms)), fmt.Sprintf("%d", in))
	e.Check("attempts-before-dlq", 10, wrong == 0, fmt.Sprintf("each handled %d times", e.MaxAttempts), fmt.Sprintf("%d differ", wrong))
	return nil
}

// ---- R12 shutdown ----

func shutdown(e *runner.Env) error {
	th := e.R.Profile.Thresholds
	n := e.R.Profile.Limits.Count
	msgs := 15 * n
	if _, err := e.Publish(msgs, small(e), nil); err != nil {
		return err
	}
	h := hnd(e.T/5, 0)
	a1, err := start(e, runner.WorkerOpts{Count: n, Handler: h})
	if err != nil {
		return err
	}
	if !waitUntil(e, 30*time.Second, func(a *runner.Analysis) bool { return startedBy(a, a1.ID) >= n }) {
		return fmt.Errorf("first consumer did not fill its limit in 30s")
	}
	e.Sleep(time.Second)
	t0 := a1.Term()
	b1, err := start(e, runner.WorkerOpts{Count: 10 * n, Handler: hnd(20*time.Millisecond, 0)})
	if err != nil {
		return err
	}
	exited := a1.Wait(grace(e))
	exitAfter := time.Since(t0)
	if !exited {
		a1.Kill()
	}
	ok := e.WaitSettled(2*e.T+30*time.Second, false)
	b1.Stop(grace(e))
	a := e.Analyze()

	e.Check("exit-within-grace", 12, exited, "exits within "+grace(e).String(), runner.FmtDur(exitAfter))
	// In-progress at SIGTERM: finished and settled by A, not handled again.
	lost, again := 0, 0
	for _, invs := range a.ByID {
		for i, inv := range invs {
			if inv.W != a1.ID || !inv.Start.Before(t0) {
				continue
			}
			if inv.End.IsZero() || inv.Outcome != sut.OutcomeAck {
				lost++
			} else if i < len(invs)-1 {
				again++
			}
		}
	}
	e.Check("in-progress-settled", 12, lost == 0 && again == 0, "handlers running at SIGTERM finish, ack, aren't handled again",
		fmt.Sprintf("%d unfinished, %d handled again", lost, again))

	// Everything A did not ack must reach B soon after SIGTERM.
	ackedByA := map[string]bool{}
	for _, inv := range a.Invs {
		if inv.W == a1.ID && inv.Outcome == sut.OutcomeAck {
			ackedByA[inv.ID] = true
		}
	}
	startB := t0
	if r := b1.ReadyAt(); r.After(startB) {
		startB = r
	}
	var worst time.Duration
	worstID := ""
	// Messages that came back with an extra attempt: returned at once (within
	// the hand-back threshold: the broker's own counting, possibly by design)
	// or only later (left to expire: the consumer's doing).
	returnedExtra, expiredExtra, attKnown, missing := 0, 0, false, 0
	startedByA := map[string]bool{}
	for _, inv := range a.Invs {
		if inv.W == a1.ID {
			startedByA[inv.ID] = true
		}
	}
	for id, invs := range a.ByID {
		if ackedByA[id] {
			continue
		}
		var first *runner.Inv
		for i := range invs {
			if invs[i].W == b1.ID {
				first = &invs[i]
				break
			}
		}
		if first == nil {
			missing++
			continue
		}
		if d := first.Start.Sub(startB); d > worst {
			worst, worstID = d, id
		}
		if first.Attempt > 0 {
			attKnown = true
			if first.Attempt > 1 && !startedByA[id] {
				if first.Start.Sub(startB) <= th.HandBack.D() {
					returnedExtra++
				} else {
					expiredExtra++
				}
			}
		}
	}
	for id := range a.Msgs {
		if _, seen := a.ByID[id]; !seen {
			missing++
		}
	}
	if left := len(a.Msgs) - len(ackedByA); left == 0 {
		e.Unobservable("hand-back", 12, fmt.Sprintf("messages not handled reach another consumer ≤ %s after shutdown", th.HandBack.D()),
			"the stopping consumer handled every message itself, so nothing was left to hand back (no broker flow control?)")
	} else {
		e.Check("hand-back", 12, missing == 0 && worst <= th.HandBack.D(),
			fmt.Sprintf("messages not handled reach another consumer ≤ %s after shutdown", th.HandBack.D()),
			fmt.Sprintf("slowest %s (%s) of %d, %d never handled", runner.FmtDur(worst), worstID, left, missing))
	}
	if attKnown {
		e.Check("no-extra-attempt-on-return", 12, returnedExtra == 0, "messages returned at shutdown keep their attempt count",
			fmt.Sprintf("%d returned within %s counted an extra attempt", returnedExtra, th.HandBack.D()))
		e.Check("no-extra-attempt-from-expiry", 12, expiredExtra == 0, "no message left to expire under its visibility timeout",
			fmt.Sprintf("%d came back later than %s with an extra attempt", expiredExtra, th.HandBack.D()))
	} else {
		e.Unobservable("no-extra-attempt", 12, "returned messages keep their attempt count", "the queue client doesn't expose delivery attempts on this broker")
	}
	if ex := a.Exits[a1.ID]; ex != nil {
		e.Metric("received_not_started_at_exit", len(ex.Unstarted))
	}
	e.Metric("drained", ok)
	return nil
}

// ---- R13 restart ----

func restart(e *runner.Env) error {
	th := e.R.Profile.Thresholds
	n := e.R.Profile.Limits.Count
	msgs := 30 * n
	if _, err := e.Publish(msgs, small(e), nil); err != nil {
		return err
	}
	h := hnd(50*time.Millisecond, 0)
	a1, err := start(e, runner.WorkerOpts{Count: n, Handler: h})
	if err != nil {
		return err
	}
	if !waitUntil(e, 30*time.Second, func(a *runner.Analysis) bool { return startedBy(a, a1.ID) >= 2*n }) {
		return fmt.Errorf("consumer handled too little in 30s")
	}
	a1.Stop(grace(e))
	a2, err := start(e, runner.WorkerOpts{Count: n, Handler: h})
	if err != nil {
		return err
	}
	ok := e.WaitSettled(drainBudget(e, msgs, n, h.Latency), false)
	a2.Stop(grace(e))
	a := e.Analyze()
	processed(e, ok, 13)
	var first time.Duration = -1
	for _, inv := range a.Invs {
		if inv.W == a2.ID {
			first = inv.Start.Sub(a2.ReadyAt())
			break
		}
	}
	// Needs broker flow control: without it (emulators) the first process
	// pulls the whole backlog into its client and the restarted one waits for
	// those leases to expire, whatever the consumer does.
	e.Check("first-after-restart", 13, first >= 0 && first <= th.FirstAfterRestart.D(), "≤ "+th.FirstAfterRestart.D().String()+" after subscribing", runner.FmtDur(first), flowCtl)
	e.Metric("extras", runner.CountExtras(a.Extras()))
	return nil
}

// ---- R15 errors ----

func missingQueue(e *runner.Env) error {
	th := e.R.Profile.Thresholds
	missing := &provider.Target{Spec: e.Target.Spec, Data: map[string]string{}}
	missing.Spec.Name = e.Target.Spec.Name + "-missing"
	w, err := e.StartWorker(runner.WorkerOpts{Count: e.R.Profile.Limits.Count, Target: missing, Handler: hnd(10*time.Millisecond, 0)})
	if err != nil {
		return err
	}
	t0 := time.Now()
	var surfacedAt time.Duration = -1
	how := ""
	deadline := t0.Add(th.ErrorWithin.D())
	for time.Now().Before(deadline) {
		select {
		case <-w.Done():
			surfacedAt, how = time.Since(t0), "process exited: "+w.ExitReason()
		default:
		}
		if surfacedAt >= 0 {
			break
		}
		a := e.Analyze()
		if ss := a.Samples[w.ID]; len(ss) > 0 && ss[len(ss)-1].WorkerFailed {
			surfacedAt, how = time.Since(t0), "worker reported failed"
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	w.Kill()
	errs := w.ErrorLogs()
	e.Check("error-surfaced", 15, surfacedAt >= 0, "worker fails or exits within "+th.ErrorWithin.D().String(),
		map[bool]string{true: runner.FmtDur(surfacedAt) + ", " + how, false: "still running, no failure reported"}[surfacedAt >= 0])
	e.Check("error-logged", 15, errs > 0, "an error is logged", fmt.Sprintf("%d error log lines", errs))
	return nil
}
