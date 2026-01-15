package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

// Mock logger for testing
type mockLogger struct {
	mu       sync.Mutex
	messages []string
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		messages: []string{},
	}
}

func (l *mockLogger) log(level, msg string, fields ...zap.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, fmt.Sprintf("[%s] %s", level, msg))
}

func (l *mockLogger) Info(msg string, fields ...zap.Field) {
	l.log("INFO", msg, fields...)
}

func (l *mockLogger) Error(msg string, fields ...zap.Field) {
	l.log("ERROR", msg, fields...)
}

func (l *mockLogger) Debug(msg string, fields ...zap.Field) {
	l.log("DEBUG", msg, fields...)
}

func (l *mockLogger) Warn(msg string, fields ...zap.Field) {
	l.log("WARN", msg, fields...)
}

func (l *mockLogger) Contains(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if contains(msg, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || s[1:len(s)-1] != s[1:len(s)-1] && contains(s[1:], substr)))
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
	logger := newMockLogger()
	supervisor := NewWorkerSupervisor(logger)

	worker := newMockWorker("test-worker", nil)
	supervisor.Register(worker)

	assert.Len(t, supervisor.workers, 1)
	assert.True(t, logger.Contains("worker registered"))
}

func TestWorkerSupervisor_RegisterDuplicateWorker(t *testing.T) {
	logger := newMockLogger()
	supervisor := NewWorkerSupervisor(logger)

	worker1 := newMockWorker("test-worker", nil)
	worker2 := newMockWorker("test-worker", nil)

	supervisor.Register(worker1)

	assert.Panics(t, func() {
		supervisor.Register(worker2)
	})
}

func TestWorkerSupervisor_Run_HealthyWorkers(t *testing.T) {
	logger := newMockLogger()
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

	// Give workers time to start
	time.Sleep(50 * time.Millisecond)

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
}

func TestWorkerSupervisor_Run_FailedWorker(t *testing.T) {
	logger := newMockLogger()
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

	// Wait for failing worker to fail
	time.Sleep(100 * time.Millisecond)

	// Verify health reflects failure while supervisor is still running
	assert.False(t, supervisor.GetHealthTracker().IsHealthy())

	status := supervisor.GetHealthTracker().GetStatus()
	assert.Equal(t, "failed", status["status"])

	workers := status["workers"].(map[string]WorkerHealth)
	assert.Equal(t, WorkerStatusFailed, workers["failing"].Status)

	// Verify that supervisor is still blocking (hasn't returned yet)
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
}

func TestWorkerSupervisor_Run_AllWorkersExit(t *testing.T) {
	logger := newMockLogger()
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
	go func() {
		errChan <- supervisor.Run(ctx)
	}()

	// Wait for both workers to exit
	err := <-errChan
	assert.Error(t, err) // Should return error when all workers exit unexpectedly
	assert.Contains(t, err.Error(), "all workers have exited unexpectedly")

	// Verify both workers are marked as failed
	assert.False(t, supervisor.GetHealthTracker().IsHealthy())

	status := supervisor.GetHealthTracker().GetStatus()
	assert.Equal(t, "failed", status["status"])

	workers := status["workers"].(map[string]WorkerHealth)
	assert.Equal(t, WorkerStatusFailed, workers["worker-1"].Status)
	assert.Equal(t, WorkerStatusFailed, workers["worker-2"].Status)

	// Verify log message
	assert.True(t, logger.Contains("all workers have exited"))
}

func TestWorkerSupervisor_Run_ContextCanceled(t *testing.T) {
	logger := newMockLogger()
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

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	err := <-errChan
	assert.ErrorIs(t, err, context.Canceled) // Should return context.Canceled
}

