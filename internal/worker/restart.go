package worker

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	restartInitialBackoff = 1 * time.Second
	restartMaxBackoff     = 60 * time.Second
	restartJitter         = 0.2
	// A run that stays up this long ends the failure episode.
	restartStableWindow = 30 * time.Second
)

const (
	phaseStartup  = "startup"
	phaseRecovery = "recovery"
)

// RestartPolicy makes the supervisor restart a worker whose Run returns an
// error, instead of marking it failed for good.
//
// A worker is in the startup phase until a run has stayed up for the stable
// window (30s) once, counted from when the run calls Ready; after that it is
// in the recovery phase. Each phase has its own Limits. While within the
// limits the worker is degraded (/healthz 200); once either limit is hit it is
// failed (/healthz 503). The supervisor keeps restarting it either way, so it
// can come back to healthy.
//
// Backoff between runs: 1s doubling, capped at 60s, ±20% jitter; 60s once failed.
type RestartPolicy struct {
	Startup  Limits
	Recovery Limits
}

// Limits bounds a failure episode, which starts at the first failed run and
// ends when a run stays up for the stable window. Whichever limit is hit
// first escalates the worker to failed. A negative value disables a limit.
type Limits struct {
	// MaxAttempts is the number of restarts. The worker escalates when the
	// run after the last allowed restart fails (0: on the first failure).
	MaxAttempts int
	// MaxDuration is the time since the first failed run of the episode.
	MaxDuration time.Duration
}

func (l Limits) attemptsExceeded(failedRuns int) bool {
	return l.MaxAttempts >= 0 && failedRuns > l.MaxAttempts
}

func (l Limits) durationExceeded(elapsed time.Duration) bool {
	return l.MaxDuration >= 0 && elapsed >= l.MaxDuration
}

// RegisterOption configures how the supervisor runs a registered worker.
type RegisterOption func(*registration)

// WithRestartPolicy restarts the worker after a failed run. See RestartPolicy.
func WithRestartPolicy(p RestartPolicy) RegisterOption {
	return func(r *registration) {
		r.policy = &p
	}
}

type readyKey struct{}

// Ready tells the supervisor that the worker running with ctx is up, e.g. a
// consumer whose subscription is open. A worker with a RestartPolicy counts
// as stable only once it has been up for the stable window after calling
// Ready; one that never calls it is never stable. No-op outside a supervisor.
func Ready(ctx context.Context) {
	if ready, ok := ctx.Value(readyKey{}).(func()); ok {
		ready()
	}
}

type registration struct {
	worker Worker
	policy *RestartPolicy
}

// runOnce runs the worker, turning a panic into an error.
func (r *WorkerSupervisor) runOnce(ctx context.Context, w Worker) (err error) {
	defer func() {
		if p := recover(); p != nil {
			r.logger.Error("worker panicked",
				zap.String("worker", w.Name()),
				zap.Any("panic", p),
				zap.ByteString("stack", debug.Stack()))
			err = fmt.Errorf("worker panicked: %v", p)
		}
	}()
	return w.Run(ctx)
}

