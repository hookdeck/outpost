package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Logger is a minimal logging interface for structured logging with zap.
type Logger interface {
	Info(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
	Debug(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
}

// WorkerSupervisor manages and supervises multiple workers.
// It tracks their health and handles graceful shutdown.
type WorkerSupervisor struct {
	workers         map[string]Worker
	health          *HealthTracker
	logger          Logger
	shutdownTimeout time.Duration // 0 means no timeout
}

// SupervisorOption configures a WorkerSupervisor.
type SupervisorOption func(*WorkerSupervisor)

// WithShutdownTimeout sets the maximum time to wait for workers to shutdown gracefully.
// After this timeout, Run() will return even if workers haven't finished.
// Default is 0 (no timeout - wait indefinitely).
func WithShutdownTimeout(timeout time.Duration) SupervisorOption {
	return func(r *WorkerSupervisor) {
		r.shutdownTimeout = timeout
	}
}

// NewWorkerSupervisor creates a new WorkerSupervisor.
func NewWorkerSupervisor(logger Logger, opts ...SupervisorOption) *WorkerSupervisor {
	r := &WorkerSupervisor{
		workers:         make(map[string]Worker),
		health:          NewHealthTracker(),
		logger:          logger,
		shutdownTimeout: 0, // Default: no timeout
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Register adds a worker to the supervisor.
// Panics if a worker with the same name is already registered.
func (r *WorkerSupervisor) Register(w Worker) {
	if _, exists := r.workers[w.Name()]; exists {
		panic(fmt.Sprintf("worker %s already registered", w.Name()))
	}
	r.workers[w.Name()] = w
	r.logger.Debug("worker registered", zap.String("worker", w.Name()))
}

// GetHealthTracker returns the health tracker for this supervisor.
func (r *WorkerSupervisor) GetHealthTracker() *HealthTracker {
	return r.health
}

// Run starts all registered workers and supervises them.
// It blocks until:
// - ALL workers have exited (either successfully or with errors), OR
// - The context is cancelled (SIGTERM/SIGINT)
//
// When a worker fails, it marks the worker as failed but does NOT
// terminate other workers. This allows:
// - Other workers to continue serving (e.g., HTTP server stays up)
// - Health checks to report the failed worker status
// - Orchestrator to detect failure and restart the pod/container
//
// Returns nil if context was cancelled and workers shutdown gracefully.
// Returns error if workers failed to shutdown within timeout (if configured).
func (r *WorkerSupervisor) Run(ctx context.Context) error {
	if len(r.workers) == 0 {
		r.logger.Warn("no workers registered")
		return nil
	}

	r.logger.Info("starting workers", zap.Int("count", len(r.workers)))

	// WaitGroup to track worker goroutines
	var wg sync.WaitGroup

	// Start all workers
	for name, worker := range r.workers {
		wg.Add(1)
		go func(name string, w Worker) {
			defer wg.Done()

			r.logger.Info("worker starting", zap.String("worker", name))

			// Run the worker
			if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Error("worker failed",
					zap.String("worker", name),
					zap.Error(err))
				r.health.MarkFailed(name)
			} else {
				r.logger.Info("worker stopped gracefully", zap.String("worker", name))
				r.health.MarkHealthy(name)
			}
		}(name, worker)
	}

	// Wait for either:
	// - All workers to exit (wg.Wait completes)
	// - Context cancellation (graceful shutdown requested)
	select {
	case <-ctx.Done():
		r.logger.Info("context cancelled, shutting down workers")

		// Wait for all workers to finish gracefully, with optional timeout
		if r.shutdownTimeout > 0 {
			return r.waitWithTimeout(&wg, r.shutdownTimeout)
		}

		// No timeout - wait indefinitely
		wg.Wait()
		return nil
	case <-r.waitForWorkers(&wg):
		// All workers exited (either successfully or with errors)
		r.logger.Warn("all workers have exited")
		return nil
	}
}

// waitForWorkers converts WaitGroup.Wait() into a channel that can be used in select.
// Returns a channel that closes when all workers have exited.
func (r *WorkerSupervisor) waitForWorkers(wg *sync.WaitGroup) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

// waitWithTimeout waits for the WaitGroup with a timeout.
// Returns nil if all workers finish within timeout.
// Returns error if timeout is exceeded.
func (r *WorkerSupervisor) waitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) error {
	select {
	case <-r.waitForWorkers(wg):
		r.logger.Info("all workers shutdown gracefully")
		return nil
	case <-time.After(timeout):
		r.logger.Warn("shutdown timeout exceeded, some workers may still be running",
			zap.Duration("timeout", timeout))
		return fmt.Errorf("shutdown timeout exceeded (%v)", timeout)
	}
}