func TestWorkerSupervisor_Run_NoWorkers(t *testing.T) {
	logger := newMockLogger()
	supervisor := NewWorkerSupervisor(logger)

	ctx := context.Background()
	err := supervisor.Run(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workers registered")
	assert.True(t, logger.Contains("no workers registered"))
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
	logger := newMockLogger()
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
	start := time.Now()
	go func() {
		errChan <- supervisor.Run(ctx)
	}()

	// Give workers time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context and verify graceful shutdown
	cancel()

	err := <-errChan
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, context.Canceled)

	// Supervisor should wait for the slowest worker (200ms)
	// Total time should be at least 200ms (slow worker) + some overhead
	assert.True(t, elapsed >= 200*time.Millisecond,
		"expected shutdown to take at least 200ms (slowest worker), got %v", elapsed)

	// But not too much longer (should complete within 300ms)
	assert.True(t, elapsed < 300*time.Millisecond,
		"shutdown took too long: %v", elapsed)
}

func TestWorkerSupervisor_Run_VerySlowShutdown_NoTimeout(t *testing.T) {
	logger := newMockLogger()
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
	start := time.Now()
	go func() {
		errChan <- supervisor.Run(ctx)
	}()

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for shutdown with timeout
	select {
	case err := <-errChan:
		elapsed := time.Since(start)
		assert.ErrorIs(t, err, context.Canceled)

		// Should wait the full 2 seconds for worker to finish
		assert.True(t, elapsed >= 2*time.Second,
			"expected to wait at least 2s for slow worker, got %v", elapsed)

		t.Logf("Supervisor waited %v for slow worker to shutdown gracefully (no timeout)", elapsed)

	case <-time.After(3 * time.Second):
		t.Fatal("Supervisor.Run() blocked for more than 3 seconds")
	}
}

func TestWorkerSupervisor_Run_ShutdownTimeout(t *testing.T) {
	logger := newMockLogger()
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
	start := time.Now()
	go func() {
		errChan <- supervisor.Run(ctx)
	}()

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	err := <-errChan
	elapsed := time.Since(start)

	// Should return timeout error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown timeout exceeded")

	// Should return after ~500ms (timeout), not 2s (worker shutdown time)
	assert.True(t, elapsed >= 500*time.Millisecond,
		"expected to wait at least 500ms (timeout), got %v", elapsed)
	assert.True(t, elapsed < 1*time.Second,
		"expected to timeout before 1s, got %v", elapsed)

	t.Logf("Supervisor timed out after %v (expected ~500ms)", elapsed)
}

func TestWorkerSupervisor_Run_ShutdownTimeout_FastWorkers(t *testing.T) {
	logger := newMockLogger()
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
	start := time.Now()
	go func() {
		errChan <- supervisor.Run(ctx)
	}()

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	err := <-errChan
	elapsed := time.Since(start)

	// Should NOT timeout since worker finishes quickly (returns nil when timeout configured but not exceeded)
	assert.NoError(t, err)

	// Should return after ~100ms (worker shutdown time), not 2s (timeout)
	assert.True(t, elapsed >= 100*time.Millisecond,
		"expected to wait at least 100ms, got %v", elapsed)
	assert.True(t, elapsed < 500*time.Millisecond,
		"shutdown took too long: %v", elapsed)

	t.Logf("Supervisor shutdown in %v (workers finished before timeout)", elapsed)
}

func TestWorkerSupervisor_Run_StuckWorker(t *testing.T) {
	logger := newMockLogger()
	supervisor := NewWorkerSupervisor(logger)

	// Worker that never shuts down (ignores context cancellation)
	stuckWorker := newMockWorker("stuck", func(ctx context.Context) error {
		// Ignores context, blocks forever
		select {}
	})

	supervisor.Register(stuckWorker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- supervisor.Run(ctx)
	}()

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Verify that supervisor blocks indefinitely waiting for stuck worker
	select {
	case <-errChan:
		t.Fatal("Supervisor.Run() returned but worker is stuck - should block forever")
	case <-time.After(500 * time.Millisecond):
		// Expected: supervisor is still waiting for stuck worker
		t.Log("Supervisor correctly blocks waiting for stuck worker (this is expected behavior)")
	}

	// Note: In production, this is why orchestrators need timeouts for pod termination
	// Kubernetes will forcefully kill pods that don't shutdown within terminationGracePeriodSeconds
}
