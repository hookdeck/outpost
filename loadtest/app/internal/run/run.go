package run

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hookdeck/outpost/loadtest/app/internal/group"
	"github.com/hookdeck/outpost/loadtest/app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest/app/internal/publisher"
)

// Phase names are metric label values, so they are part of the query surface.
const (
	PhasePending  = "pending"
	PhaseWarmup   = "warmup" // discarded: pools, caches, autoscaling settling
	PhaseSteady   = "steady" // the measured window
	PhaseDrain    = "drain"  // publishing stopped, deliveries still landing
	PhaseComplete = "complete"
	PhaseFailed   = "failed"
	PhaseAborted  = "aborted"
)

// Run is one execution of a Spec. Only one is active at a time: the whole
// point is that every profile shares a single deployment, so a second
// concurrent run would contaminate the first.
type Run struct {
	ID        string    `json:"id"`
	Spec      *Spec     `json:"spec"`
	Budget    Budget    `json:"budget"`
	Phase     string    `json:"phase"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`

	// PhaseStarts records when each phase began, which is what makes the
	// steady window queryable in Prometheus after the fact.
	PhaseStarts map[string]time.Time `json:"phase_starts"`

	// StoppedEarly marks a run whose steady window was cut short on request.
	// It is not a void: the window that did run was measured normally and
	// drained normally, it is simply shorter than the spec asked for.
	StoppedEarly bool `json:"stopped_early,omitempty"`

	Error string   `json:"error,omitempty"`
	Voids []string `json:"voids,omitempty"` // conditions that invalidate the result
}

// SteadyWindow is the interval a report may draw from.
func (r *Run) SteadyWindow() (start, end time.Time) {
	start = r.PhaseStarts[PhaseSteady]
	if d, ok := r.PhaseStarts[PhaseDrain]; ok {
		end = d
	} else {
		end = time.Now()
	}
	return start, end
}

type Controller struct {
	store       *group.Store
	provisioner *group.Provisioner
	publisher   *publisher.Publisher
	tracker     *metrics.InFlightTracker
	exportDir   string

	mu      sync.RWMutex
	current *Run
	cancel  context.CancelFunc
	done    chan struct{}
	stop    chan struct{}
}

func NewController(
	store *group.Store,
	provisioner *group.Provisioner,
	pub *publisher.Publisher,
	tracker *metrics.InFlightTracker,
	exportDir string,
) *Controller {
	return &Controller{
		store:       store,
		provisioner: provisioner,
		publisher:   pub,
		tracker:     tracker,
		exportDir:   exportDir,
	}
}

