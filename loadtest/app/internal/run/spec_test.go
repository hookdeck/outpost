package run

import (
	"strings"
	"testing"
)

const smokeSpec = `
name: smoke
target: direct
concurrency_budget: 1000
warmup: 30s
window: 5m
drain: 1m
profiles:
  - name: baseline
    tenants: 4
    destinations_per_tenant: 1
    rate_per_tenant: 25
    payload_bytes: 1024
    response_ms: 250
`

func TestParseSpec(t *testing.T) {
	s, err := ParseSpec([]byte(smokeSpec))
	if err != nil {
		t.Fatal(err)
	}
	if s.Window.Duration().Minutes() != 5 {
		t.Errorf("window = %s, want 5m", s.Window)
	}
	if got := s.Profiles[0].Rate(); got != 100 {
		t.Errorf("rate = %d, want 100", got)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("smoke spec should be valid: %v", err)
	}
}

// A typo'd key must fail loudly rather than silently taking a default — a spec
// that ran with the wrong payload size and said nothing is worse than one that
// refused to start.
func TestParseSpecRejectsUnknownFields(t *testing.T) {
	_, err := ParseSpec([]byte(strings.Replace(smokeSpec, "payload_bytes", "payload_size", 1)))
	if err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestBudgetArithmetic(t *testing.T) {
	// 100 events/s × 2 destinations × 5s response = 1000 concurrent deliveries.
	p := Profile{TenantCount: 10, RatePerTenant: 10, Destinations: 2, ResponseMs: 5000, PayloadBytes: 1024}
	if got := p.Concurrency(); got != 1000 {
		t.Errorf("concurrency = %.0f, want 1000", got)
	}
	// 100/s × 2 × 1KiB = 204800 B/s.
	if got := p.BytesPerSec(); got != 204800 {
		t.Errorf("bytes/s = %.0f, want 204800", got)
	}
}

// The budget check is the guard that stops a scenario set from silently
// measuring saturation instead of the dimension it sweeps.
func TestValidateRejectsOverBudget(t *testing.T) {
	s := &Spec{
		Name: "over", Target: "direct", Window: Duration(1e9), ConcurrencyBudget: 1000,
		Profiles: []Profile{
			{Name: "fanout", TenantCount: 10, RatePerTenant: 10, Destinations: 20, ResponseMs: 1000, PayloadBytes: 512},
		},
	}
	// 100/s × 20 × 1s = 2000 concurrent, double the budget.
	err := s.Validate()
	if err == nil {
		t.Fatal("expected over-budget spec to be rejected")
	}
	if !strings.Contains(err.Error(), "concurrency budget exceeded") {
		t.Errorf("error should name the budget: %v", err)
	}

	// Deliberate overcommit is allowed, but has to be stated.
	s.BudgetTolerance = 2.0
	if err := s.Validate(); err != nil {
		t.Errorf("explicit tolerance should permit the run: %v", err)
	}
}

// The full 4×4×4 factorial the report rejects: it is infeasible, not merely
// unreadable. This pins the arithmetic behind that claim.
func TestFullFactorialExceedsAnyReasonableBudget(t *testing.T) {
	var s Spec
	s.Name, s.Target, s.Window = "factorial", "direct", Duration(1e9)
	s.ConcurrencyBudget = 1000
	for _, payload := range []int{1024, 6144, 51200, 102400} {
		for _, fanout := range []int{1, 5, 10, 20} {
			for _, resp := range []int64{250, 1000, 5000, 10000} {
				s.Profiles = append(s.Profiles, Profile{
					TenantCount: 1, RatePerTenant: 16, Destinations: fanout,
					ResponseMs: resp, PayloadBytes: payload,
				})
			}
		}
	}
	if got := s.Budget().Concurrency; got < 30_000 {
		t.Fatalf("factorial concurrency = %.0f, expected tens of thousands", got)
	}
	if s.Budget().WithinBudget {
		t.Fatal("factorial should never fit a 1000-delivery budget")
	}
}

func TestValidateRequiresTargetPath(t *testing.T) {
	s := &Spec{Name: "x", Window: Duration(1e9), Profiles: []Profile{
		{Name: "p", TenantCount: 1, RatePerTenant: 1, Destinations: 1, PayloadBytes: 1},
	}}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "target must be") {
		t.Fatalf("target is what the report claims; it must be required: %v", err)
	}
}
