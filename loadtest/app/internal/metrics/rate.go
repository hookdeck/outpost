package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// RateCounter tracks event rates over sliding windows.
type RateCounter struct {
	mu      sync.Mutex
	buckets []bucket
	window  time.Duration
	total   atomic.Int64
}

type bucket struct {
	count int64
	start time.Time
}

func NewRateCounter(window time.Duration, bucketCount int) *RateCounter {
	return &RateCounter{
		buckets: make([]bucket, 0, bucketCount),
		window:  window,
	}
}

func (r *RateCounter) Record(now time.Time) {
	r.total.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()

	bucketDuration := r.window / time.Duration(cap(r.buckets))
	if len(r.buckets) == 0 || now.Sub(r.buckets[len(r.buckets)-1].start) >= bucketDuration {
		if len(r.buckets) >= cap(r.buckets) {
			r.buckets = r.buckets[1:]
		}
		r.buckets = append(r.buckets, bucket{count: 1, start: now})
	} else {
		r.buckets[len(r.buckets)-1].count++
	}
}

func (r *RateCounter) Rate(now time.Time) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := now.Add(-r.window)
	var count int64
	for _, b := range r.buckets {
		if b.start.After(cutoff) {
			count += b.count
		}
	}
	return float64(count) / r.window.Seconds()
}

func (r *RateCounter) Total() int64 {
	return r.total.Load()
}

func (r *RateCounter) Reset() {
	r.total.Store(0)
	r.mu.Lock()
	r.buckets = r.buckets[:0]
	r.mu.Unlock()
}
