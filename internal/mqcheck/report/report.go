// Package report writes a run's results: report.md (verdict first) and JSON
// for tools.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/payload"
	"github.com/hookdeck/outpost/internal/mqcheck/profile"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqcheck/runner"
)

// Requirements are the short titles of R1-R17 (cmd/mqcheck/REQUIREMENTS.md).
var Requirements = map[int]string{
	1: "Count limit", 2: "Byte limit", 3: "Oversized message", 4: "Waiting doesn't count",
	5: "At a limit, stop taking", 6: "Unlimited under the limits", 7: "Full use of the limit",
	8: "Mixed sizes", 9: "Horizontal scale", 10: "At least once", 11: "No consumer duplicates",
	12: "Shutdown", 13: "Restart and outage", 14: "Idle", 15: "Errors", 16: "Off = today", 17: "Same meaning everywhere",
}

// Run describes a run.
type Run struct {
	RunID             string           `json:"run_id"`
	Provider          string           `json:"provider"`
	Broker            string           `json:"broker"`
	Profile           *profile.Profile `json:"profile"`
	Build             Build            `json:"build"`
	Host              string           `json:"host"`
	Started           time.Time        `json:"started"`
	Finished          time.Time        `json:"finished"`
	Prefix            string           `json:"resource_prefix"`
	VisibilityTimeout string           `json:"visibility_timeout"`
	MaxAttempts       int              `json:"max_attempts"`
	ByteLimitSetting  string           `json:"byte_limit_setting"`
	ByteLimitInBuild  bool             `json:"byte_limit_in_build"`
	Caps              provider.Caps    `json:"caps"`
	// Subset describes a run limited by --cases or --capacity-only.
	Subset string `json:"subset,omitempty"`
}

// Build identifies the code under test.
type Build struct {
	Commit    string `json:"commit"`
	Dirty     bool   `json:"dirty"`
	GoVersion string `json:"go_version"`
}

// Exit codes.
const (
	ExitPass      = 0 // every case and the capacity target judged, no FAIL
	ExitFail      = 1 // a FAIL
	ExitError     = 2 // harness or broker error, no FAIL
	ExitNotJudged = 3 // no FAIL, but something could not be judged here
)

// Verdict summarizes the run.
type Verdict struct {
	Counts   map[string]int `json:"counts"`
	Failing  []string       `json:"failing"`
	ExitCode int            `json:"exit_code"`
	Line     string         `json:"line"`
}

// Decide computes the verdict.
// subset, when not empty, says the run covered only some cases (--cases):
// such a run is never a PASS.
func Decide(results []runner.CaseResult, env *runner.Envelope, subset string) Verdict {
	v := Verdict{Counts: map[string]int{}}
	unjudgedChecks := 0
	for _, r := range results {
		v.Counts[r.Status]++
		if r.Status == runner.StatusByDesign {
			for _, c := range r.Checks {
				if c.Status == runner.StatusNotObservable {
					unjudgedChecks++
					break
				}
			}
		}
	}
	for _, q := range runner.Requirements(results) {
		st, notes := runner.RequirementStatus(results, q)
		if st == runner.StatusFail {
			v.Failing = append(v.Failing, fmt.Sprintf("R%d %s: %s", q, Requirements[q], strings.Join(notes, "; ")))
		}
	}
	var notJudged []string
	for _, st := range []string{runner.StatusPartial, runner.StatusNotObservable, runner.StatusNotInBuild, runner.StatusNA} {
		if n := v.Counts[st]; n > 0 {
			notJudged = append(notJudged, fmt.Sprintf("%d %s", n, st))
		}
	}
	if unjudgedChecks > 0 {
		notJudged = append(notJudged, fmt.Sprintf("%d BY-DESIGN with checks not observable", unjudgedChecks))
	}
	if env != nil && (env.Status == runner.StatusNotObservable || env.Status == runner.StatusMeasured) {
		notJudged = append(notJudged, "capacity "+env.Status)
	}
	if subset != "" {
		notJudged = append(notJudged, subset)
	}
	word := "PASS"
	switch {
	case v.Counts[runner.StatusFail] > 0 || (env != nil && env.Status == runner.StatusFail):
		v.ExitCode, word = ExitFail, "FAIL"
	case v.Counts[runner.StatusError] > 0 || (env != nil && env.Status == runner.StatusError):
		v.ExitCode, word = ExitError, "ERROR"
	case len(notJudged) > 0:
		v.ExitCode, word = ExitNotJudged, "NO FAIL, NOT FULLY JUDGED ("+strings.Join(notJudged, ", ")+")"
	}
	var parts []string
	for _, s := range []string{runner.StatusPass, runner.StatusPartial, runner.StatusFail, runner.StatusByDesign, runner.StatusNotObservable, runner.StatusNotInBuild, runner.StatusError, runner.StatusNA} {
		if n := v.Counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}
	v.Line = fmt.Sprintf("%s. Cases: %s.", word, strings.Join(parts, " · "))
	if env != nil {
		v.Line += " Capacity: " + envelopeHeadline(env) + "."
	}
	return v
}

