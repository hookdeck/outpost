package worker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyWorker fails immediately while broken; otherwise it runs until ctx is
// cancelled or crash() is called.
type flakyWorker struct {
	name  string
	crash chan struct{}

	mu        sync.Mutex
	broken    bool
	panicNext bool
	runs      int
}

func newFlakyWorker(broken bool) *flakyWorker {
	return &flakyWorker{name: "flaky", crash: make(chan struct{}), broken: broken}
}

func (w *flakyWorker) Name() string { return w.name }

func (w *flakyWorker) Run(ctx context.Context) error {
	w.mu.Lock()
	w.runs++
	broken, panicNext := w.broken, w.panicNext
	w.panicNext = false
	w.mu.Unlock()

	if panicNext {
		panic("boom")
	}
	if broken {
		return errors.New("dial tcp 10.0.0.1:5672: connection refused")
	}
	Ready(ctx)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.crash:
		return errors.New("connection lost")
	}
}

func (w *flakyWorker) setBroken(b bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.broken = b
}

func (w *flakyWorker) Runs() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.runs
}

type supervisorHarness struct {
	t      *testing.T
	sup    *WorkerSupervisor
	cancel context.CancelFunc
	done   chan struct{}
}

// startSupervisor must be called inside a synctest bubble.
func startSupervisor(t *testing.T, w Worker, opts ...RegisterOption) *supervisorHarness {
	t.Helper()
	sup := NewWorkerSupervisor(testutil.CreateTestLogger(t))
	sup.jitter = func(d time.Duration) time.Duration { return d }
	sup.Register(w, opts...)

	ctx, cancel := context.WithCancel(context.Background())
	h := &supervisorHarness{t: t, sup: sup, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		sup.Run(ctx)
	}()
	t.Cleanup(h.stop)
	synctest.Wait()
	return h
}

func (h *supervisorHarness) stop() {
	h.cancel()
	<-h.done
}

func (h *supervisorHarness) health(name string) WorkerHealth {
	h.t.Helper()
	workers := h.sup.GetHealthTracker().GetStatus()["workers"].(map[string]WorkerHealth)
	return workers[name]
}

// advance moves the fake clock forward and waits for the bubble to settle.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestRestartPolicy_NoPolicyUnchanged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(true)
		h := startSupervisor(t, w)

		assert.Equal(t, WorkerHealth{Status: WorkerStatusFailed}, h.health("flaky"))
		advance(time.Hour)
		assert.Equal(t, 1, w.Runs())
		assert.False(t, h.sup.GetHealthTracker().IsHealthy())
	})
}

func TestRestartPolicy_DegradedOnFirstError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		w := newFlakyWorker(true)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: 120 * time.Second},
		}))

		got := h.health("flaky")
		assert.Equal(t, WorkerStatusDegraded, got.Status)
		assert.Equal(t, ReasonStartupFailed, got.Reason)
		require.NotNil(t, got.Since)
		assert.Equal(t, start, *got.Since)
		assert.True(t, h.sup.GetHealthTracker().IsHealthy())
	})
}

func TestRestartPolicy_StartupEscalatesAtMaxAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(true)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))

		// Runs at 0, 1, 3, 7, 15, 31s: the 5th restart fails at 31s.
		advance(30 * time.Second)
		assert.Equal(t, 5, w.Runs())
		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)

		advance(2 * time.Second)
		assert.Equal(t, 6, w.Runs())
		got := h.health("flaky")
		assert.Equal(t, WorkerStatusFailed, got.Status)
		assert.Equal(t, ReasonStartupFailed, got.Reason)
		assert.False(t, h.sup.GetHealthTracker().IsHealthy())
	})
}

func TestRestartPolicy_RecoveryEscalatesAtMaxDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(false)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 0, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: 120 * time.Second},
		}))

		advance(time.Minute)
		assert.Equal(t, WorkerStatusHealthy, h.health("flaky").Status)

		w.setBroken(true)
		w.crash <- struct{}{}
		synctest.Wait()
		failedAt := time.Now()

		got := h.health("flaky")
		assert.Equal(t, WorkerStatusDegraded, got.Status)
		assert.Equal(t, ReasonRecoveryFailed, got.Reason)
		assert.Equal(t, failedAt, *got.Since)

		advance(119 * time.Second)
		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)

		// Escalates at 120s, while waiting out the backoff.
		advance(time.Second)
		got = h.health("flaky")
		assert.Equal(t, WorkerStatusFailed, got.Status)
		assert.Equal(t, ReasonRecoveryFailed, got.Reason)
		assert.Equal(t, failedAt, *got.Since)
	})
}

