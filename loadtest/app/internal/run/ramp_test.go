package run

import (
	"strings"
	"testing"
)

// rampSpec ramps to its target inside a 60s warmup, so the measured window
// opens at full rate.
const rampSpec = `
name: ramped
target: direct
concurrency_budget: 1000
warmup: 60s
window: 5m
drain: 1m
profiles:
  - name: baseline
    tenants: 4
    destinations_per_tenant: 1
    rate_per_tenant: 25
    payload_bytes: 1024
    response_ms: 250
    pattern: ramp
    pattern_params:
      ramp_duration_seconds: 45
`

func TestRampSpecIsValid(t *testing.T) {
	s, err := ParseSpec([]byte(rampSpec))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("ramp inside warmup should be valid: %v", err)
	}
	if got := s.Profiles[0].RampSeconds(); got != 45 {
		t.Errorf("ramp = %ds, want 45", got)
	}
}

// A ramp still climbing when measurement starts means the window's opening
// samples are below the rate the run claims to have offered — the figures would
// report a rate the deployment was never asked to sustain.
func TestRampMustFinishInsideWarmup(t *testing.T) {
	err := mustParse(t, strings.Replace(rampSpec, "ramp_duration_seconds: 45", "ramp_duration_seconds: 90", 1)).Validate()
	if err == nil {
		t.Fatal("expected a ramp longer than warmup to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds warmup") {
		t.Errorf("error should name the warmup conflict, got: %v", err)
	}
}

// An unspecified ramp has to mean the same thing here as it does in the
// publisher, or validation passes a run the publisher then runs differently.
func TestRampDefaultMatchesPublisher(t *testing.T) {
	s := mustParse(t, strings.Replace(rampSpec, `
    pattern_params:
      ramp_duration_seconds: 45`, "", 1))
	if got := s.Profiles[0].RampSeconds(); got != 60 {
		t.Errorf("default ramp = %ds, want 60 to match newPattern", got)
	}
	// 60s default against a 60s warmup fits exactly.
	if err := s.Validate(); err != nil {
		t.Fatalf("default ramp equal to warmup should be valid: %v", err)
	}
}

// An unrecognised pattern falls back to constant inside the publisher, so
// without this check a typo produces a run that looks normal and silently did
// not do what was asked.
func TestUnknownPatternIsRejected(t *testing.T) {
	err := mustParse(t, strings.Replace(rampSpec, "pattern: ramp", "pattern: rmap", 1)).Validate()
	if err == nil {
		t.Fatal("expected unknown pattern to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown pattern") {
		t.Errorf("error should name the pattern, got: %v", err)
	}
}

func mustParse(t *testing.T, src string) *Spec {
	t.Helper()
	s, err := ParseSpec([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
