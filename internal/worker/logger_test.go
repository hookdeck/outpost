package worker_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/hookdeck/outpost/internal/worker"
)

// TestLoggingLoggerImplementsInterface verifies that *logging.Logger
// from internal/logging satisfies the worker.Logger interface.
func TestLoggingLoggerImplementsInterface(t *testing.T) {
	logger := testutil.CreateTestLogger(t)

	// This will fail to compile if *logging.Logger doesn't implement worker.Logger
	var _ worker.Logger = logger

	// Also verify we can actually use it with WorkerRegistry
	registry := worker.NewWorkerRegistry(logger)
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
}
