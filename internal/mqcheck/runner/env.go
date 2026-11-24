package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/payload"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqcheck/sut"
)

// Rec is a worker event tagged with its worker.
type Rec struct {
	W string
	sut.Event
}

// Msg is a published message.
type Msg struct {
	ID        string
	Size      int
	Directive string
	Published time.Time
}

// Env is one case's run: its queue, messages, workers and recordings.
type Env struct {
	R   *Runner
	C   Case
	Ctx context.Context
	Dir string
	// T is the queue's visibility timeout; MaxAttempts its dead-letter
	// threshold.
	T           time.Duration
	MaxAttempts int
	Target      *provider.Target

	cancel   context.CancelFunc
	gen      *payload.Generator
	genMu    sync.Mutex
	seq      int
	mu       sync.Mutex
	msgs     map[string]*Msg
	order    []string
	recs     []Rec
	samples  []provider.Sample
	workers  []*Worker
	dlq      map[string]bool
	dlqErr   error // last ReadDLQ error, nil after a good read
	dlqFails int   // ReadDLQ errors in a row
	checks   []CheckResult
	metrics  map[string]any
	settings map[string]any
	wg       sync.WaitGroup
}

func (r *Runner) newEnv(ctx context.Context, c Case) (*Env, error) {
	ctx, cancel := context.WithCancel(ctx)
	e := &Env{
		R: r, C: c, Ctx: ctx, cancel: cancel,
		Dir:         filepath.Join(r.OutDir, "cases", c.ID),
		T:           r.VisibilityTimeout(),
		MaxAttempts: r.MaxAttempts(),
		gen:         payload.NewGenerator(uint64(time.Now().UnixNano())),
		msgs:        map[string]*Msg{},
		dlq:         map[string]bool{},
		metrics:     map[string]any{},
		settings:    map[string]any{},
	}
	if err := mkdir(e.Dir); err != nil {
		cancel()
		return nil, err
	}
	name := provider.ResourceName(r.Prefix, r.RunID, provider.SanitizeName(c.ID))
	t, err := r.P.Provision(ctx, provider.QueueSpec{Name: name, VisibilityTimeout: e.T, MaxAttempts: e.MaxAttempts})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("provision %s: %w", name, err)
	}
	e.Target = t
	e.settings["queue"] = name
	e.settings["visibility_timeout"] = e.T.String()
	e.settings["max_attempts"] = e.MaxAttempts
	e.wg.Add(1)
	go e.sampleBroker()
	return e, nil
}

func (e *Env) close() {
	for _, w := range e.workers {
		w.Kill()
	}
	e.cancel()
	e.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.R.P.Teardown(ctx, e.Target); err != nil {
		e.R.logf("%-6s teardown: %v", e.C.ID, err)
	}
}

func (e *Env) sampleBroker() {
	defer e.wg.Done()
	every := e.R.Caps.SampleEvery
	if every <= 0 {
		every = time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-e.Ctx.Done():
			return
		case <-t.C:
			s, err := e.R.P.Sample(e.Ctx, e.Target)
			if err == nil {
				e.mu.Lock()
				e.samples = append(e.samples, s)
				e.mu.Unlock()
			}
		}
	}
}

// Setting records a case setting for the report.
func (e *Env) Setting(k string, v any) {
	e.mu.Lock()
	e.settings[k] = v
	e.mu.Unlock()
}

// Metric records a number for the report.
func (e *Env) Metric(k string, v any) {
	e.mu.Lock()
	e.metrics[k] = v
	e.mu.Unlock()
}

