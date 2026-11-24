package worker

import (
	"sync"
	"time"
)

const (
	WorkerStatusHealthy  = "healthy"
	WorkerStatusDegraded = "degraded"
	WorkerStatusFailed   = "failed"
)

const (
	ReasonStartupFailed  = "startup_failed"
	ReasonRecoveryFailed = "recovery_failed"
)

// WorkerHealth represents the health status of a single worker.
// Error details are NOT exposed for security reasons: /healthz is
// unauthenticated and broker errors carry hosts and credentials.
type WorkerHealth struct {
	Status string     `json:"status"`
	Since  *time.Time `json:"since,omitempty"`
	Reason string     `json:"reason,omitempty"`
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
	h.set(name, WorkerHealth{Status: WorkerStatusHealthy})
}

// MarkDegraded marks a worker as failing but still within its restart budget.
func (h *HealthTracker) MarkDegraded(name string, since time.Time, reason string) {
	h.set(name, WorkerHealth{Status: WorkerStatusDegraded, Since: &since, Reason: reason})
}

// MarkFailed marks a worker as failed.
// Note: Error details are NOT stored for security reasons.
func (h *HealthTracker) MarkFailed(name string) {
	h.set(name, WorkerHealth{Status: WorkerStatusFailed})
}

// MarkFailedWithReason marks a worker as failed after its restart budget was spent.
func (h *HealthTracker) MarkFailedWithReason(name string, since time.Time, reason string) {
	h.set(name, WorkerHealth{Status: WorkerStatusFailed, Since: &since, Reason: reason})
}

func (h *HealthTracker) set(name string, health WorkerHealth) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.workers[name] = health
}

// IsHealthy returns false if any worker has failed. Degraded workers still
// count as healthy: they are being restarted within their budget.
func (h *HealthTracker) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.statusLocked() != WorkerStatusFailed
}

// GetStatus returns the overall health status with details of all workers.
// The overall status is the worst worker status: failed > degraded > healthy.
func (h *HealthTracker) GetStatus() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	workers := make(map[string]WorkerHealth)
	for name, w := range h.workers {
		workers[name] = w
	}

	return map[string]interface{}{
		"status":    h.statusLocked(),
		"timestamp": time.Now(),
		"workers":   workers,
	}
}

// statusLocked returns the overall status (caller must hold read lock).
func (h *HealthTracker) statusLocked() string {
	status := WorkerStatusHealthy
	for _, w := range h.workers {
		switch w.Status {
		case WorkerStatusFailed:
			return WorkerStatusFailed
		case WorkerStatusDegraded:
			status = WorkerStatusDegraded
		}
	}
	return status
}