func envelopeHeadline(env *runner.Envelope) string {
	switch env.Status {
	case runner.StatusSkipped, runner.StatusNotObservable, runner.StatusError:
		return env.Status + " (" + env.Reason + ")"
	}
	prefix := ""
	if env.Status == runner.StatusMeasured {
		prefix = "MEASURED, not judged (" + env.Reason + "): "
	}
	s := prefix + fmt.Sprintf("max sustained %d/s", env.MaxRate)
	if env.TargetRate > 0 {
		met := "met"
		if !env.TargetMet {
			met = "NOT met"
		}
		if env.Status == runner.StatusMeasured {
			met += ", not judged"
		}
		s += fmt.Sprintf(", target %d/s %s", env.TargetRate, met)
	}
	for _, d := range env.Drains {
		name := d.Payload
		if d.CappedTo != "" {
			name += "→" + d.CappedTo
		}
		s += fmt.Sprintf(", %s %.1f MB/s RSS %s", name, d.MBPerSec, payload.FormatBytes(d.RSSPeak))
	}
	return s
}

// Write writes report.md, run.json, results.json and envelope.json to dir.
func Write(dir string, run Run, results []runner.CaseResult, env *runner.Envelope) (Verdict, error) {
	v := Decide(results, env, run.Subset)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return v, err
	}
	files := map[string]any{"run.json": run, "results.json": results, "verdict.json": v}
	if env != nil {
		files["envelope.json"] = env
	}
	for name, obj := range files {
		b, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return v, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return v, err
		}
	}
	return v, os.WriteFile(filepath.Join(dir, "report.md"), []byte(markdown(run, results, env, v)), 0o644)
}

