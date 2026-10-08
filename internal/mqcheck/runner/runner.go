// Package runner is the harness driver: it provisions queues through a
// provider, publishes, runs worker processes with the consumer under test,
// records what they and the broker report, and judges each case.
package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mqcheck/payload"
	"github.com/hookdeck/outpost/internal/mqcheck/profile"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
)

// Statuses of a check, a case or a requirement.
const (
	StatusPass          = "PASS"
	StatusFail          = "FAIL"
	StatusByDesign      = "BY-DESIGN"
	StatusNotObservable = "NOT-OBSERVABLE"
	// StatusPartial: the checks that could be judged passed, others were not
	// observable on this broker.
	StatusPartial = "PARTIAL"
	// StatusMeasured: capacity numbers recorded but not judged against the
	// targets (the broker is an emulation).
	StatusMeasured   = "MEASURED"
	StatusNotInBuild = "NOT-IN-BUILD"
	StatusNA         = "N/A"
	StatusError      = "ERROR"
	StatusSkipped    = "SKIPPED"
)

// Case is one scenario, written once, run per provider.
type Case struct {
	ID    string
	Title string
	// Req lists the requirements the case checks (R numbers).
	Req []int
	// Tiers the case runs in: "quick", "official".
	Tiers []string
	// Needs are behavior classes every check in the case relies on.
	Needs []provider.Class
	// Bytes marks cases that need the byte limit setting in the build.
	Bytes bool
	// Run drives the scenario and records checks on e.
	Run func(e *Env) error
}

// CheckResult is one judged condition.
type CheckResult struct {
	Name   string `json:"name"`
	Req    int    `json:"req"`
	Status string `json:"status"`
	Want   string `json:"want"`
	Got    string `json:"got"`
	Reason string `json:"reason,omitempty"`
	// Pre marks a precondition (e.g. the backlog drained): it can fail a
	// case but never makes one pass.
	Pre bool `json:"pre,omitempty"`
}

// CaseResult is a case's outcome.
type CaseResult struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Req      []int          `json:"req"`
	Status   string         `json:"status"`
	Reason   string         `json:"reason,omitempty"`
	Checks   []CheckResult  `json:"checks"`
	Metrics  map[string]any `json:"metrics"`
	Seconds  float64        `json:"seconds"`
	Dir      string         `json:"dir"`
	Settings map[string]any `json:"settings"`
}

// Runner runs cases against one provider.
type Runner struct {
	Reg     provider.Registration
	P       provider.Provider
	Caps    provider.Caps
	Profile *profile.Profile
	Prefix  string
	RunID   string
	OutDir  string
	// Exe is the mqcheck binary started as worker processes.
	Exe string
	// Progress receives one line per case event.
	Progress io.Writer

	// HasBytes reports whether the build has the byte limit setting.
	HasBytes bool

	mu sync.Mutex
}

// New prepares a runner.
func New(reg provider.Registration, p provider.Provider, prof *profile.Profile, prefix, outDir, exe string, progress io.Writer) *Runner {
	r := &Runner{
		Reg: reg, P: p, Caps: p.Caps(), Profile: prof, Prefix: prefix, Exe: exe, Progress: progress,
		RunID: provider.RunName(time.Now()),
	}
	r.OutDir = filepath.Join(outDir, time.Now().UTC().Format("20060102-150405")+"-"+reg.Name+"-"+prof.Name)
	r.HasBytes = HasSetting(prof.Worker.BytesEnv)
	return r
}

// HasSetting reports whether Outpost's config has a field bound to env.
func HasSetting(env string) bool {
	if env == "" {
		return false
	}
	return hasEnvTag(reflect.TypeOf(config.Config{}), env, 0)
}

func hasEnvTag(t reflect.Type, env string, depth int) bool {
	if depth > 6 {
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tag, _, _ := strings.Cut(f.Tag.Get("env"), ","); tag == env {
			return true
		}
		if hasEnvTag(f.Type, env, depth+1) {
			return true
		}
	}
	return false
}

func (r *Runner) logf(format string, args ...any) {
	if r.Progress == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.Progress, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05")}, args...)...)
}

// VisibilityTimeout is the timeout test queues get.
func (r *Runner) VisibilityTimeout() time.Duration {
	return max(r.Profile.Queue.VisibilityTimeout.D(), r.Caps.MinVisibilityTimeout)
}

// MaxAttempts is the dead-letter threshold test queues get.
func (r *Runner) MaxAttempts() int {
	return max(r.Profile.Queue.MaxAttempts, r.Caps.MinMaxAttempts)
}

// Capped returns spec with its size capped at 90 % of the broker's max
// message size, and whether it was capped.
func (r *Runner) Capped(spec payload.Spec) (payload.Spec, bool) {
	if limit := r.Caps.MaxMessageBytes * 9 / 10; r.Caps.MaxMessageBytes > 0 && spec.Size > limit {
		spec.Size = limit
		return spec, true
	}
	return spec, false
}

