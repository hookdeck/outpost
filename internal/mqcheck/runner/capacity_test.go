package runner

import "testing"

func TestJudgeEnvelope(t *testing.T) {
	ok := Step{Rate: 1000, OK: true}
	bad := Step{Rate: 2000, Why: "consumed 1500/s"}
	for _, tc := range []struct {
		name      string
		env       Envelope
		minRate   int
		notJudged string
		want      string
	}{
		{"target met, bounded drain", Envelope{Steps: []Step{ok, bad}, TargetMet: true, Drains: []Drain{{Drained: true, RSSBound: 100, RSSPeak: 50}}}, 1000, "", StatusPass},
		{"target missed", Envelope{Steps: []Step{bad}}, 1000, "", StatusFail},
		{"drain unfinished", Envelope{Steps: []Step{ok}, TargetMet: true, Drains: []Drain{{Drained: false}}}, 1000, "", StatusFail},
		{"drain over bound", Envelope{Steps: []Step{ok}, TargetMet: true, Drains: []Drain{{Drained: true, RSSBound: 100, RSSPeak: 150}}}, 1000, "", StatusFail},
		{"drain without bound", Envelope{Steps: []Step{ok}, TargetMet: true, Drains: []Drain{{Drained: true}}}, 1000, "", StatusMeasured},
		{"no rate target, failed step", Envelope{Steps: []Step{bad}}, 0, "", StatusMeasured},
		{"generator short of target", Envelope{Steps: []Step{ok, {Rate: 1000, GeneratorCap: true, Why: "published only 800/s"}}}, 1000, "", StatusError},
		{"drain stalled on an emulated broker", Envelope{Drains: []Drain{{Drained: false}}}, 0, "emulated", StatusFail},
		{"emulated broker", Envelope{Steps: []Step{ok}, TargetMet: true}, 1000, "emulated", StatusMeasured},
	} {
		env := tc.env
		if got, _ := judgeEnvelope(&env, tc.minRate, tc.notJudged); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}
