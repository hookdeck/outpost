package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/payload"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqcheck/sut"
)

// Step is one rate step of the sweep.
type Step struct {
	Rate         int     `json:"rate"`
	Published    int     `json:"published"`
	PublishRate  float64 `json:"publish_rate"`
	ConsumeRate  float64 `json:"consume_rate"`
	BacklogEnd   int     `json:"backlog_end"`
	QueueP50     string  `json:"queue_latency_p50"`
	QueueP95     string  `json:"queue_latency_p95"`
	QueueP99     string  `json:"queue_latency_p99"`
	QueueMax     string  `json:"queue_latency_max"`
	E2EP50       string  `json:"e2e_p50"`
	E2EP99       string  `json:"e2e_p99"`
	RSSPeak      int64   `json:"rss_peak"`
	CPUCores     float64 `json:"cpu_cores"`
	Redelivered  int     `json:"redelivered"`
	OK           bool    `json:"ok"`
	Why          string  `json:"why,omitempty"`
	GeneratorCap bool    `json:"limited_by_generator,omitempty"`

	queueP99 time.Duration
	queueP50 time.Duration
}

// Drain is one large-payload backlog drain.
type Drain struct {
	Payload      string  `json:"payload"`
	Count        int     `json:"count"`
	Seconds      float64 `json:"seconds"`
	MBPerSec     float64 `json:"mb_per_s"`
	MsgPerSec    float64 `json:"msg_per_s"`
	RSSPeak      int64   `json:"rss_peak"`
	HandledMax   int     `json:"handled_max"`
	HandledBytes int64   `json:"handled_bytes_max"`
	Redelivered  int     `json:"redelivered"`
	Drained      bool    `json:"drained"`
	Settled      int     `json:"settled"` // messages acked; rates count only these
	// RSSBound is the memory bound judged when a byte limit is set; 0 = not judged.
	RSSBound int64  `json:"rss_bound,omitempty"`
	CappedTo string `json:"capped_to,omitempty"`
}

// Envelope is the capacity result.
type Envelope struct {
	Status        string         `json:"status"`
	Reason        string         `json:"reason,omitempty"`
	Settings      map[string]any `json:"settings"`
	Steps         []Step         `json:"steps"`
	MaxRate       int            `json:"max_sustained_rate"`
	MaxRateNote   string         `json:"max_rate_note,omitempty"`
	TargetRate    int            `json:"target_rate"`
	TargetMet     bool           `json:"target_met"`
	LatencyTarget string         `json:"latency_target"`
	Drains        []Drain        `json:"drains"`
}

// Capacity runs the rate sweep and large-payload drains.
func (r *Runner) Capacity(ctx context.Context) *Envelope {
	cp := r.Profile.Capacity
	env := &Envelope{Settings: map[string]any{}}
	if !cp.Enabled {
		env.Status, env.Reason = StatusSkipped, "profile has capacity disabled"
		return env
	}
	if why, ok := r.Caps.Unobservable[provider.ClassCapacity]; ok {
		env.Status, env.Reason = StatusNotObservable, why
		return env
	}
	spec, err := payload.ParseSpec(cp.Payload)
	if err != nil {
		env.Status, env.Reason = StatusError, err.Error()
		return env
	}
	spec, _ = r.Capped(spec)
	var bytesLimit int64
	env.Settings["bytes_limit"] = "unset"
	if cp.Limits.Bytes != "" {
		if !r.HasBytes {
			env.Settings["bytes_limit"] = cp.Limits.Bytes + " (not in this build: unset)"
		} else if bytesLimit, err = payload.ParseBytes(cp.Limits.Bytes); err != nil {
			env.Status, env.Reason = StatusError, err.Error()
			return env
		} else {
			env.Settings["bytes_limit"] = payload.FormatBytes(bytesLimit)
		}
	}
	h := sut.HandlerSpec{Latency: cp.Handler.Latency.D(), Jitter: cp.Handler.Jitter.D()}
	env.Settings["count_limit"] = cp.Limits.Count
	env.Settings["handler"] = fmt.Sprintf("%s ± %s", h.Latency, h.Jitter)
	env.Settings["payload"] = spec.String()
	env.TargetRate = cp.Targets.MinRate
	env.LatencyTarget = fmt.Sprintf("queue latency p50 ≤ %s, p99 ≤ %s", cp.Targets.QueueLatencyP50.D(), cp.Targets.QueueLatencyP99.D())
	wo := WorkerOpts{Count: cp.Limits.Count, Bytes: bytesLimit, Handler: h}

	for _, rate := range cp.RateSteps {
		st, err := r.rateStep(ctx, rate, spec, wo)
		if err != nil {
			env.Status, env.Reason = StatusError, fmt.Sprintf("rate %d/s: %v", rate, err)
			return env
		}
		env.Steps = append(env.Steps, st)
		r.logf("capacity %d/s: consumed %.0f/s, p99 %s, ok=%v %s", rate, st.ConsumeRate, st.QueueP99, st.OK, st.Why)
		if !st.OK {
			if st.GeneratorCap {
				env.MaxRateNote = fmt.Sprintf("load generator could not reach %d/s; the consumer may go higher", rate)
			}
			break
		}
		env.MaxRate = rate
		if rate >= cp.Targets.MinRate && cp.Targets.MinRate > 0 {
			env.TargetMet = true
		}
	}
	if len(env.Steps) > 0 && env.Steps[len(env.Steps)-1].OK {
		env.MaxRateNote = "highest step passed; the ceiling is higher"
	}
	for i, l := range cp.Large {
		ls, err := payload.ParseSpec(l.Payload)
		if err != nil {
			env.Status, env.Reason = StatusError, err.Error()
			return env
		}
		d, err := r.drain(ctx, i+1, ls, l.Count, wo)
		if err != nil {
			env.Status, env.Reason = StatusError, fmt.Sprintf("drain %s: %v", l.Payload, err)
			return env
		}
		env.Drains = append(env.Drains, d)
		r.logf("capacity drain %s x%d: %.1f MB/s, RSS %s", d.Payload, d.Count, d.MBPerSec, payload.FormatBytes(d.RSSPeak))
	}
	env.Status, env.Reason = judgeEnvelope(env, cp.Targets.MinRate, r.Caps.CapacityNotJudged)
	return env
}

