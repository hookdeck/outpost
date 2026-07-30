package metrics

import (
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

// The old implementation kept only the last 10 000 values, so a percentile
// taken after a long run described a recency window rather than the run.
// Every value must count.
func TestHistogramCountsEveryValue(t *testing.T) {
	h := NewLatencyHistogram()
	for i := 0; i < 250_000; i++ {
		h.Record(10 * time.Millisecond)
	}
	if got := h.Count(); got != 250_000 {
		t.Fatalf("count = %d, want 250000", got)
	}
}

// A tail that arrives early must still be visible at the end of the run.
func TestHistogramRetainsEarlyTail(t *testing.T) {
	h := NewLatencyHistogram()
	for i := 0; i < 1000; i++ {
		h.Record(5 * time.Second) // early excursion
	}
	for i := 0; i < 99_000; i++ {
		h.Record(10 * time.Millisecond)
	}
	if p := h.PercentileMs(99.5); p < 4000 {
		t.Fatalf("p99.5 = %.1fms, want the 5s excursion to survive", p)
	}
	if max := h.Max(); max < 4900 {
		t.Fatalf("max = %dms, want ~5000", max)
	}
}

func TestHistogramPercentileAccuracy(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	h := NewLatencyHistogram()
	var raw []float64
	for i := 0; i < 200_000; i++ {
		// log-normal-ish latency: mostly fast, occasional slow
		ms := math.Exp(rng.NormFloat64()*0.9 + 3.0)
		raw = append(raw, ms)
		h.Record(time.Duration(ms * float64(time.Millisecond)))
	}
	sort.Float64s(raw)

	for _, p := range []float64{50, 90, 99, 99.9} {
		want := raw[int(p/100*float64(len(raw)))-1]
		got := h.PercentileMs(p)
		relErr := math.Abs(got-want) / want
		// Buckets are 2% wide, so a percentile lands within one bucket.
		if relErr > 0.03 {
			t.Errorf("p%.1f = %.2fms, want ~%.2fms (rel err %.3f)", p, got, want, relErr)
		}
	}
}

func TestHistogramSubMillisecond(t *testing.T) {
	h := NewLatencyHistogram()
	for i := 0; i < 1000; i++ {
		h.Record(300 * time.Microsecond)
	}
	p := h.PercentileMs(50)
	if p < 0.28 || p > 0.32 {
		t.Fatalf("p50 = %.4fms, want ~0.30ms", p)
	}
}

func TestHistogramEmpty(t *testing.T) {
	h := NewLatencyHistogram()
	if h.PercentileMs(99) != 0 || h.Max() != 0 || h.MeanMs() != 0 {
		t.Fatal("empty histogram should report zeros")
	}
}