func (c *Controller) Current() *Run {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

func (c *Controller) active() bool {
	if c.current == nil {
		return false
	}
	switch c.current.Phase {
	case PhaseComplete, PhaseFailed, PhaseAborted:
		return false
	}
	return true
}

// Start validates the spec, provisions every profile, and runs the phases in
// the background. It returns as soon as the run is accepted.
func (c *Controller) Start(spec *Spec) (*Run, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.active() {
		c.mu.Unlock()
		return nil, fmt.Errorf("run %s is already active in phase %s", c.current.ID, c.current.Phase)
	}

	now := time.Now()
	r := &Run{
		ID:          fmt.Sprintf("%s-%s", spec.Name, now.UTC().Format("20060102T150405Z")),
		Spec:        spec,
		Budget:      spec.Budget(),
		Phase:       PhasePending,
		StartedAt:   now,
		PhaseStarts: map[string]time.Time{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.current = r
	c.cancel = cancel
	c.done = make(chan struct{})
	c.stop = make(chan struct{})
	stop := c.stop
	c.mu.Unlock()

	c.publisher.SetRunID(r.ID)
	metrics.EventLedger.Reset()
	publishProfileInfo(r)

	go c.execute(ctx, stop, r)
	return r, nil
}

// publishProfileInfo exposes the spec's swept dimensions as series labels. It
// runs at start rather than per phase because the shape is fixed for the run,
// and Reset first so a previous run's profiles do not linger in a scrape.
func publishProfileInfo(r *Run) {
	metrics.ProfileInfo.Reset()
	for _, p := range r.Spec.Profiles {
		metrics.ProfileInfo.With(prometheus.Labels{
			"run_id":                  r.ID,
			"profile":                 p.Name,
			"tenants":                 strconv.Itoa(p.TenantCount),
			"rate_per_tenant":         strconv.Itoa(p.RatePerTenant),
			"destinations_per_tenant": strconv.Itoa(p.Destinations),
			"payload_bytes":           strconv.Itoa(p.PayloadBytes),
			"response_ms":             strconv.FormatInt(p.ResponseMs, 10),
		}).Set(1)
	}
}

// Abort stops the run immediately. Its results are marked void: a run that did
// not complete its phases is not a measurement.
func (c *Controller) Abort() error {
	c.mu.Lock()
	if !c.active() {
		c.mu.Unlock()
		return fmt.Errorf("no active run")
	}
	cancel, done := c.cancel, c.done
	c.mu.Unlock()

	cancel()
	<-done
	return nil
}

// Stop ends the steady window early and lets the run finish the way it
// normally would: publishing stops, in-flight deliveries drain, the ledger
// closes, the export is written.
//
// This is the difference between cutting a long run short and losing it.
// Abort tears the run down mid-flight, so its in-flight events are neither
// delivered nor cut off and the ledger cannot balance — which is exactly the
// condition that voids a run. A 24 h window that has to end at hour 9 should
// yield nine hours of measurement, not nothing.
//
// It returns immediately; the drain runs in the background. Callers that need
// the export should poll for the complete phase.
func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active() {
		return fmt.Errorf("no active run")
	}
	select {
	case <-c.stop: // already stopping — repeated requests are harmless
	default:
		close(c.stop)
		c.current.StoppedEarly = true
		slog.Info("run stopping early", "run", c.current.ID, "phase", c.current.Phase)
	}
	return nil
}

func (c *Controller) setPhase(r *Run, phase string) {
	c.mu.Lock()
	r.Phase = phase
	if _, seen := r.PhaseStarts[phase]; !seen {
		r.PhaseStarts[phase] = time.Now()
	}
	c.mu.Unlock()

	for _, g := range c.store.List() {
		c.publisher.SetPhase(g.Config.Name, phase)
	}

	metrics.RunInfo.Reset()
	metrics.RunInfo.With(prometheus.Labels{
		"run_id": r.ID, "name": r.Spec.Name, "target": r.Spec.Target, "phase": phase,
	}).Set(1)
	metrics.RunPhaseStart.With(prometheus.Labels{
		"run_id": r.ID, "phase": phase,
	}).Set(float64(r.PhaseStarts[phase].UnixNano()) / 1e9)

	slog.Info("run phase", "run", r.ID, "phase", phase)
}

func (c *Controller) fail(r *Run, err error) {
	c.mu.Lock()
	r.Phase = PhaseFailed
	r.Error = err.Error()
	r.EndedAt = time.Now()
	c.mu.Unlock()
	slog.Error("run failed", "run", r.ID, "error", err)
}

func (c *Controller) execute(ctx context.Context, stop <-chan struct{}, r *Run) {
	defer close(c.done)
	defer c.publisher.SetRunID("")

	if err := c.provision(ctx, r); err != nil {
		c.teardown(r)
		if ctx.Err() != nil {
			c.abortRun(r)
			return
		}
		c.fail(r, err)
		return
	}

	// Warm-up. Every profile starts here, and all of them cross into steady
	// together — windows that don't align make per-profile comparison
	// meaningless.
	c.setPhase(r, PhaseWarmup)
	if err := c.startPublishers(r); err != nil {
		c.stopPublishers()
		c.teardown(r)
		c.fail(r, err)
		return
	}
	switch sleepPhase(ctx, stop, r.Spec.Warmup.Duration()) {
	case phaseAborted:
		c.stopPublishers()
		c.teardown(r)
		c.abortRun(r)
		return
	case phaseStopped:
		// Stopping this early still drains cleanly, but there is no steady
		// window to report on. Say so rather than publishing an empty one.
		c.void(r, "stopped during warm-up — no steady window was measured")
	}

	// A stop during warm-up leaves the channel closed, so this returns
	// immediately and the run falls through to a normal drain.
	c.setPhase(r, PhaseSteady)
	if sleepPhase(ctx, stop, r.Spec.Window.Duration()) == phaseAborted {
		c.stopPublishers()
		c.teardown(r)
		c.abortRun(r)
		return
	}

	// Drain: publishing stops, deliveries keep landing. Sweeping is suspended
	// so an event that is merely slow is not recorded as missing.
	c.setPhase(r, PhaseDrain)
	c.tracker.SetSweeping(false)
	c.stopPublishers()
	c.drain(ctx, r.Spec.Drain.Duration())
	c.cutoff(r)
	c.tracker.SetSweeping(true)

	c.checkVoids(r)

	c.mu.Lock()
	r.Phase = PhaseComplete
	r.EndedAt = time.Now()
	c.mu.Unlock()
	c.setPhase(r, PhaseComplete)

	if path, err := c.Export(r); err != nil {
		slog.Error("run export failed", "run", r.ID, "error", err)
	} else {
		slog.Info("run complete", "run", r.ID, "export", path)
	}

	c.teardown(r)
}

func (c *Controller) void(r *Run, reason string) {
	c.mu.Lock()
	r.Voids = append(r.Voids, reason)
	c.mu.Unlock()
}

func (c *Controller) abortRun(r *Run) {
	c.mu.Lock()
	r.Phase = PhaseAborted
	r.EndedAt = time.Now()
	r.Voids = append(r.Voids, "run aborted before completing its phases")
	c.mu.Unlock()
	slog.Warn("run aborted", "run", r.ID)
}

func (c *Controller) provision(ctx context.Context, r *Run) error {
	for _, p := range r.Spec.Profiles {
		cfg := p.GroupConfig()
		if existing, err := c.store.Get(cfg.Name); err == nil {
			// Reuse only if the shape matches; otherwise the run would publish
			// into destinations it did not describe.
			if existing.Config.DestinationsPerTenant == cfg.DestinationsPerTenant &&
				existing.Config.TenantCount == cfg.TenantCount {
				existing.Config = cfg
				existing.MockProfile.SetLatency(cfg.MockProfile.LatencyMs)
				existing.MockProfile.SetJitter(cfg.MockProfile.JitterMs)
				existing.MockProfile.SetErrorRate(cfg.MockProfile.ErrorRate)
				continue
			}
			if err := c.provisioner.Teardown(ctx, existing); err != nil {
				return fmt.Errorf("teardown stale profile %s: %w", cfg.Name, err)
			}
			if err := c.store.Delete(cfg.Name); err != nil {
				return err
			}
		}
		g, err := c.store.Create(cfg)
		if err != nil {
			return err
		}
		if err := c.provisioner.Provision(ctx, g); err != nil {
			return fmt.Errorf("provision profile %s: %w", cfg.Name, err)
		}
	}
	return nil
}

func (c *Controller) startPublishers(r *Run) error {
	for _, p := range r.Spec.Profiles {
		g, err := c.store.Get(p.Name)
		if err != nil {
			return err
		}
		if g.State == group.StateProvisioned || g.State == group.StateStopped {
			if err := g.Transition(group.StateRunning); err != nil {
				return err
			}
		}
		if err := c.publisher.Start(g); err != nil {
			return err
		}
		c.publisher.SetPhase(p.Name, PhaseWarmup)
	}
	return nil
}

func (c *Controller) stopPublishers() {
	for _, g := range c.store.List() {
		c.publisher.Stop(g)
		if g.State == group.StateRunning {
			_ = g.Transition(group.StateStopping)
			_ = g.Transition(group.StateStopped)
		}
	}
}

// drain waits for deliveries to stop arriving, up to the spec's drain budget.
// It exits early once in-flight goes quiet, so a clean run doesn't pay the
// full drain window.
func (c *Controller) drain(ctx context.Context, budget time.Duration) {
	deadline := time.Now().Add(budget)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var quiet int
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if c.tracker.InFlightCount() == 0 {
			quiet++
			if quiet >= 3 {
				return
			}
		} else {
			quiet = 0
		}
	}
}

// cutoff moves everything still in flight into its own bucket. These are not
// failures — the run ended while they were legitimately in transit — but they
// must be counted, or published wouldn't balance.
func (c *Controller) cutoff(r *Run) {
	remaining := c.tracker.DrainInFlight()
	for _, m := range remaining {
		metrics.Emit{RunID: r.ID, Profile: m.GroupName, Phase: m.Phase}.Cutoff()
		if g, err := c.store.Get(m.GroupName); err == nil {
			g.Metrics.RecordCutoff()
		}
	}
	if len(remaining) > 0 {
		slog.Info("run cutoff", "run", r.ID, "events", len(remaining))
	}
}

// checkVoids applies the conditions under which a run is not a measurement.
// They are recorded on the run rather than thrown away, so the reason a run
// was discarded stays visible.
func (c *Controller) checkVoids(r *Run) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Measured against the window that actually ran, not the one the spec
	// asked for. A run stopped early offers proportionally fewer events, and
	// comparing those against the full window would flag every early stop as
	// generator-bound.
	steadyStart, steadyEnd := r.SteadyWindow()
	steadySeconds := steadyEnd.Sub(steadyStart).Seconds()

	for _, p := range r.Spec.Profiles {
		counts := metrics.EventLedger.Get(r.ID, p.Name, PhaseSteady)

		if rem := counts.Remainder(); rem != 0 {
			r.Voids = append(r.Voids, fmt.Sprintf(
				"%s: ledger does not balance (%s) — harness bug, not a result", p.Name, counts))
		}
		if counts.Missing > 0 {
			r.Voids = append(r.Voids, fmt.Sprintf(
				"%s: %d events never arrived", p.Name, counts.Missing))
		}

		// Generator-bound: if we could not offer the rate we promised, the
		// generator is the thing being measured.
		g, err := c.store.Get(p.Name)
		if err != nil {
			continue
		}
		want := float64(p.Rate()) * steadySeconds
		got := float64(counts.Published + counts.PublishErrors)
		if want > 0 && got < want*0.98 {
			r.Voids = append(r.Voids, fmt.Sprintf(
				"%s: generator fell short — offered %.0f of %.0f scheduled events (%.1f%%)",
				p.Name, got, want, got/want*100))
		}
		if lag := g.Metrics.Snapshot().GeneratorLagP99; lag > 1000 {
			r.Voids = append(r.Voids, fmt.Sprintf(
				"%s: generator lag p99 %dms — latency includes our own delay", p.Name, lag))
		}
	}
}