// judgeEnvelope folds what was judged: FAIL when the rate target was missed,
// a drain didn't finish or went over its memory bound; MEASURED when nothing
// failed but something had no target (no rate target, a drain without a byte
// limit to bound memory) or the broker is an emulation; PASS only when every
// part was judged and passed.
func judgeEnvelope(env *Envelope, minRate int, notJudged string) (string, string) {
	var fails, unjudged, stalls []string
	if minRate > 0 && !env.TargetMet {
		if n := len(env.Steps); n > 0 && env.Steps[n-1].GeneratorCap && env.Steps[n-1].Rate <= minRate {
			// The publisher, not the consumer, fell short of the target.
			return StatusError, fmt.Sprintf("load generator could not reach %d/s (%s): the %d/s target was not tested", env.Steps[n-1].Rate, env.Steps[n-1].Why, minRate)
		}
		fails = append(fails, fmt.Sprintf("did not sustain %d/s within the latency target", minRate))
	}
	if minRate <= 0 && len(env.Steps) > 0 {
		unjudged = append(unjudged, "no rate target")
		for _, st := range env.Steps {
			if !st.OK {
				unjudged = append(unjudged, fmt.Sprintf("%d/s step failed: %s", st.Rate, st.Why))
			}
		}
	}
	for _, d := range env.Drains {
		switch {
		case !d.Drained:
			stalls = append(stalls, fmt.Sprintf("%s drain did not finish (%d/%d)", d.Payload, d.Settled, d.Count))
		case d.RSSBound == 0:
			unjudged = append(unjudged, d.Payload+" memory not judged (no byte limit)")
		case d.RSSPeak > d.RSSBound:
			fails = append(fails, fmt.Sprintf("%s RSS %s over %s", d.Payload, payload.FormatBytes(d.RSSPeak), payload.FormatBytes(d.RSSBound)))
		}
	}
	switch {
	case len(stalls) > 0:
		// A drain that stalls fails on any broker, emulated or not.
		return StatusFail, strings.Join(append(stalls, fails...), "; ")
	case notJudged != "":
		return StatusMeasured, strings.Join(append([]string{notJudged}, append(fails, unjudged...)...), "; ")
	case len(fails) > 0:
		return StatusFail, strings.Join(fails, "; ")
	case len(unjudged) > 0:
		return StatusMeasured, strings.Join(unjudged, "; ")
	}
	return StatusPass, ""
}

func (r *Runner) capEnv(ctx context.Context, id string) (*Env, error) {
	return r.newEnv(ctx, Case{ID: id})
}