// Check records a judged condition. needs are the behavior classes it relies
// on; on a provider that cannot show one, the check is NOT-OBSERVABLE. A
// failing check listed in the provider's by-design table is BY-DESIGN.
func (e *Env) Check(name string, req int, pass bool, want, got string, needs ...provider.Class) {
	cr := CheckResult{Name: name, Req: req, Want: want, Got: got, Status: StatusPass}
	for _, cl := range needs {
		if why, ok := e.R.Caps.Unobservable[cl]; ok {
			cr.Status, cr.Reason = StatusNotObservable, why
			if !pass {
				cr.Reason += "; observed value would fail"
			}
			break
		}
	}
	if cr.Status == StatusPass && !pass {
		cr.Status = StatusFail
		if why, ok := e.R.Caps.ByDesign[e.C.ID+"/"+name]; ok {
			cr.Status, cr.Reason = StatusByDesign, why
		}
	}
	e.mu.Lock()
	e.checks = append(e.checks, cr)
	e.mu.Unlock()
}

// Precondition records a condition the scenario needs (e.g. the backlog
// drained). Failing it fails the case; passing it doesn't make the case pass.
func (e *Env) Precondition(name string, req int, ok bool, want, got string) {
	cr := CheckResult{Name: name, Req: req, Want: want, Got: got, Status: StatusPass, Pre: true}
	if !ok {
		cr.Status = StatusFail
	}
	e.mu.Lock()
	e.checks = append(e.checks, cr)
	e.mu.Unlock()
}

// Unobservable records a check that cannot be judged here, with why.
func (e *Env) Unobservable(name string, req int, want, why string) {
	e.mu.Lock()
	e.checks = append(e.checks, CheckResult{Name: name, Req: req, Want: want, Status: StatusNotObservable, Reason: why})
	e.mu.Unlock()
}

// Gen builds n message bodies of spec; directive(i) sets each one's handler
// directive (nil = default).
func (e *Env) Gen(n int, spec payload.Spec, directive func(i int) string) ([]*Msg, [][]byte) {
	e.genMu.Lock()
	defer e.genMu.Unlock()
	msgs := make([]*Msg, n)
	bodies := make([][]byte, n)
	for i := 0; i < n; i++ {
		e.seq++
		id := fmt.Sprintf("m%06d", e.seq)
		d := ""
		if directive != nil {
			d = directive(i)
		}
		bodies[i] = e.gen.Body(id, spec, d)
		msgs[i] = &Msg{ID: id, Size: len(bodies[i]), Directive: d}
	}
	return msgs, bodies
}

// Publish sends n messages of spec now and returns them.
func (e *Env) Publish(n int, spec payload.Spec, directive func(i int) string) ([]*Msg, error) {
	msgs, bodies := e.Gen(n, spec, directive)
	const chunk = 500
	for i := 0; i < n; i += chunk {
		j := min(n, i+chunk)
		now := time.Now()
		for k := i; k < j; k++ {
			payload.Stamp(bodies[k], now)
			msgs[k].Published = now
		}
		e.register(msgs[i:j])
		if err := e.R.P.Publish(e.Ctx, e.Target, bodies[i:j]); err != nil {
			return msgs, fmt.Errorf("publish: %w", err)
		}
	}
	return msgs, nil
}

func (e *Env) register(msgs []*Msg) {
	e.mu.Lock()
	for _, m := range msgs {
		e.msgs[m.ID] = m
		e.order = append(e.order, m.ID)
	}
	e.mu.Unlock()
}

// PublishRate publishes spec at rate messages/s for d and returns how many
// it published. Bodies are generated as they are due and stamped when sent.
func (e *Env) PublishRate(rate int, d time.Duration, spec payload.Spec) (int, error) {
	const tick = 10 * time.Millisecond
	total := int(float64(rate) * d.Seconds())
	sem := make(chan struct{}, 256)
	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		pubErr  error
	)
	start := time.Now()
	sent := 0
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for sent < total {
		select {
		case <-e.Ctx.Done():
			wg.Wait()
			return sent, e.Ctx.Err()
		case <-ticker.C:
		}
		due := min(total, int(float64(rate)*time.Since(start).Seconds()))
		for sent < due {
			batch := min(due-sent, 100)
			msgs, bodies := e.Gen(batch, spec, nil)
			now := time.Now()
			for k := range bodies {
				payload.Stamp(bodies[k], now)
				msgs[k].Published = now
			}
			e.register(msgs)
			sent += batch
			select {
			case sem <- struct{}{}:
			case <-e.Ctx.Done():
				wg.Wait()
				return sent, e.Ctx.Err()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if err := e.R.P.Publish(e.Ctx, e.Target, bodies); err != nil {
					errOnce.Do(func() { pubErr = err })
				}
			}()
		}
	}
	wg.Wait()
	return sent, pubErr
}

