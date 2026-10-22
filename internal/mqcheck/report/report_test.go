package report_test

import (
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/mqcheck/report"
	"github.com/hookdeck/outpost/internal/mqcheck/runner"
)

func TestDecideNeverPassesUnjudged(t *testing.T) {
	pass := runner.CaseResult{ID: "C10.1", Req: []int{10}, Status: runner.StatusPass, Checks: []runner.CheckResult{{Req: 10, Status: runner.StatusPass}}}
	unobs := runner.CaseResult{ID: "C10.3", Req: []int{10}, Status: runner.StatusNotObservable}
	v := report.Decide([]runner.CaseResult{pass, unobs}, nil, "")
	if v.ExitCode != report.ExitNotJudged || !strings.HasPrefix(v.Line, "NO FAIL, NOT FULLY JUDGED") {
		t.Errorf("verdict %d %q, want not fully judged", v.ExitCode, v.Line)
	}
	if st, _ := runner.RequirementStatus([]runner.CaseResult{pass, unobs}, 10); st != runner.StatusPartial {
		t.Errorf("R10 = %s, want PARTIAL", st)
	}
	if v := report.Decide([]runner.CaseResult{pass}, nil, ""); v.ExitCode != report.ExitPass {
		t.Errorf("all judged and passed: exit %d", v.ExitCode)
	}
	if v := report.Decide([]runner.CaseResult{pass}, nil, "subset: 1 of 15 cases"); v.ExitCode != report.ExitNotJudged {
		t.Errorf("subset run: exit %d, want not fully judged", v.ExitCode)
	}
	fail := runner.CaseResult{ID: "C4.1", Req: []int{4}, Status: runner.StatusFail, Checks: []runner.CheckResult{{Req: 4, Status: runner.StatusFail}}}
	if v := report.Decide([]runner.CaseResult{pass, unobs, fail}, nil, ""); v.ExitCode != report.ExitFail {
		t.Errorf("with a FAIL: exit %d", v.ExitCode)
	}
}
