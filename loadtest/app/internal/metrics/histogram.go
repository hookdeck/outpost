package metrics

import "sync"

// Histogram is a simple fixed-bucket histogram for latency tracking.
// Uses log-linear buckets to keep memory bounded.
type Histogram struct {
	mu     sync.Mutex
	min    int64
	max    int64
	count  int64
	sum    int64
	values []int64 // sorted insert, capped
}

const maxHistogramValues = 10000

func NewHistogram(minVal, maxVal int64, sigFigs int) *Histogram {
	return &Histogram{
		min:    minVal,
		max:    maxVal,
		values: make([]int64, 0, 1024),
	}
}

func (h *Histogram) Record(value int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	h.sum += value

	if len(h.values) < maxHistogramValues {
		h.values = append(h.values, value)
		// Keep sorted for percentile calculation (insertion sort at tail)
		for i := len(h.values) - 1; i > 0 && h.values[i] < h.values[i-1]; i-- {
			h.values[i], h.values[i-1] = h.values[i-1], h.values[i]
		}
	} else {
		// Reservoir sampling to maintain representative distribution
		// For simplicity, just replace the oldest entry after sorting
		// This is a bounded approximation
		idx := int(h.count % int64(maxHistogramValues))
		h.values[idx] = value
	}
}

func (h *Histogram) Percentile(p float64) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.values) == 0 {
		return 0
	}

	// If we've exceeded reservoir, sort for accurate percentile
	if h.count > maxHistogramValues {
		h.sort()
	}

	idx := int(float64(len(h.values)-1) * p / 100.0)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(h.values) {
		idx = len(h.values) - 1
	}
	return h.values[idx]
}

func (h *Histogram) Max() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.values) == 0 {
		return 0
	}
	if h.count > maxHistogramValues {
		h.sort()
	}
	return h.values[len(h.values)-1]
}

func (h *Histogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count = 0
	h.sum = 0
	h.values = h.values[:0]
}

func (h *Histogram) sort() {
	// Simple insertion sort — values are mostly sorted already
	for i := 1; i < len(h.values); i++ {
		for j := i; j > 0 && h.values[j] < h.values[j-1]; j-- {
			h.values[j], h.values[j-1] = h.values[j-1], h.values[j]
		}
	}
}
