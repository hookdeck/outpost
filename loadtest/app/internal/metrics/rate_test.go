package metrics

import (
	"testing"
	"time"
)

// Eviction by reslicing (`r.buckets = r.buckets[1:]`) shrinks cap by one, so the
// next append reallocates to a *larger* array. Capacity ratchets upward forever,
// and each reallocation copies the whole array while holding the mutex — which is
// what produced multi-second p99 excursions and ~0.30 GB/h of heap growth in the
// 24h run.
func TestRecordKeepsCapacityFixed(t *testing.T) {
	const bucketCount = 10
	r := NewRateCounter(10*time.Second, bucketCount)

	now := time.Now()
	// An hour at 1000 ev/s, compressed: 1 ms apart is well under any sane
	// bucket duration, so the great majority must land in an existing bucket.
	for i := 0; i < 200_000; i++ {
		r.Record(now)
		now = now.Add(time.Millisecond)
	}

	if got := len(r.buckets); got > bucketCount {
		t.Fatalf("len(buckets) = %d after 200k events, want <= %d", got, bucketCount)
	}
	if got := cap(r.buckets); got != bucketCount {
		t.Fatalf("cap(buckets) = %d after 200k events, want exactly %d — capacity ratcheted", got, bucketCount)
	}
}

// bucketDuration must come from the configured bucket count, not from the live
// capacity of the slice. Deriving it from cap() means a grown slice implies a
// shorter bucket, which implies more buckets, which grows the slice again.
func TestBucketDurationDoesNotDriftWithUse(t *testing.T) {
	r := NewRateCounter(10*time.Second, 10)

	now := time.Now()
	for i := 0; i < 100_000; i++ {
		r.Record(now)
		now = now.Add(time.Millisecond)
	}

	// After all that, 500 events spread over half a second — a fifth of one
	// 1s bucket — must not open more than one new bucket.
	before := countBuckets(r)
	for i := 0; i < 500; i++ {
		r.Record(now)
		now = now.Add(time.Millisecond)
	}
	if got := countBuckets(r) - before; got > 1 {
		t.Fatalf("500 events inside one bucket window opened %d new buckets, want <= 1 — bucket duration collapsed", got)
	}
}

// The reported rate is the whole point of the type; the fix must not change it.
func TestRateReportsOfferedRate(t *testing.T) {
	r := NewRateCounter(10*time.Second, 10)

	now := time.Now()
	// 30 s at exactly 1000 ev/s, so the trailing 10 s window is fully populated.
	for i := 0; i < 30_000; i++ {
		r.Record(now)
		now = now.Add(time.Millisecond)
	}

	// Tight bound on purpose. A full-window divisor over a 10-bucket ring reads
	// ~10% low, which is exactly the kind of quiet bias that makes a benchmark
	// under-report its own offered rate.
	got := r.Rate(now)
	if got < 970 || got > 1030 {
		t.Fatalf("Rate = %.1f/s after 30 s at 1000/s, want 1000 ±3%%", got)
	}
}

// A counter that goes quiet must decay to zero rather than reporting the rate it
// had when traffic stopped.
func TestRateDecaysWhenIdle(t *testing.T) {
	r := NewRateCounter(10*time.Second, 10)

	now := time.Now()
	for i := 0; i < 10_000; i++ {
		r.Record(now)
		now = now.Add(time.Millisecond)
	}
	if got := r.Rate(now.Add(time.Minute)); got != 0 {
		t.Fatalf("Rate = %.1f/s a minute after the last event, want 0", got)
	}
}

func TestResetClearsBuckets(t *testing.T) {
	r := NewRateCounter(10*time.Second, 10)

	now := time.Now()
	for i := 0; i < 10_000; i++ {
		r.Record(now)
		now = now.Add(time.Millisecond)
	}
	r.Reset()

	if got := r.Total(); got != 0 {
		t.Fatalf("Total after Reset = %d, want 0", got)
	}
	if got := r.Rate(now); got != 0 {
		t.Fatalf("Rate after Reset = %.1f/s, want 0", got)
	}
	if got := cap(r.buckets); got != 10 {
		t.Fatalf("cap(buckets) after Reset = %d, want 10", got)
	}
}

// Record is on the hot path of every publish and every delivery. It must not
// allocate, or the allocation is charged to the measurement.
func TestRecordDoesNotAllocate(t *testing.T) {
	r := NewRateCounter(10*time.Second, 10)
	now := time.Now()
	r.Record(now)

	allocs := testing.AllocsPerRun(10_000, func() {
		now = now.Add(time.Millisecond)
		r.Record(now)
	})
	if allocs != 0 {
		t.Fatalf("Record allocates %.1f objects per call, want 0", allocs)
	}
}

func countBuckets(r *RateCounter) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, b := range r.buckets {
		if b.count > 0 {
			n++
		}
	}
	return n
}
