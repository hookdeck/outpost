package worker

import (
	"sync"
	"time"
)

const (
	WorkerStatusHealthy = "healthy"
	WorkerStatusFailed  = "failed"
)

// WorkerHealth represents the health status of a single worker.
// Error details are NOT exposed for security reasons.
type WorkerHealth struct {
	Status    string    `json:"status"` // "healthy" or "failed"
	LastCheck time.Time `json:"last_check"`
}

// HealthTracker tracks the health status of all workers.
// It is safe for concurrent use.
type HealthTracker struct {
	mu      sync.RWMutex
	workers map[string]WorkerHealth
}

// NewHealthTracker creates a new HealthTracker.
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		workers: make(map[string]WorkerHealth),
	}
}

// MarkHealthy marks a worker as healthy.
func (h *HealthTracker) MarkHealthy(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.workers[name] = WorkerHealth{
		Status:    WorkerStatusHealthy,
		LastCheck: time.Now(),
	}
}

// MarkFailed marks a worker as failed.
// Note: Error details are NOT stored for security reasons.
func (h *HealthTracker) MarkFailed(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.workers[name] = WorkerHealth{
		Status:    WorkerStatusFailed,
		LastCheck: time.Now(),
	}
}

// IsHealthy returns true if all workers are healthy.
func (h *HealthTracker) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, w := range h.workers {
		if w.Status != WorkerStatusHealthy {
			return false
		}
	}
	return true
}

// GetStatus returns the overall health status with details of all workers.
func (h *HealthTracker) GetStatus() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	workers := make(map[string]WorkerHealth)
	for name, w := range h.workers {
		workers[name] = w
	}

	status := "healthy"
	if !h.isHealthyLocked() {
		status = "failed"
	}

	return map[string]interface{}{
		"status":  status,
		"workers": workers,
	}
}

// isHealthyLocked checks health without acquiring lock (caller must hold read lock)
func (h *HealthTracker) isHealthyLocked() bool {
	for _, w := range h.workers {
		if w.Status != WorkerStatusHealthy {
			return false
		}
	}
	return true
}