func TestRestartPolicy_EitherLimitFirst(t *testing.T) {
	t.Run("attempts first", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			w := newFlakyWorker(true)
			h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
				Startup: Limits{MaxAttempts: 2, MaxDuration: 100 * time.Second},
			}))
			// Runs at 0, 1, 3s.
			advance(2 * time.Second)
			assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)
			advance(time.Second)
			assert.Equal(t, WorkerStatusFailed, h.health("flaky").Status)
		})
	})

	t.Run("duration first", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			w := newFlakyWorker(true)
			h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
				Startup: Limits{MaxAttempts: 100, MaxDuration: 5 * time.Second},
			}))
			advance(4*time.Second + 999*time.Millisecond)
			assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)
			advance(time.Millisecond)
			assert.Equal(t, WorkerStatusFailed, h.health("flaky").Status)
			assert.Equal(t, 3, w.Runs())
		})
	})
}

func TestRestartPolicy_NoLimitsNeverEscalates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(true)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: -1, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))

		advance(24 * time.Hour)
		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)
		assert.Greater(t, w.Runs(), 1000)
	})
}

func TestRestartPolicy_KeepsRestartingAfterEscalation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		w := newFlakyWorker(true)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 0, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))

		assert.Equal(t, WorkerStatusFailed, h.health("flaky").Status)
		assert.Equal(t, 1, w.Runs())

		// Once failed, restarts every 60s.
		advance(60 * time.Second)
		assert.Equal(t, 2, w.Runs())
		advance(60 * time.Second)
		assert.Equal(t, 3, w.Runs())
		got := h.health("flaky")
		assert.Equal(t, WorkerStatusFailed, got.Status)
		assert.Equal(t, start, *got.Since)

		// Fixed: the next run stays up; healthy after the stable window.
		w.setBroken(false)
		advance(60 * time.Second)
		assert.Equal(t, 4, w.Runs())
		assert.Equal(t, WorkerStatusFailed, h.health("flaky").Status)

		advance(30 * time.Second)
		assert.Equal(t, WorkerHealth{Status: WorkerStatusHealthy}, h.health("flaky"))
		assert.True(t, h.sup.GetHealthTracker().IsHealthy())
	})
}

func TestRestartPolicy_HealthyAfterStableWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(true)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: 120 * time.Second},
		}))
		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)

		w.setBroken(false)
		advance(time.Second) // restart, stays up
		assert.Equal(t, 2, w.Runs())
		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)

		advance(29 * time.Second)
		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)
		advance(time.Second)
		assert.Equal(t, WorkerHealth{Status: WorkerStatusHealthy}, h.health("flaky"))

		// The episode is over: the next failure starts a fresh one, in recovery,
		// with the backoff reset to 1s.
		w.crash <- struct{}{}
		synctest.Wait()
		got := h.health("flaky")
		assert.Equal(t, WorkerStatusDegraded, got.Status)
		assert.Equal(t, ReasonRecoveryFailed, got.Reason)
		assert.Equal(t, time.Now(), *got.Since)
		advance(time.Second)
		assert.Equal(t, 3, w.Runs())
	})
}

func TestRestartPolicy_RecoveryOnlyAfterFirstHealthy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(false)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 1, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))

		// Up for less than the stable window: still startup.
		advance(29 * time.Second)
		w.crash <- struct{}{}
		synctest.Wait()
		got := h.health("flaky")
		assert.Equal(t, WorkerStatusDegraded, got.Status)
		assert.Equal(t, ReasonStartupFailed, got.Reason)

		advance(time.Second) // restarted
		advance(10 * time.Second)
		w.crash <- struct{}{}
		synctest.Wait()
		got = h.health("flaky")
		assert.Equal(t, WorkerStatusFailed, got.Status)
		assert.Equal(t, ReasonStartupFailed, got.Reason)
	})
}

func TestRestartPolicy_PanicRecovered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(false)
		w.panicNext = true
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))

		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)
		advance(31 * time.Second)
		assert.Equal(t, 2, w.Runs())
		assert.Equal(t, WorkerStatusHealthy, h.health("flaky").Status)
	})
}

