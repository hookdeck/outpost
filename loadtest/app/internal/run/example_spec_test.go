package run

import "testing"

// The example spec is the one spec in the repo, and `make bench` runs it by
// default on a fresh checkout. If it stops parsing or busts its own budget,
// the first thing a new contributor tries is a void run.
func TestExampleSpecIsValid(t *testing.T) {
	s, err := LoadSpec("../../runs/example.yaml")
	if err != nil {
		t.Fatalf("example.yaml: %v", err)
	}
	b := s.Budget()
	t.Logf("%s: %d events/s, %.0f concurrent deliveries, %.1f MB/s, %d profiles",
		s.Name, b.OfferedRate, b.Concurrency, b.BytesPerSec/1e6, len(s.Profiles))
	if err := s.Validate(); err != nil {
		t.Errorf("example.yaml: %v", err)
	}

	// It has to actually demonstrate a sweep, or it documents half the format.
	if len(s.Profiles) < 2 {
		t.Errorf("example has %d profile(s) — needs a baseline plus at least one sweep", len(s.Profiles))
	}
}

// Tolerance above 1.0 is deliberate overcommit and is covered in spec_test.go.
// Below 1.0 is the other direction: headroom, so the run stays out of the
// regime where queueing rather than the swept dimension sets the latency.
func TestBudgetToleranceReservesHeadroom(t *testing.T) {
	spec := func(tolerance float64) *Spec {
		return &Spec{
			Name: "t", Target: "direct", Window: Duration(1e9),
			ConcurrencyBudget: 100, BudgetTolerance: tolerance,
			// 360/s × 1 dest × 0.25s = 90 concurrent, just under the budget.
			Profiles: []Profile{{Name: "p", TenantCount: 1, RatePerTenant: 360,
				Destinations: 1, PayloadBytes: 1024, ResponseMs: 250}},
		}
	}
	if err := spec(0).Validate(); err != nil {
		t.Errorf("90 of 100 with no tolerance set should pass: %v", err)
	}
	if err := spec(0.8).Validate(); err == nil {
		t.Error("90 of 100 at tolerance 0.8 should be rejected — it measures queueing")
	}
}
