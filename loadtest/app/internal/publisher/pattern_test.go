package publisher

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The publisher stores the pattern's opening rate and sizes its limiters from
// it. A ramp that opened at the target would spend its first tick at full rate
// with a full token bucket — a burst of the entire per-tenant rate at once,
// which is the thing a ramp exists to avoid.
func TestRampOpensBelowTarget(t *testing.T) {
	p := rampPattern{durationSeconds: 60}
	if got := p.InitialRate(6000); got != 100 {
		t.Errorf("initial rate = %d, want 100 (one second of a 60s ramp to 6000)", got)
	}
}

// Integer division must not floor the opening rate to zero, which would stall
// the group instead of starting it slowly.
func TestRampOpeningRateIsNeverZero(t *testing.T) {
	p := rampPattern{durationSeconds: 60}
	if got := p.InitialRate(10); got != 1 {
		t.Errorf("initial rate = %d, want 1", got)
	}
}

// A zero or negative duration disables the ramp, and a disabled ramp must not
// silently hold the group at a fraction of its rate.
func TestRampDisabledStartsAtTarget(t *testing.T) {
	p := rampPattern{durationSeconds: 0}
	if got := p.InitialRate(500); got != 500 {
		t.Errorf("initial rate = %d, want 500", got)
	}
	var r atomic.Int64
	r.Store(500)
	p.Start(context.Background(), &r, 500) // returns immediately
	if got := r.Load(); got != 500 {
		t.Errorf("rate = %d, want 500 left untouched", got)
	}
}

// Patterns that oscillate around the base rate belong at the base rate at t=0.
func TestOscillatingPatternsOpenAtTarget(t *testing.T) {
	for name, p := range map[string]PatternController{
		"constant": constantPattern{},
		"burst":    burstPattern{},
		"sine":     sinePattern{},
	} {
		if got := p.InitialRate(750); got != 750 {
			t.Errorf("%s initial rate = %d, want 750", name, got)
		}
	}
}

// The ramp must actually reach its target and then stop adjusting, or the
// steady window would be driven by a controller still moving the rate.
func TestRampReachesTargetAndStops(t *testing.T) {
	var r atomic.Int64
	r.Store(0)
	done := make(chan struct{})
	go func() {
		rampPattern{durationSeconds: 2}.Start(context.Background(), &r, 100)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ramp did not finish")
	}
	if got := r.Load(); got != 100 {
		t.Errorf("final rate = %d, want 100", got)
	}
}