func (r *Runner) rateStep(ctx context.Context, rate int, spec payload.Spec, wo WorkerOpts) (Step, error) {
	cp := r.Profile.Capacity
	st := Step{Rate: rate}
	e, err := r.capEnv(ctx, fmt.Sprintf("rate-%d", rate))
	if err != nil {
		return st, err
	}
	defer e.close()
	w, err := e.StartWorker(wo)
	if err != nil {
		return st, err
	}
	if err := w.WaitReady(60 * time.Second); err != nil {
		return st, err
	}
	warm, hold := cp.Warmup.D(), cp.StepHold.D()
	t0 := time.Now()
	sent, err := e.PublishRate(rate, warm+hold, spec)
	if err != nil {
		return st, err
	}
	pubEnd := time.Now()
	from := t0.Add(warm)
	// Consumption in the hold window, then a short wait to see the backlog.
	a := e.Analyze()
	started := 0
	for _, invs := range a.ByID {
		if first := invs[0].Start; !first.Before(from) && first.Before(pubEnd) {
			started++
		}
	}
	backlog := sent - len(a.ByID)
	e.Sleep(2 * time.Second)
	cpuStart, cpuEnd := cpuAt(a, w.ID, from), cpuAt(a, w.ID, pubEnd)
	w.Kill()
	a = e.Analyze()

	window := pubEnd.Sub(from).Seconds()
	st.Published = sent
	st.PublishRate = float64(sent) / pubEnd.Sub(t0).Seconds()
	st.ConsumeRate = float64(started) / window
	st.BacklogEnd = backlog
	q, e2e := a.QueueLatency(from, pubEnd)
	st.queueP50, st.queueP99 = Percentile(q, 50), Percentile(q, 99)
	st.QueueP50, st.QueueP95, st.QueueP99, st.QueueMax = FmtDur(st.queueP50), FmtDur(Percentile(q, 95)), FmtDur(st.queueP99), FmtDur(MaxDur(q))
	st.E2EP50, st.E2EP99 = FmtDur(Percentile(e2e, 50)), FmtDur(Percentile(e2e, 99))
	st.RSSPeak = a.MaxRSS(w.ID)
	if cpuEnd > cpuStart {
		st.CPUCores = float64(cpuEnd-cpuStart) / float64(pubEnd.Sub(from).Nanoseconds())
	}
	for _, x := range a.Extras() {
		if x.Class != ExtraAfterShutdown {
			st.Redelivered++
		}
	}
	switch {
	case st.PublishRate < 0.95*float64(rate):
		st.GeneratorCap = true
		st.Why = fmt.Sprintf("published only %.0f/s", st.PublishRate)
	case st.ConsumeRate < 0.95*float64(rate):
		st.Why = fmt.Sprintf("consumed %.0f/s", st.ConsumeRate)
	case backlog > max(rate, 2*wo.Count):
		st.Why = fmt.Sprintf("backlog %d at the end", backlog)
	case cp.Targets.QueueLatencyP99 > 0 && st.queueP99 > cp.Targets.QueueLatencyP99.D():
		st.Why = "queue latency p99 " + st.QueueP99 + " over target"
	case cp.Targets.QueueLatencyP50 > 0 && st.queueP50 > cp.Targets.QueueLatencyP50.D():
		st.Why = "queue latency p50 " + st.QueueP50 + " over target"
	default:
		st.OK = true
	}
	return st, nil
}

func cpuAt(a *Analysis, w string, t time.Time) int64 {
	s, ok := a.workerAt(w, t)
	if !ok {
		return 0
	}
	return s.CPUNanos
}

func (r *Runner) drain(ctx context.Context, n int, spec payload.Spec, count int, wo WorkerOpts) (Drain, error) {
	d := Drain{Payload: spec.String(), Count: count}
	// Named from the requested payload and its position: two payloads capped
	// to the same size must not reuse a queue name.
	e, err := r.capEnv(ctx, fmt.Sprintf("drain-%d-%s", n, spec.String()))
	spec, capped := r.Capped(spec)
	if capped {
		d.CappedTo = payload.FormatBytes(int64(spec.Size))
	}
	if err != nil {
		return d, err
	}
	defer e.close()
	if _, err := e.Publish(count, spec, nil); err != nil {
		return d, err
	}
	w, err := e.StartWorker(wo)
	if err != nil {
		return d, err
	}
	if err := w.WaitReady(60 * time.Second); err != nil {
		return d, err
	}
	t0 := w.ReadyAt()
	d.Drained = e.WaitSettled(10*time.Minute, false)
	el := time.Since(t0)
	w.Stop(r.Profile.GracePeriod.D())
	a := e.Analyze()
	settled, _ := e.Settled()
	d.Seconds = el.Seconds()
	d.Settled = settled
	d.MsgPerSec = float64(settled) / el.Seconds()
	d.MBPerSec = float64(settled) * float64(spec.Size) / (1 << 20) / el.Seconds()
	d.RSSPeak = a.MaxRSS(w.ID)
	if wo.Bytes > 0 {
		// Memory bound with a byte limit: the idle process plus 3 x (limit +
		// one message) for copies made while decoding and handling.
		d.RSSBound = a.IdleRSS(w.ID) + 3*(wo.Bytes+int64(spec.Size))
	}
	d.HandledMax = a.MaxHandled(w.ID)
	d.HandledBytes = a.MaxHandledBytes(w.ID)
	for _, x := range a.Extras() {
		if x.Class != ExtraAfterShutdown {
			d.Redelivered++
		}
	}
	return d, nil
}
