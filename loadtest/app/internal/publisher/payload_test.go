package publisher

import "testing"

// The jitter is what makes the sweep a distribution rather than one size, so
// the spread has to actually appear in the bytes on the wire.
func TestPayloadJitterVariesSize(t *testing.T) {
	const target, jitter = 4096, 1024

	lo, hi := 1<<30, 0
	for i := range 500 {
		n := len(generatePayload("e", "t", target, jitter))
		lo, hi = min(lo, n), max(hi, n)
		_ = i
	}

	if lo == hi {
		t.Fatalf("every payload was %d bytes — jitter had no effect", lo)
	}
	// The generator hits the target within a dozen bytes of JSON framing, so
	// the observed range must sit inside the requested one with that slack.
	if lo < target-jitter-32 || hi > target+jitter+32 {
		t.Errorf("sizes %d..%d fall outside %d±%d", lo, hi, target, jitter)
	}
	// 500 draws over a 2049-wide range should cover most of it; a generator
	// that jittered by a constant would pass the bounds check above.
	if spread := hi - lo; spread < jitter {
		t.Errorf("spread %d over 500 draws is too narrow for ±%d", spread, jitter)
	}
}

// Symmetric jitter is what lets the spec keep reporting a single bandwidth
// number, so the mean has to stay on the target.
func TestPayloadJitterIsCenteredOnTarget(t *testing.T) {
	const target, jitter, n = 8192, 2048, 2000

	total := 0
	for range n {
		total += len(generatePayload("e", "t", target, jitter))
	}
	mean := total / n

	if mean < target-128 || mean > target+128 {
		t.Errorf("mean size %d drifted from target %d — bytes_per_sec would be wrong", mean, target)
	}
}

func TestPayloadWithoutJitterIsFixed(t *testing.T) {
	first := len(generatePayload("e", "t", 2048, 0))
	for range 50 {
		if n := len(generatePayload("e", "t", 2048, 0)); n != first {
			t.Fatalf("unjittered payload varied: %d then %d", first, n)
		}
	}
}
