package metrics

import (
	"math"
	"sync"
	"time"
)

// Histogram is a log-linear bucketed latency histogram.
//
// Buckets grow geometrically, so relative error is bounded at growthFactor-1
// regardless of magnitude, and memory is fixed no matter how many values are
// recorded. Every observation is counted — there is no sampling and no
// recency window, so a percentile taken at the end of a run describes the
// whole run.
//
// Two histograms over the same range can be merged by summing their buckets,
// which is what makes per-bucket percentiles recoverable after the fact.
type Histogram struct {
	mu      sync.Mutex
	buckets []uint64
	count   uint64
	sumUs   float64
	maxUs   float64
	minUs   float64 // lower bound of bucket 1; anything below lands in bucket 0
	growth  float64
	logBase float64
}

// NewHistogram builds a histogram covering [minVal, maxVal] milliseconds with
// the given relative error, e.g. relErr 0.02 for 2% bucket width.
func NewHistogram(minMs, maxMs float64, relErr float64) *Histogram {
	if minMs <= 0 {
		minMs = 0.01 // 10µs
	}
	if relErr <= 0 {
		relErr = 0.02
	}
	growth := 1 + relErr
	logBase := math.Log(growth)
	n := int(math.Ceil(math.Log(maxMs/minMs)/logBase)) + 2
	return &Histogram{
		buckets: make([]uint64, n),
		minUs:   minMs * 1000,
		growth:  growth,
		logBase: logBase,
	}
}

// NewLatencyHistogram is the standard shape: 10µs–10min at 2% relative error.
func NewLatencyHistogram() *Histogram {
	return NewHistogram(0.01, 600_000, 0.02)
}

func (h *Histogram) index(us float64) int {
	if us < h.minUs {
		return 0
	}
	i := int(math.Log(us/h.minUs)/h.logBase) + 1
	if i >= len(h.buckets) {
		return len(h.buckets) - 1
	}
	return i
}

// bucketUpper is the upper bound in microseconds of bucket i.
func (h *Histogram) bucketUpper(i int) float64 {
	if i == 0 {
		return h.minUs
	}
	return h.minUs * math.Pow(h.growth, float64(i))
}

func (h *Histogram) Record(d time.Duration) {
	us := float64(d.Microseconds())
	if us < 0 {
		us = 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buckets[h.index(us)]++
	h.count++
	h.sumUs += us
	if us > h.maxUs {
		h.maxUs = us
	}
}

// PercentileMs returns the p-th percentile in milliseconds.
func (h *Histogram) PercentileMs(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	target := uint64(math.Ceil(p / 100.0 * float64(h.count)))
	if target == 0 {
		target = 1
	}
	var cum uint64
	for i, c := range h.buckets {
		cum += c
		if cum >= target {
			return math.Min(h.bucketUpper(i), h.maxUs) / 1000
		}
	}
	return h.maxUs / 1000
}

// Percentile returns the p-th percentile rounded to whole milliseconds.
func (h *Histogram) Percentile(p float64) int64 {
	return int64(math.Round(h.PercentileMs(p)))
}

func (h *Histogram) Max() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(math.Round(h.maxUs / 1000))
}

func (h *Histogram) MeanMs() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.sumUs / float64(h.count) / 1000
}

func (h *Histogram) Count() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

func (h *Histogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.buckets {
		h.buckets[i] = 0
	}
	h.count = 0
	h.sumUs = 0
	h.maxUs = 0
}