// Select returns the cases the profile asks for, in order.
func (r *Runner) Select(all []Case, only []string) []Case {
	want := only
	if len(want) == 0 {
		want = r.Profile.Cases
	}
	var out []Case
	for _, c := range all {
		if !slices.Contains(c.Tiers, r.Profile.Tier) && !slices.Contains(want, c.ID) {
			continue
		}
		if slices.Contains(want, "all") || slices.Contains(want, c.ID) {
			out = append(out, c)
		}
	}
	return out
}

// RunCases runs cases, Parallel at a time, and returns results in case order.
func (r *Runner) RunCases(ctx context.Context, cases []Case) []CaseResult {
	results := make([]CaseResult, len(cases))
	par := max(1, r.Profile.Parallel)
	sem := make(chan struct{}, par)
	var wg sync.WaitGroup
	for i, c := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = r.runCase(ctx, c)
		}()
	}
	wg.Wait()
	return results
}

func (r *Runner) runCase(ctx context.Context, c Case) CaseResult {
	res := CaseResult{ID: c.ID, Title: c.Title, Req: c.Req, Metrics: map[string]any{}, Settings: map[string]any{}}
	for _, cl := range c.Needs {
		if why, ok := r.Caps.Unobservable[cl]; ok {
			res.Status, res.Reason = StatusNotObservable, why
			r.logf("%-6s %s (%s)", c.ID, res.Status, why)
			return res
		}
	}
	if c.Bytes && !r.HasBytes {
		res.Status = StatusNotInBuild
		res.Reason = fmt.Sprintf("this build has no %s setting", r.Profile.Worker.BytesEnv)
		r.logf("%-6s %s", c.ID, res.Status)
		return res
	}
	start := time.Now()
	r.logf("%-6s start: %s", c.ID, c.Title)
	e, err := r.newEnv(ctx, c)
	if err != nil {
		res.Status, res.Reason = StatusError, err.Error()
		r.logf("%-6s ERROR %v", c.ID, err)
		return res
	}
	runErr := func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		return c.Run(e)
	}()
	e.close()
	res.Dir = e.Dir
	res.Checks = e.checks
	res.Metrics = e.metrics
	res.Settings = e.settings
	res.Seconds = time.Since(start).Seconds()
	if runErr == nil {
		if err := e.DLQFailure(); err != nil {
			runErr = fmt.Errorf("dead-letter queue unreadable: %w", err)
		}
	}
	if runErr != nil {
		res.Status, res.Reason = StatusError, runErr.Error()
	} else {
		res.Status = caseStatus(e.checks)
	}
	e.writeSummary(res)
	r.logf("%-6s %s (%.0fs)", c.ID, res.Status, res.Seconds)
	return res
}

// caseStatus folds check statuses: any FAIL → FAIL, else BY-DESIGN; else
// PASS when every judged check passed, PARTIAL when some were not observable,
// NOT-OBSERVABLE when none could be judged. Passing preconditions don't count.
func caseStatus(checks []CheckResult) string {
	has := map[string]bool{}
	for _, c := range checks {
		if c.Pre && c.Status == StatusPass {
			continue
		}
		has[c.Status] = true
	}
	switch {
	case has[StatusFail]:
		return StatusFail
	case has[StatusByDesign]:
		return StatusByDesign
	case has[StatusPass] && has[StatusNotObservable]:
		return StatusPartial
	case has[StatusPass]:
		return StatusPass
	case has[StatusNotObservable]:
		return StatusNotObservable
	}
	return StatusNA
}

// RequirementStatus folds every check of requirement req, and the status of
// every case for req that has no checks (not observable, not in build, ...):
// FAIL, else ERROR, else BY-DESIGN; PASS only when everything was judged and
// passed, PARTIAL when something passed and something wasn't judged.
func RequirementStatus(results []CaseResult, req int) (string, []string) {
	var notes []string
	has := map[string]bool{}
	for _, cr := range results {
		if !slices.Contains(cr.Req, req) {
			continue
		}
		if len(cr.Checks) == 0 || cr.Status == StatusError {
			has[cr.Status] = true
			continue
		}
		for _, ch := range cr.Checks {
			if ch.Req != req || ch.Pre && ch.Status == StatusPass {
				continue
			}
			has[ch.Status] = true
			if ch.Status == StatusFail {
				notes = append(notes, cr.ID+" "+ch.Name+": "+ch.Got)
			}
		}
	}
	unjudged := ""
	for _, s := range []string{StatusNotObservable, StatusNotInBuild, StatusNA, StatusSkipped} {
		if has[s] {
			unjudged = s
			break
		}
	}
	switch {
	case has[StatusFail]:
		return StatusFail, notes
	case has[StatusError]:
		return StatusError, notes
	case has[StatusByDesign]:
		return StatusByDesign, notes
	case has[StatusPass] && unjudged != "":
		return StatusPartial, notes
	case has[StatusPass]:
		return StatusPass, notes
	}
	return unjudged, notes
}

// Requirements returns the requirement numbers covered by results, sorted.
func Requirements(results []CaseResult) []int {
	seen := map[int]bool{}
	for _, r := range results {
		for _, q := range r.Req {
			seen[q] = true
		}
	}
	var out []int
	for q := range seen {
		out = append(out, q)
	}
	sort.Ints(out)
	return out
}

func mkdir(dir string) error { return os.MkdirAll(dir, 0o755) }
