package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// RateCounter tracks event rates over sliding windows.
//
// The buckets are a fixed-size ring, allocated once. Record must never grow or
// reallocate them: it runs on the hot path of every publish and every delivery,
// under a mutex those paths contend on, so a reallocation here shows up as a
// latency excursion in the measurement itself.
type RateCounter struct {
	mu       sync.Mutex
	buckets  []bucket // fixed length, indexed as a ring
	head     int      // index of the newest bucket; -1 when empty
	window   time.Duration
	bucketDu time.Duration
	total    atomic.Int64
}

type bucket struct {
	count int64
	start time.Time
}

func NewRateCounter(window time.Duration, bucketCount int) *RateCounter {
	if bucketCount < 1 {
		bucketCount = 1
	}
	return &RateCounter{
		buckets:  make([]bucket, bucketCount),
		head:     -1,
		window:   window,
		bucketDu: window / time.Duration(bucketCount),
	}
}

func (r *RateCounter) Record(now time.Time) {
	r.total.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.head >= 0 && now.Sub(r.buckets[r.head].start) < r.bucketDu {
		r.buckets[r.head].count++
		return
	}
	r.head = (r.head + 1) % len(r.buckets)
	r.buckets[r.head] = bucket{count: 1, start: now}
}

func (r *RateCounter) Rate(now time.Time) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := now.Add(-r.window)
	var count int64
	var oldest time.Time
	for _, b := range r.buckets {
		if b.count == 0 || !b.start.After(cutoff) {
			continue
		}
		count += b.count
		if oldest.IsZero() || b.start.Before(oldest) {
			oldest = b.start
		}
	}
	if count == 0 {
		return 0
	}
	// Divide by the span the counted buckets actually cover, not by the nominal
	// window. The newest bucket is mid-fill and the oldest has aged partway out,
	// so a full-window divisor reports one bucket low — 10% on a 10-bucket ring.
	span := now.Sub(oldest)
	if span < r.bucketDu {
		span = r.bucketDu
	} else if span > r.window {
		span = r.window
	}
	return float64(count) / span.Seconds()
}

func (r *RateCounter) Total() int64 {
	return r.total.Load()
}

func (r *RateCounter) Reset() {
	r.total.Store(0)
	r.mu.Lock()
	clear(r.buckets)
	r.head = -1
	r.mu.Unlock()
}