func TestRestartPolicy_CancelDuringBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(true)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))
		before := h.health("flaky")
		require.Equal(t, WorkerStatusDegraded, before.Status)

		start := time.Now()
		h.stop()
		assert.Equal(t, start, time.Now(), "Run should return without waiting out the backoff")
		assert.Equal(t, 1, w.Runs())
		assert.Equal(t, before, h.health("flaky"))
	})
}

func TestRestartPolicy_CancelDuringRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newFlakyWorker(false)
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 0, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))
		h.stop()
		assert.Equal(t, 1, w.Runs())
		assert.Equal(t, WorkerHealth{Status: WorkerStatusHealthy}, h.health("flaky"))
	})
}

// hangingWorker never becomes ready: each run blocks for hang (e.g. a dial
// to a host that drops packets) and then fails, or returns nil if exitNil.
type hangingWorker struct {
	hang    time.Duration
	exitNil bool
	mu      sync.Mutex
	runs    int
}

func (w *hangingWorker) Name() string { return "flaky" }

func (w *hangingWorker) Run(ctx context.Context) error {
	w.mu.Lock()
	w.runs++
	w.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(w.hang):
	}
	if w.exitNil {
		return nil
	}
	return errors.New("dial tcp 10.0.0.1:5672: i/o timeout")
}

func (w *hangingWorker) Runs() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.runs
}

func TestRestartPolicy_SlowFailureWithoutReadyEscalates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &hangingWorker{hang: 31 * time.Second}
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: 120 * time.Second},
		}))

		// Runs outlast the stable window but never signal ready, so the
		// episode never resets: 6 failed runs of 31s plus 31s of backoff.
		advance(31*time.Second + 1)
		got := h.health("flaky")
		assert.Equal(t, WorkerStatusDegraded, got.Status)
		assert.Equal(t, ReasonStartupFailed, got.Reason)

		for range 10 * 60 {
			advance(time.Second)
			assert.NotEqual(t, WorkerStatusHealthy, h.health("flaky").Status)
		}
		got = h.health("flaky")
		assert.Equal(t, WorkerStatusFailed, got.Status)
		assert.Equal(t, ReasonStartupFailed, got.Reason)
	})
}

func TestRestartPolicy_NilReturnWhileRunningIsFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &hangingWorker{hang: time.Second, exitNil: true}
		h := startSupervisor(t, w, WithRestartPolicy(RestartPolicy{
			Startup:  Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: Limits{MaxAttempts: -1, MaxDuration: -1},
		}))

		advance(time.Second)
		assert.Equal(t, WorkerStatusDegraded, h.health("flaky").Status)
		advance(time.Second)
		assert.Equal(t, 2, w.Runs())
	})
}

func TestRestartBackoff(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
	for i, w := range want {
		assert.Equal(t, w*time.Second, restartBackoff(i+1), "attempt %d", i+1)
	}
	for range 100 {
		d := defaultJitter(10 * time.Second)
		assert.GreaterOrEqual(t, d, 8*time.Second)
		assert.LessOrEqual(t, d, 12*time.Second)
	}
}

func TestHealthTracker_JSON(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)

	tracker := NewHealthTracker()
	tracker.MarkHealthy("http-server")
	tracker.MarkDegraded("publishmq-consumer", since, ReasonRecoveryFailed)

	status := tracker.GetStatus()
	assert.Equal(t, WorkerStatusDegraded, status["status"])
	assert.True(t, tracker.IsHealthy())

	b, err := json.Marshal(status["workers"])
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"http-server": {"status": "healthy"},
		"publishmq-consumer": {"status": "degraded", "since": "2026-09-30T08:00:00Z", "reason": "recovery_failed"}
	}`, string(b))

	tracker.MarkFailedWithReason("publishmq-consumer", since, ReasonStartupFailed)
	status = tracker.GetStatus()
	assert.Equal(t, WorkerStatusFailed, status["status"])
	assert.False(t, tracker.IsHealthy())

	b, err = json.Marshal(status["workers"])
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"http-server": {"status": "healthy"},
		"publishmq-consumer": {"status": "failed", "since": "2026-09-30T08:00:00Z", "reason": "startup_failed"}
	}`, string(b))
}