// WorkerOpts configures a worker process.
type WorkerOpts struct {
	// Count is the count limit (N); Bytes the byte limit (B), 0 = unset.
	Count int
	Bytes int64
	// Handler is the synthetic handler's default behavior.
	Handler sut.HandlerSpec
	// Target overrides the env's queue (e.g. a queue that doesn't exist).
	Target *provider.Target
	// Env is extra Outpost environment.
	Env map[string]string
}

// Worker is a running worker process.
type Worker struct {
	ID       string
	e        *Env
	cmd      *exec.Cmd
	ready    chan struct{}
	readyAt  time.Time
	done     chan struct{}
	exit     *sut.Event
	exitErr  error
	termAt   time.Time
	killed   bool
	mu       sync.Mutex
	readOnce sync.Once
}

// baseEnv is the process environment every worker inherits. Providers add
// what their clients read (Registration.PassEnv); everything else comes from
// the provider's WorkerEnv and the profile, so a shell's Outpost settings
// never leak into the consumer under test.
var baseEnv = []string{"PATH", "HOME", "TMPDIR", "USER", "LANG", "TZ"}

// StartWorker starts a worker process with the consumer under test.
func (e *Env) StartWorker(o WorkerOpts) (*Worker, error) {
	e.mu.Lock()
	id := fmt.Sprintf("w%d", len(e.workers)+1)
	e.mu.Unlock()
	target := o.Target
	if target == nil {
		target = e.Target
	}
	spec := sut.Spec{Provider: e.R.Reg.Name, Worker: id, Handler: o.Handler, VisibilityTimeout: e.T}
	specJSON, _ := json.Marshal(spec)

	env := map[string]string{}
	for _, k := range append(append([]string(nil), baseEnv...), e.R.Reg.PassEnv...) {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	for k, v := range e.R.Profile.Worker.Env {
		env[k] = v
	}
	for k, v := range e.R.P.WorkerEnv(target) {
		env[k] = v
	}
	env["LOG_LEVEL"] = e.R.Profile.Worker.LogLevel
	if o.Count > 0 {
		env[e.R.Profile.Worker.CountEnv] = strconv.Itoa(o.Count)
	}
	if o.Bytes > 0 {
		env[e.R.Profile.Worker.BytesEnv] = strconv.FormatInt(o.Bytes, 10)
	}
	for k, v := range o.Env {
		env[k] = v
	}
	env[sut.EnvSpec] = string(specJSON)

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	logf, err := os.Create(filepath.Join(e.Dir, id+".log"))
	if err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	evf, err := os.Create(filepath.Join(e.Dir, id+".events.jsonl"))
	if err != nil {
		pr.Close()
		pw.Close()
		logf.Close()
		return nil, err
	}
	cmd := exec.Command(e.R.Exe, "worker")
	cmd.Env = provider.EnvList(env)
	cmd.Dir = e.Dir
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.ExtraFiles = []*os.File{pw} // fd 3
	setParentDeathSignal(cmd)
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		logf.Close()
		evf.Close()
		return nil, err
	}
	pw.Close()
	w := &Worker{ID: id, e: e, cmd: cmd, ready: make(chan struct{}), done: make(chan struct{})}
	e.mu.Lock()
	e.workers = append(e.workers, w)
	e.mu.Unlock()
	bl := "unset"
	if o.Bytes > 0 {
		bl = payload.FormatBytes(o.Bytes)
	}
	e.Setting(id, fmt.Sprintf("N %d, B %s, handler %s±%s", o.Count, bl, o.Handler.Latency, o.Handler.Jitter))

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer evf.Close()
		w.read(io.TeeReader(pr, evf))
	}()
	go func() {
		<-readDone
		w.exitErr = cmd.Wait()
		logf.Close()
		w.readOnce.Do(func() { close(w.ready) })
		close(w.done)
	}()
	return w, nil
}

