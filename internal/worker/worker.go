package worker

import "context"

// Worker represents a long-running background process.
// Each worker runs in its own goroutine and can be monitored for health.
//
// Workers should:
// - Block in Run() until context is cancelled or a fatal error occurs
// - Return nil or context.Canceled for graceful shutdown
// - Return non-nil error only for fatal failures
type Worker interface {
	// Name returns a unique identifier for this worker (e.g., "http-server", "retry-scheduler")
	Name() string

	// Run executes the worker and blocks until context is cancelled or error occurs.
	// Returns nil or context.Canceled for graceful shutdown.
	// Returns error for fatal failures.
	Run(ctx context.Context) error
}