func markdown(run Run, results []runner.CaseResult, env *runner.Envelope, v Verdict) string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	p("# mqcheck: %s, profile %s", run.Provider, run.Profile.Name)
	p("")
	p("**Verdict: %s**", v.Line)
	p("")
	p("Broker: %s. Build: %s%s. Tier: %s. Visibility timeout %s, max attempts %d. Byte limit setting `%s`: %s.",
		run.Broker, run.Build.Commit, map[bool]string{true: " (dirty)"}[run.Build.Dirty], run.Profile.Tier,
		run.VisibilityTimeout, run.MaxAttempts, run.ByteLimitSetting, map[bool]string{true: "in this build", false: "not in this build"}[run.ByteLimitInBuild])
	p("")
	if len(v.Failing) > 0 {
		p("## Failing requirements")
		p("")
		for _, f := range v.Failing {
			p("- %s", f)
		}
		p("")
	}

	p("## Requirements")
	p("")
	p("| Req | | Status | Cases |")
	p("|---|---|---|---|")
	for _, q := range runner.Requirements(results) {
		st, _ := runner.RequirementStatus(results, q)
		var cs []string
		for _, r := range results {
			for _, rq := range r.Req {
				if rq == q {
					cs = append(cs, r.ID+" "+r.Status)
				}
			}
		}
		p("| R%d | %s | %s | %s |", q, Requirements[q], st, strings.Join(cs, ", "))
	}
	p("")

	p("## Cases")
	p("")
	for _, r := range results {
		p("### %s %s: %s", r.ID, r.Title, r.Status)
		p("")
		if r.Reason != "" {
			p("%s", r.Reason)
			p("")
		}
		if len(r.Checks) > 0 {
			p("| Check | R | Status | Want | Got |")
			p("|---|---|---|---|---|")
			for _, c := range r.Checks {
				got := c.Got
				if c.Reason != "" {
					got = strings.TrimSpace(got + " (" + c.Reason + ")")
				}
				name := c.Name
				if c.Pre {
					name += " (precondition)"
				}
				p("| %s | %d | %s | %s | %s |", name, c.Req, c.Status, esc(c.Want), esc(got))
			}
			p("")
		}
		if len(r.Metrics) > 0 {
			p("Metrics: %s", kv(r.Metrics))
			p("")
		}
		if len(r.Settings) > 0 {
			p("Settings: %s", kv(r.Settings))
			p("")
		}
		if r.Dir != "" {
			p("Raw: `%s`", r.Dir)
			p("")
		}
	}

	if env != nil {
		p("## Capacity envelope")
		p("")
		p("%s.", envelopeHeadline(env))
		p("")
		if len(env.Settings) > 0 {
			p("Valid for: %s. Targets: %s.", kv(env.Settings), env.LatencyTarget)
			p("")
		}
		if len(env.Steps) > 0 {
			p("| Rate/s | Published/s | Consumed/s | Backlog end | Queue p50 / p95 / p99 / max | E2E p50 / p99 | RSS peak | CPU cores | Redelivered | OK |")
			p("|---|---|---|---|---|---|---|---|---|---|")
			for _, s := range env.Steps {
				ok := "yes"
				if !s.OK {
					ok = "no: " + s.Why
				}
				p("| %d | %.0f | %.0f | %d | %s / %s / %s / %s | %s / %s | %s | %.2f | %d | %s |", s.Rate, s.PublishRate, s.ConsumeRate, s.BacklogEnd,
					s.QueueP50, s.QueueP95, s.QueueP99, s.QueueMax, s.E2EP50, s.E2EP99, payload.FormatBytes(s.RSSPeak), s.CPUCores, s.Redelivered, ok)
			}
			p("")
			if env.MaxRateNote != "" {
				p("%s.", env.MaxRateNote)
				p("")
			}
		}
		if len(env.Drains) > 0 {
			p("| Backlog drain | Count | Seconds | MB/s | Msg/s | Handled max | Handled bytes max | RSS peak | Redelivered |")
			p("|---|---|---|---|---|---|---|---|---|")
			for _, d := range env.Drains {
				name := d.Payload
				if d.CappedTo != "" {
					name += " (capped to " + d.CappedTo + ", the broker's max)"
				}
				if !d.Drained {
					name += fmt.Sprintf(" NOT DRAINED (%d/%d settled in 10 min)", d.Settled, d.Count)
				}
				p("| %s | %d | %.1f | %.1f | %.0f | %d | %s | %s | %d |", name, d.Count, d.Seconds, d.MBPerSec, d.MsgPerSec, d.HandledMax,
					payload.FormatBytes(d.HandledBytes), rssCell(d), d.Redelivered)
			}
			p("")
		}
	}

	p("## Provider")
	p("")
	p("- Broker: %s", run.Broker)
	p("- In-flight count: %s", run.Caps.InFlight)
	if run.Caps.MaxMessageBytes > 0 {
		p("- Max message size: %s", payload.FormatBytes(int64(run.Caps.MaxMessageBytes)))
	}
	for _, cl := range sortedKeys(run.Caps.Unobservable) {
		p("- Not observable here, %s: %s", cl, run.Caps.Unobservable[provider.Class(cl)])
	}
	for _, k := range sortedKeys(run.Caps.ByDesign) {
		p("- By design, %s: %s", k, run.Caps.ByDesign[k])
	}
	for _, n := range run.Caps.Notes {
		p("- %s", n)
	}
	p("")
	p("Run %s, %s → %s on %s. Resource prefix `%s`.", run.RunID, run.Started.Format(time.RFC3339), run.Finished.Format(time.RFC3339), run.Host, run.Prefix)
	return b.String()
}

func rssCell(d runner.Drain) string {
	if d.RSSBound > 0 {
		return payload.FormatBytes(d.RSSPeak) + " (bound " + payload.FormatBytes(d.RSSBound) + ")"
	}
	return payload.FormatBytes(d.RSSPeak)
}

func sortedKeys[K ~string, V any](m map[K]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

func kv(m map[string]any) string {
	keys := sortedKeys(m)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		val := m[k]
		if n, ok := val.(int64); ok && (strings.Contains(k, "rss") || strings.Contains(k, "bytes")) {
			val = payload.FormatBytes(n)
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, val))
	}
	return strings.Join(parts, ", ")
}

func esc(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
