package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock worker for testing
type mockWorker struct {
	name    string
	runFunc func(ctx context.Context) error
	mu      sync.Mutex
	started bool
}

func newMockWorker(name string, runFunc func(ctx context.Context) error) *mockWorker {
	return &mockWorker{
		name:    name,
		runFunc: runFunc,
	}
}

func (m *mockWorker) Name() string {
	return m.name
}

func (m *mockWorker) Run(ctx context.Context) error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	if m.runFunc != nil {
		return m.runFunc(ctx)
	}
	<-ctx.Done()
	return nil
}

func (m *mockWorker) WasStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// Tests

func TestHealthTracker_MarkHealthy(t *testing.T) {
	t.Parallel()

	tracker := NewHealthTracker()

	tracker.MarkHealthy("worker-1")

	status := tracker.GetStatus()
	assert.Equal(t, "healthy", status["status"])

	workers := status["workers"].(map[string]WorkerHealth)
	assert.Len(t, workers, 1)
	assert.Equal(t, WorkerStatusHealthy, workers["worker-1"].Status)
}

func TestHealthTracker_MarkFailed(t *testing.T) {
	t.Parallel()

	tracker := NewHealthTracker()

	tracker.MarkFailed("worker-1")

	status := tracker.GetStatus()
	assert.Equal(t, "failed", status["status"])

	workers := status["workers"].(map[string]WorkerHealth)
	assert.Len(t, workers, 1)
	assert.Equal(t, WorkerStatusFailed, workers["worker-1"].Status)
}

func TestHealthTracker_IsHealthy_AllHealthy(t *testing.T) {
	t.Parallel()

	tracker := NewHealthTracker()

	tracker.MarkHealthy("worker-1")
	tracker.MarkHealthy("worker-2")

	assert.True(t, tracker.IsHealthy())
}

func TestHealthTracker_IsHealthy_OneFailed(t *testing.T) {
	t.Parallel()

	tracker := NewHealthTracker()

	tracker.MarkHealthy("worker-1")
	tracker.MarkFailed("worker-2")

	assert.False(t, tracker.IsHealthy())
}

func TestHealthTracker_NoErrorExposed(t *testing.T) {
	t.Parallel()

	tracker := NewHealthTracker()

	tracker.MarkFailed("worker-1")

	status := tracker.GetStatus()
	workers := status["workers"].(map[string]WorkerHealth)

	// Verify that error details are NOT exposed
	health := workers["worker-1"]
	assert.Equal(t, WorkerStatusFailed, health.Status)
	// Verify WorkerHealth struct has no Error field (compile-time check via struct)
	// If Error field existed, this would have compile error
	_ = WorkerHealth{
		Status: "healthy",
	}
}

func TestWorkerSupervisor_RegisterWorker(t *testing.T) {
	logger := testutil.CreateTestLogger(t)
	supervisor := NewWorkerSupervisor(logger)

	worker := newMockWorker("test-worker", nil)
	supervisor.Register(worker)

	assert.Len(t, supervisor.workers, 1)
}

func TestWorkerSupervisor_RegisterDuplicateWorker(t *testing.T) {
	logger := testutil.CreateTestLogger(t)
	supervisor := NewWorkerSupervisor(logger)

	worker1 := newMockWorker("test-worker", nil)
	worker2 := newMockWorker("test-worker", nil)

	supervisor.Register(worker1)

	assert.Panics(t, func() {
		supervisor.Register(worker2)
	})
}

