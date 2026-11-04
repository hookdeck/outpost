package runner

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqcheck/sut"
)

func TestExtras(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	a := &Analysis{
		T:      10 * time.Second,
		TermAt: map[string]time.Time{"w2": at(30)},
		Killed: map[string]bool{},
		Msgs:   map[string]*Msg{"fast-dup": {ID: "fast-dup", Published: at(0)}, "held": {ID: "held", Published: at(-30)}},
		ByID: map[string][]Inv{
			"nacked":           {{W: "w1", Start: at(0), End: at(1), Outcome: sut.OutcomeNack}, {W: "w1", Start: at(1.1), End: at(2), Outcome: sut.OutcomeAck}},
			"slow":             {{W: "w1", Start: at(0), End: at(15), Outcome: sut.OutcomeAck}, {W: "w1", Start: at(10), End: at(11), Outcome: sut.OutcomeAck}},
			"held":             {{W: "w1", Start: at(6), End: at(12), Outcome: sut.OutcomeAck, Recv: at(0)}, {W: "w1", Start: at(10.2), End: at(16), Outcome: sut.OutcomeAck}},
			"shutdown":         {{W: "w2", Start: at(29), End: at(31), Outcome: sut.OutcomeAck}, {W: "w1", Start: at(40), End: at(41), Outcome: sut.OutcomeAck}},
			"broker":           {{W: "w1", Start: at(0), End: at(1), Outcome: sut.OutcomeAck}, {W: "w1", Start: at(20), End: at(21), Outcome: sut.OutcomeAck}},
			"late-att":         {{W: "w1", Start: at(5), End: at(6), Outcome: sut.OutcomeAck, Attempt: 3}},
			"dup-same-attempt": {{W: "w1", Start: at(0), End: at(1), Outcome: sut.OutcomeAck, Attempt: 1}, {W: "w1", Start: at(2), End: at(3), Outcome: sut.OutcomeAck, Attempt: 1}},
			"fast-dup":         {{W: "w1", Start: at(0.5), End: at(1), Outcome: sut.OutcomeAck}, {W: "w1", Start: at(2), End: at(3), Outcome: sut.OutcomeAck}},
			"once":             {{W: "w1", Start: at(0), End: at(1), Outcome: sut.OutcomeAck, Attempt: 1}},
		},
	}
	got := map[string]string{}
	for _, x := range a.Extras() {
		got[x.ID] = x.Class
	}
	want := map[string]string{
		"nacked": ExtraAfterNack, "slow": ExtraAfterTimeout, "held": ExtraHeldTooLong,
		"shutdown": ExtraAfterShutdown, "broker": ExtraBroker, "late-att": ExtraHeldTooLong,
		"dup-same-attempt": ExtraBroker, "fast-dup": ExtraBroker,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: got %q, want %q", id, got[id], w)
		}
	}
	if _, ok := got["once"]; ok {
		t.Error("a single first-attempt invocation counted as extra")
	}
}

func TestHiddenHeld(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	a := &Analysis{
		Invs: []Inv{{W: "w1", Start: at(0), End: at(100)}, {W: "w1", Start: at(0), End: at(100)}},
		Samples: map[string][]sut.Event{"w1": {
			{K: sut.KindSample, T: at(0).UnixNano(), VisHeld: 1},
		}},
	}
	// in flight: 2 handled + 1 visible + 4 hidden, with a one-sample spike to 9.
	for i, v := range []int64{7, 7, 7, 12, 7, 7, 7, 7} {
		a.Broker = append(a.Broker, provider.Sample{At: at(1 + 0.25*float64(i)), InFlight: v})
	}
	sus, peak, ok := a.HiddenHeld(time.Time{}, at(100), time.Second)
	if !ok || sus != 4 || peak != 9 {
		t.Errorf("HiddenHeld = %d, %d, %v; want 4, 9, true", sus, peak, ok)
	}
	a.Broker = []provider.Sample{{At: at(1), InFlight: -1}}
	if _, _, ok := a.HiddenHeld(time.Time{}, at(100), time.Second); ok {
		t.Error("HiddenHeld reported a value without an in-flight count")
	}
}

func TestCaseStatus(t *testing.T) {
	c := func(st string, pre bool) CheckResult { return CheckResult{Status: st, Pre: pre} }
	for _, tc := range []struct {
		checks []CheckResult
		want   string
	}{
		{[]CheckResult{c(StatusPass, false), c(StatusPass, true)}, StatusPass},
		{[]CheckResult{c(StatusNotObservable, false), c(StatusPass, true)}, StatusNotObservable},
		{[]CheckResult{c(StatusNotObservable, false), c(StatusPass, false)}, StatusPartial},
		{[]CheckResult{c(StatusByDesign, false), c(StatusPass, false)}, StatusByDesign},
		{[]CheckResult{c(StatusByDesign, false), c(StatusFail, true)}, StatusFail},
	} {
		if got := caseStatus(tc.checks); got != tc.want {
			t.Errorf("caseStatus(%v) = %s, want %s", tc.checks, got, tc.want)
		}
	}
}

func TestPercentile(t *testing.T) {
	var ds []time.Duration
	for i := 1; i <= 100; i++ {
		ds = append(ds, time.Duration(i)*time.Millisecond)
	}
	if got := Percentile(ds, 99); got != 99*time.Millisecond {
		t.Errorf("p99 = %s", got)
	}
	if got := Percentile(ds, 50); got != 50*time.Millisecond {
		t.Errorf("p50 = %s", got)
	}
}

func TestHasSetting(t *testing.T) {
	if !HasSetting("DELIVERY_MAX_CONCURRENCY") {
		t.Error("DELIVERY_MAX_CONCURRENCY not found in config")
	}
	if HasSetting("MQCHECK_NO_SUCH_SETTING") {
		t.Error("found a setting that does not exist")
	}
}
