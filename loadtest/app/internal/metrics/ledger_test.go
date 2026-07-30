package metrics

import "testing"

// The invariant the whole report rests on: published events are fully
// accounted for. A non-zero remainder is a harness bug and must be visible.
func TestLedgerBalances(t *testing.T) {
	l := NewLedger()
	e := Emit{RunID: "r1", Profile: "baseline", Phase: "steady"}

	for i := 0; i < 100; i++ {
		l.add(e, func(c *Counts) { c.Published++ })
	}
	for i := 0; i < 95; i++ {
		l.add(e, func(c *Counts) { c.Completed++ })
	}
	l.add(e, func(c *Counts) { c.Missing += 2 })
	l.add(e, func(c *Counts) { c.Cutoff += 3 })

	c := l.Get("r1", "baseline", "steady")
	if !c.Balanced() {
		t.Fatalf("ledger should balance: %s", c)
	}

	l.add(e, func(c *Counts) { c.Published++ })
	if c2 := l.Get("r1", "baseline", "steady"); c2.Balanced() {
		t.Fatal("an unaccounted event must show as a remainder")
	}
}

// Warmup events must not leak into the measured window.
func TestLedgerSeparatesPhases(t *testing.T) {
	l := NewLedger()
	l.add(Emit{"r1", "p", "warmup"}, func(c *Counts) { c.Published += 50 })
	l.add(Emit{"r1", "p", "steady"}, func(c *Counts) { c.Published += 10 })

	if got := l.Get("r1", "p", "steady").Published; got != 10 {
		t.Errorf("steady published = %d, want 10", got)
	}
	if got := l.Total("r1", "steady").Published; got != 10 {
		t.Errorf("steady total = %d, want 10", got)
	}
}

func TestLedgerTotalsAcrossProfiles(t *testing.T) {
	l := NewLedger()
	l.add(Emit{"r1", "a", "steady"}, func(c *Counts) { c.Published += 10; c.Completed += 10 })
	l.add(Emit{"r1", "b", "steady"}, func(c *Counts) { c.Published += 5; c.Completed += 5 })

	total := l.Total("r1", "steady")
	if total.Published != 15 || !total.Balanced() {
		t.Fatalf("total = %s", total)
	}
	if len(l.ByProfile("r1", "steady")) != 2 {
		t.Fatal("both profiles should be present")
	}
}