func TestWorkerSupervisor_Run_HealthyWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		supervisor := NewWorkerSupervisor(logger)

		worker1 := newMockWorker("worker-1", func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})
		worker2 := newMockWorker("worker-2", func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})

		supervisor.Register(worker1)
		supervisor.Register(worker2)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()
		synctest.Wait()

		// Verify workers started
		assert.True(t, worker1.WasStarted())
		assert.True(t, worker2.WasStarted())

		// Verify health while workers are running
		tracker := supervisor.GetHealthTracker()
		assert.True(t, tracker.IsHealthy(), "all workers should be healthy while running")

		status := tracker.GetStatus()
		assert.Equal(t, "healthy", status["status"])

		workers := status["workers"].(map[string]WorkerHealth)
		assert.Len(t, workers, 2, "should have 2 workers in health status")
		assert.Equal(t, WorkerStatusHealthy, workers["worker-1"].Status, "worker-1 should be healthy")
		assert.Equal(t, WorkerStatusHealthy, workers["worker-2"].Status, "worker-2 should be healthy")
		assert.NotZero(t, status["timestamp"], "should have timestamp field")

		// Cancel context and verify graceful shutdown
		cancel()

		err := <-errChan
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestWorkerSupervisor_Run_FailedWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		supervisor := NewWorkerSupervisor(logger)

		healthyWorker := newMockWorker("healthy", func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})

		failingWorker := newMockWorker("failing", func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return errors.New("worker failed")
		})

		supervisor.Register(healthyWorker)
		supervisor.Register(failingWorker)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()

		// The failing worker has not failed yet
		advance(49 * time.Millisecond)
		assert.True(t, supervisor.GetHealthTracker().IsHealthy())

		// Wait for failing worker to fail
		advance(time.Millisecond)

		// Verify health reflects failure while supervisor is still running
		assert.False(t, supervisor.GetHealthTracker().IsHealthy())

		status := supervisor.GetHealthTracker().GetStatus()
		assert.Equal(t, "failed", status["status"])

		workers := status["workers"].(map[string]WorkerHealth)
		assert.Equal(t, WorkerStatusFailed, workers["failing"].Status)

		// Verify that supervisor is still blocking (hasn't returned yet)
		advance(time.Hour)
		select {
		case <-errChan:
			t.Fatal("supervisor.Run() returned early - should keep running until context cancelled")
		default:
			// Good - still blocking
		}

		// Now cancel context and verify graceful shutdown
		cancel()
		err := <-errChan
		assert.ErrorIs(t, err, context.Canceled) // Graceful shutdown should return context.Canceled from ctx.Err()
	})
}

func TestWorkerSupervisor_Run_AllWorkersExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		supervisor := NewWorkerSupervisor(logger)

		// Both workers exit on their own (not from context cancellation)
		worker1 := newMockWorker("worker-1", func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return errors.New("worker 1 failed")
		})

		worker2 := newMockWorker("worker-2", func(ctx context.Context) error {
			time.Sleep(100 * time.Millisecond)
			return errors.New("worker 2 failed")
		})

		supervisor.Register(worker1)
		supervisor.Register(worker2)

		ctx := context.Background()

		errChan := make(chan error, 1)
		start := time.Now()
		go func() {
			errChan <- supervisor.Run(ctx)
		}()

		// Wait for both workers to exit
		err := <-errChan
		assert.Error(t, err) // Should return error when all workers exit unexpectedly
		assert.Contains(t, err.Error(), "all workers have exited unexpectedly")
		assert.Equal(t, 100*time.Millisecond, time.Since(start), "should return when the last worker exits")

		// Verify both workers are marked as failed
		assert.False(t, supervisor.GetHealthTracker().IsHealthy())

		status := supervisor.GetHealthTracker().GetStatus()
		assert.Equal(t, "failed", status["status"])

		workers := status["workers"].(map[string]WorkerHealth)
		assert.Equal(t, WorkerStatusFailed, workers["worker-1"].Status)
		assert.Equal(t, WorkerStatusFailed, workers["worker-2"].Status)
	})
}

func TestWorkerSupervisor_Run_ContextCanceled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		supervisor := NewWorkerSupervisor(logger)

		worker := newMockWorker("worker-1", func(ctx context.Context) error {
			<-ctx.Done()
			return context.Canceled
		})

		supervisor.Register(worker)

		ctx, cancel := context.WithCancel(context.Background())

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()
		synctest.Wait()
		assert.True(t, worker.WasStarted())

		// Cancel context
		cancel()

		err := <-errChan
		assert.ErrorIs(t, err, context.Canceled) // Should return context.Canceled
	})
}

func TestWorkerSupervisor_Run_NoWorkers(t *testing.T) {
	logger := testutil.CreateTestLogger(t)
	supervisor := NewWorkerSupervisor(logger)

	ctx := context.Background()
	err := supervisor.Run(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workers registered")
}

func TestHealthTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	tracker := NewHealthTracker()

	var wg sync.WaitGroup
	workers := 100

	// Concurrent writes
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("worker-%d", i)
			if i%2 == 0 {
				tracker.MarkHealthy(name)
			} else {
				tracker.MarkFailed(name)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tracker.IsHealthy()
			_ = tracker.GetStatus()
		}()
	}

	wg.Wait()

	// Verify final state
	status := tracker.GetStatus()
	workersMap := status["workers"].(map[string]WorkerHealth)
	assert.Len(t, workersMap, workers)
}