func (w *Worker) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var ev sut.Event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.K {
		case sut.KindReady:
			w.mu.Lock()
			if w.readyAt.IsZero() {
				w.readyAt = time.Unix(0, ev.T)
			}
			w.mu.Unlock()
			w.readOnce.Do(func() { close(w.ready) })
		case sut.KindExit:
			evc := ev
			w.mu.Lock()
			w.exit = &evc
			w.mu.Unlock()
		}
		w.e.mu.Lock()
		w.e.recs = append(w.e.recs, Rec{W: w.ID, Event: ev})
		w.e.mu.Unlock()
	}
}

// WaitReady waits until the worker subscribed, it exited, or timeout.
func (w *Worker) WaitReady(timeout time.Duration) error {
	select {
	case <-w.ready:
	case <-time.After(timeout):
		return fmt.Errorf("worker %s not ready after %s", w.ID, timeout)
	}
	if w.ReadyAt().IsZero() {
		return fmt.Errorf("worker %s exited before subscribing: %s", w.ID, w.ExitReason())
	}
	return nil
}

// ReadyAt is when the worker subscribed (zero if it never did).
func (w *Worker) ReadyAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.readyAt
}

// Term sends SIGTERM and returns when it was sent.
func (w *Worker) Term() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.termAt.IsZero() {
		w.termAt = time.Now()
		_ = w.cmd.Process.Signal(syscall.SIGTERM)
	}
	return w.termAt
}

// TermAt is when SIGTERM was sent (zero if never).
func (w *Worker) TermAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.termAt
}

// Kill stops the worker at once if it still runs.
func (w *Worker) Kill() {
	select {
	case <-w.done:
		return
	default:
	}
	w.mu.Lock()
	w.killed = true
	w.mu.Unlock()
	_ = w.cmd.Process.Kill()
	<-w.done
}

// Stop sends SIGTERM, waits up to grace for exit, then kills. It returns how
// long the worker took to exit and whether it exited on its own.
func (w *Worker) Stop(grace time.Duration) (time.Duration, bool) {
	t0 := w.Term()
	select {
	case <-w.done:
		return time.Since(t0), true
	case <-time.After(grace):
		w.Kill()
		return time.Since(t0), false
	}
}