func (c *Controller) teardown(r *Run) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, g := range c.store.List() {
		if err := c.provisioner.Teardown(ctx, g); err != nil {
			slog.Warn("teardown failed", "profile", g.Config.Name, "error", err)
			continue
		}
		_ = c.store.Delete(g.Config.Name)
	}
}

// Export writes the run artifact: spec, identity, budget, and ledger. Time
// series come from Prometheus at report time; this file is what makes the
// counts reproducible after retention rolls.
func (c *Controller) Export(r *Run) (string, error) {
	if err := os.MkdirAll(c.exportDir, 0o755); err != nil {
		return "", err
	}

	steadyStart, steadyEnd := r.SteadyWindow()
	art := Artifact{
		Run:         r,
		SteadyStart: steadyStart,
		SteadyEnd:   steadyEnd,
		Profiles:    map[string]metrics.Counts{},
	}
	for _, p := range r.Spec.Profiles {
		art.Profiles[p.Name] = metrics.EventLedger.Get(r.ID, p.Name, PhaseSteady)
	}
	art.Total = metrics.EventLedger.Total(r.ID, PhaseSteady)

	path := filepath.Join(c.exportDir, r.ID+".json")
	data, err := json.MarshalIndent(art, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// Artifact is the on-disk run record.
type Artifact struct {
	Run         *Run                      `json:"run"`
	SteadyStart time.Time                 `json:"steady_start"`
	SteadyEnd   time.Time                 `json:"steady_end"`
	Profiles    map[string]metrics.Counts `json:"profiles"`
	Total       metrics.Counts            `json:"total"`
}

// phaseOutcome distinguishes the three ways a timed phase can end. Stopping
// and aborting both cut the phase short, but only one of them leaves a
// measurable run behind, so they cannot share a boolean.
type phaseOutcome int

const (
	phaseElapsed phaseOutcome = iota // ran its full duration
	phaseStopped                     // Stop() — finish early, drain normally
	phaseAborted                     // context cancelled — tear down, void
)

func sleepPhase(ctx context.Context, stop <-chan struct{}, d time.Duration) phaseOutcome {
	if d <= 0 {
		if ctx.Err() != nil {
			return phaseAborted
		}
		select {
		case <-stop:
			return phaseStopped
		default:
			return phaseElapsed
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return phaseElapsed
	case <-stop:
		return phaseStopped
	case <-ctx.Done():
		return phaseAborted
	}
}