// superviseWithRestarts runs w until ctx is cancelled, restarting it after
// failed runs according to policy.
func (r *WorkerSupervisor) superviseWithRestarts(ctx context.Context, w Worker, policy RestartPolicy) {
	name := w.Name()
	phase := phaseStartup
	var (
		inEpisode bool
		since     time.Time
		attempts  int
		escalated bool
	)

	limits := func() Limits {
		if phase == phaseStartup {
			return policy.Startup
		}
		return policy.Recovery
	}
	reason := func() string {
		if phase == phaseStartup {
			return ReasonStartupFailed
		}
		return ReasonRecoveryFailed
	}
	escalate := func() {
		escalated = true
		r.health.MarkFailedWithReason(name, since, reason())
		r.recordStatus(ctx, name, WorkerStatusFailed)
		r.logger.Error("worker restart budget exhausted, marking failed",
			zap.String("worker", name),
			zap.String("phase", phase),
			zap.Int("attempts", attempts),
			zap.Duration("elapsed", time.Since(since)))
	}

	r.recordStatus(ctx, name, WorkerStatusHealthy)

	for {
		ready := make(chan struct{})
		var readyOnce sync.Once
		runCtx := context.WithValue(ctx, readyKey{}, func() { readyOnce.Do(func() { close(ready) }) })
		readyC := (<-chan struct{})(ready)

		errCh := make(chan error, 1)
		go func() { errCh <- r.runOnce(runCtx, w) }()

		var stable *time.Timer
		var stableC <-chan time.Time

		var err error
	wait:
		for {
			select {
			case err = <-errCh:
				break wait
			case <-readyC:
				readyC = nil
				stable = time.NewTimer(restartStableWindow)
				stableC = stable.C
			case <-stableC:
				stableC = nil
				if inEpisode {
					r.logger.Info("worker recovered",
						zap.String("worker", name),
						zap.String("phase", phase),
						zap.Int("attempts", attempts))
					r.health.MarkHealthy(name)
					r.recordStatus(ctx, name, WorkerStatusHealthy)
				}
				inEpisode, attempts, escalated = false, 0, false
				since = time.Time{}
				phase = phaseRecovery
			}
		}
		if stable != nil {
			stable.Stop()
		}

		if ctx.Err() != nil {
			r.logger.Info("worker stopped gracefully", zap.String("worker", name))
			return
		}
		if err == nil {
			err = errors.New("worker exited while the service is running")
		}

		if !inEpisode {
			inEpisode = true
			since = time.Now()
		}
		attempts++
		r.recordRunFailed(ctx, name, phase)

		if !escalated {
			if limits().attemptsExceeded(attempts) || limits().durationExceeded(time.Since(since)) {
				escalate()
			} else {
				r.health.MarkDegraded(name, since, reason())
				r.recordStatus(ctx, name, WorkerStatusDegraded)
			}
		}

		backoff := restartMaxBackoff
		if !escalated {
			backoff = restartBackoff(attempts)
		}
		backoff = r.jitter(backoff)

		r.logger.Warn("worker failed, restarting",
			zap.String("worker", name),
			zap.String("phase", phase),
			zap.Int("attempt", attempts),
			zap.Duration("backoff", backoff),
			zap.Error(err))

		if !r.sleepBackoff(ctx, backoff, escalated, limits(), since, escalate) {
			return
		}
	}
}

// sleepBackoff waits for backoff, escalating if the episode's MaxDuration
// passes meanwhile. Returns false if ctx was cancelled.
func (r *WorkerSupervisor) sleepBackoff(ctx context.Context, backoff time.Duration, escalated bool, limits Limits, since time.Time, escalate func()) bool {
	timer := time.NewTimer(backoff)
	defer timer.Stop()

	var deadlineC <-chan time.Time
	if !escalated && limits.MaxDuration >= 0 {
		deadline := time.NewTimer(time.Until(since.Add(limits.MaxDuration)))
		defer deadline.Stop()
		deadlineC = deadline.C
	}

	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadlineC:
			deadlineC = nil
			escalate()
		case <-timer.C:
			return true
		}
	}
}

func restartBackoff(attempt int) time.Duration {
	backoff := restartInitialBackoff
	for i := 1; i < attempt && backoff < restartMaxBackoff; i++ {
		backoff *= 2
	}
	return min(backoff, restartMaxBackoff)
}

func defaultJitter(d time.Duration) time.Duration {
	f := 1 + restartJitter*(2*rand.Float64()-1)
	return time.Duration(float64(d) * f)
}

func (r *WorkerSupervisor) recordRunFailed(ctx context.Context, name, phase string) {
	if r.metrics != nil {
		r.metrics.WorkerRunFailed(context.WithoutCancel(ctx), name, phase)
	}
}

func (r *WorkerSupervisor) recordStatus(ctx context.Context, name, status string) {
	if r.metrics != nil {
		r.metrics.WorkerStatus(context.WithoutCancel(ctx), name, status)
	}
}