// Wait waits for exit up to timeout and reports whether it exited.
func (w *Worker) Wait(timeout time.Duration) bool {
	select {
	case <-w.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ErrorLogs counts error-level lines in the worker's log so far.
func (w *Worker) ErrorLogs() int {
	b, err := os.ReadFile(filepath.Join(w.e.Dir, w.ID+".log"))
	if err != nil {
		return 0
	}
	return strings.Count(string(b), `"level":"error"`)
}

// Done is closed when the process has exited.
func (w *Worker) Done() <-chan struct{} { return w.done }

// Exit returns the worker's exit event (nil if it died without one).
func (w *Worker) Exit() *sut.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exit
}

// ExitCode is the process exit code, -1 while running or when killed.
func (w *Worker) ExitCode() int {
	select {
	case <-w.done:
	default:
		return -1
	}
	if w.cmd.ProcessState == nil {
		return -1
	}
	return w.cmd.ProcessState.ExitCode()
}

// ExitReason describes how the worker ended.
func (w *Worker) ExitReason() string {
	if ev := w.Exit(); ev != nil && ev.Err != "" {
		return ev.Err
	}
	if w.exitErr != nil {
		return w.exitErr.Error()
	}
	return "exited"
}

// Settled reports whether every published message is acked or dead-lettered.
func (e *Env) Settled() (settled, total int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	acked := map[string]bool{}
	for _, r := range e.recs {
		if r.K == sut.KindEnd && r.Outcome == sut.OutcomeAck {
			acked[r.ID] = true
		}
	}
	for id := range e.dlq {
		acked[id] = true
	}
	n := 0
	for _, id := range e.order {
		if acked[id] {
			n++
		}
	}
	return n, len(e.order)
}

// PollDLQ reads the dead-letter queue once and records what it found.
func (e *Env) PollDLQ() error {
	bodies, err := e.R.P.ReadDLQ(e.Ctx, e.Target)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil && !errors.Is(err, provider.ErrUnsupported) && e.Ctx.Err() == nil {
		if e.dlqErr == nil || e.dlqErr.Error() != err.Error() {
			e.R.logf("%-6s dlq: %v", e.C.ID, err)
		}
		e.dlqErr = err
		e.dlqFails++
		return err
	}
	if err != nil {
		return err
	}
	e.dlqErr, e.dlqFails = nil, 0
	for _, b := range bodies {
		var probe struct {
			Event struct {
				ID string `json:"id"`
			} `json:"event"`
		}
		if json.Unmarshal(b, &probe) == nil && probe.Event.ID != "" {
			e.dlq[probe.Event.ID] = true
		}
	}
	return nil
}

// DLQFailure returns the dead-letter read error when the last reads all
// failed: the case can't judge dead-lettering.
func (e *Env) DLQFailure() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dlqFails >= 3 {
		return e.dlqErr
	}
	return nil
}

// DLQ returns the ids found in the dead-letter queue.
func (e *Env) DLQ() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]bool, len(e.dlq))
	for k := range e.dlq {
		out[k] = true
	}
	return out
}

// WaitSettled waits until every published message is acked or
// dead-lettered, or timeout. With dlq it also polls the dead-letter queue.
func (e *Env) WaitSettled(timeout time.Duration, dlq bool) bool {
	deadline := time.Now().Add(timeout)
	lastDLQ := time.Time{}
	for {
		if dlq && time.Since(lastDLQ) > time.Second {
			_ = e.PollDLQ()
			lastDLQ = time.Now()
		}
		n, total := e.Settled()
		if n >= total {
			return true
		}
		if dlq && e.DLQFailure() != nil {
			return false // the case reports ERROR; waiting longer can't help
		}
		if time.Now().After(deadline) || e.Ctx.Err() != nil {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Sleep waits d or until the case is cancelled.
func (e *Env) Sleep(d time.Duration) {
	select {
	case <-time.After(d):
	case <-e.Ctx.Done():
	}
}

// Snapshot returns copies of the recordings so far.
func (e *Env) Snapshot() ([]Rec, []provider.Sample, map[string]*Msg) {
	e.mu.Lock()
	defer e.mu.Unlock()
	recs := append([]Rec(nil), e.recs...)
	samples := append([]provider.Sample(nil), e.samples...)
	msgs := make(map[string]*Msg, len(e.msgs))
	for k, v := range e.msgs {
		msgs[k] = v
	}
	return recs, samples, msgs
}

func (e *Env) writeSummary(res CaseResult) {
	_, samples, _ := e.Snapshot()
	if b, err := json.MarshalIndent(samples, "", " "); err == nil {
		_ = os.WriteFile(filepath.Join(e.Dir, "broker-samples.json"), b, 0o644)
	}
	if b, err := json.MarshalIndent(res, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(e.Dir, "result.json"), b, 0o644)
	}
}

// FmtDur renders d at a precision that suits its size.
func FmtDur(d time.Duration) string {
	switch {
	case d >= 10*time.Second:
		return d.Round(time.Second).String()
	case d >= time.Second:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Millisecond).String()
	}
}