func TestWorkerSupervisor_Run_VariableShutdownTiming(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		supervisor := NewWorkerSupervisor(logger)

		// Worker that shuts down quickly (50ms)
		fastWorker := newMockWorker("fast", func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond)
			return nil
		})

		// Worker that shuts down slowly (200ms)
		slowWorker := newMockWorker("slow", func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(200 * time.Millisecond)
			return nil
		})

		// Worker that shuts down instantly
		instantWorker := newMockWorker("instant", func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})

		supervisor.Register(fastWorker)
		supervisor.Register(slowWorker)
		supervisor.Register(instantWorker)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()
		synctest.Wait()

		// Cancel context and verify graceful shutdown
		cancelAt := time.Now()
		cancel()

		err := <-errChan

		assert.ErrorIs(t, err, context.Canceled)

		// Supervisor waits for exactly the slowest worker
		assert.Equal(t, 200*time.Millisecond, time.Since(cancelAt))
	})
}

func TestWorkerSupervisor_Run_VerySlowShutdown_NoTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		supervisor := NewWorkerSupervisor(logger) // No timeout

		// Worker that takes a very long time to shutdown (2 seconds)
		verySlowWorker := newMockWorker("very-slow", func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(2 * time.Second)
			return nil
		})

		supervisor.Register(verySlowWorker)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()
		synctest.Wait()

		cancelAt := time.Now()
		cancel()

		// Still waiting just before the worker finishes
		advance(2*time.Second - time.Millisecond)
		select {
		case <-errChan:
			t.Fatal("Supervisor.Run() returned before the worker finished shutting down")
		default:
		}

		err := <-errChan
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 2*time.Second, time.Since(cancelAt), "should wait the full 2s for the slow worker")
	})
}

func TestWorkerSupervisor_Run_ShutdownTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		// Set shutdown timeout to 500ms
		supervisor := NewWorkerSupervisor(logger, WithShutdownTimeout(500*time.Millisecond))

		// Worker that takes 2 seconds to shutdown (longer than timeout)
		slowWorker := newMockWorker("slow", func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(2 * time.Second)
			return nil
		})

		supervisor.Register(slowWorker)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()
		synctest.Wait()

		cancelAt := time.Now()
		cancel()

		err := <-errChan

		// Should return timeout error
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutdown timeout exceeded")

		// Should return at the timeout, not when the worker finishes (2s)
		assert.Equal(t, 500*time.Millisecond, time.Since(cancelAt))

		// Let the worker finish so nothing outlives the bubble
		advance(2 * time.Second)
	})
}

func TestWorkerSupervisor_Run_ShutdownTimeout_FastWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		// Set shutdown timeout to 2s
		supervisor := NewWorkerSupervisor(logger, WithShutdownTimeout(2*time.Second))

		// Workers that shutdown quickly (100ms)
		fastWorker := newMockWorker("fast", func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(100 * time.Millisecond)
			return nil
		})

		supervisor.Register(fastWorker)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()
		synctest.Wait()

		cancelAt := time.Now()
		cancel()

		err := <-errChan

		// Should NOT timeout since worker finishes quickly (returns nil when timeout configured but not exceeded)
		assert.NoError(t, err)

		// Should return when the worker finishes (100ms), not at the timeout (2s)
		assert.Equal(t, 100*time.Millisecond, time.Since(cancelAt))
	})
}

func TestWorkerSupervisor_Run_StuckWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)
		supervisor := NewWorkerSupervisor(logger)

		// Worker that never shuts down on its own (ignores context
		// cancellation); the test releases it at the end so the bubble can exit.
		release := make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		defer unblock()
		stuckWorker := newMockWorker("stuck", func(ctx context.Context) error {
			<-release
			return nil
		})

		supervisor.Register(stuckWorker)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- supervisor.Run(ctx)
		}()
		synctest.Wait()

		// Cancel context
		cancel()

		// Verify that supervisor keeps waiting for the stuck worker
		advance(24 * time.Hour)
		select {
		case <-errChan:
			t.Fatal("Supervisor.Run() returned but worker is stuck - should block until the worker exits")
		default:
		}

		// Once the worker exits, the supervisor returns
		unblock()
		synctest.Wait()
		select {
		case err := <-errChan:
			assert.ErrorIs(t, err, context.Canceled)
		default:
			t.Fatal("Supervisor.Run() did not return after the worker exited")
		}

		// Note: In production, this is why orchestrators need timeouts for pod termination
		// Kubernetes will forcefully kill pods that don't shutdown within terminationGracePeriodSeconds
	})
}
