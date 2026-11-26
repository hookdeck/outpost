package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/hookdeck/outpost/internal/worker"
	"github.com/stretchr/testify/require"
)

var errQueueGone = errors.New("queue gone")

// fakeQueue is an in-memory queue that can break: while broken, Subscribe
// fails and every open subscription's Receive returns an error.
type fakeQueue struct {
	msgs chan *mqs.Message

	mu     sync.Mutex
	broken bool
	gone   chan struct{} // closed when the queue breaks
}

func newFakeQueue(broken bool) *fakeQueue {
	q := &fakeQueue{msgs: make(chan *mqs.Message, 100), gone: make(chan struct{})}
	if broken {
		q.setBroken(true)
	}
	return q
}

func (q *fakeQueue) setBroken(broken bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if broken == q.broken {
		return
	}
	q.broken = broken
	if broken {
		close(q.gone)
	} else {
		q.gone = make(chan struct{})
	}
}

func (q *fakeQueue) Subscribe(ctx context.Context, opts ...mqs.SubscribeOption) (mqs.Subscription, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.broken {
		return nil, errQueueGone
	}
	return &fakeSubscription{q: q, gone: q.gone}, nil
}

func (q *fakeQueue) publish(t *testing.T, id string) {
	t.Helper()
	body, err := json.Marshal(&testMsg{ID: id})
	require.NoError(t, err)
	q.msgs <- &mqs.Message{QueueMessage: noopSettle{}, Body: body}
}

type fakeSubscription struct {
	q    *fakeQueue
	gone chan struct{}
}

func (s *fakeSubscription) Receive(ctx context.Context) (*mqs.Message, error) {
	select {
	case <-s.gone:
		return nil, errQueueGone
	default:
	}
	select {
	case msg := <-s.q.msgs:
		return msg, nil
	case <-s.gone:
		return nil, errQueueGone
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *fakeSubscription) Shutdown(ctx context.Context) error { return nil }

type noopSettle struct{}

func (noopSettle) Ack()  {}
func (noopSettle) Nack() {}

// fakeHarness runs one publishmq consumer worker on a fakeQueue under the
// supervisor, wired as the builder wires it. Use inside a synctest bubble.
type fakeHarness struct {
	t          *testing.T
	q          *fakeQueue
	supervisor *worker.WorkerSupervisor
	received   chan string
}

func startFakeConsumer(t *testing.T, cfg *config.Config, q *fakeQueue) *fakeHarness {
	t.Helper()
	h := &fakeHarness{t: t, q: q, received: make(chan string, 100)}
	handler := handlerFunc(func(ctx context.Context, msg *mqs.Message) error {
		msg.Ack()
		parsed := &testMsg{}
		if err := parsed.FromMessage(msg); err != nil {
			return err
		}
		h.received <- parsed.ID
		return nil
	})

	logger := testutil.CreateTestLogger(t)
	h.supervisor = worker.NewWorkerSupervisor(logger)
	w, registerOpts := services.NewSupervisedConsumerWorker(cfg, config.SupervisorWorkerPublishMQ, publishMQWorkerName, q.Subscribe, handler, 1, logger)
	h.supervisor.Register(w, registerOpts...)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.supervisor.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	synctest.Wait()
	return h
}

func (h *fakeHarness) health() worker.WorkerHealth {
	return h.supervisor.GetHealthTracker().GetStatus()["workers"].(map[string]worker.WorkerHealth)[publishMQWorkerName]
}

// awaitStatus advances the fake clock until the worker reports status.
func (h *fakeHarness) awaitStatus(status string, within time.Duration) worker.WorkerHealth {
	h.t.Helper()
	deadline := time.Now().Add(within)
	for {
		synctest.Wait()
		if got := h.health(); got.Status == status {
			return got
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("worker never became %s within %s, last %+v", status, within, h.health())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *fakeHarness) awaitMessage(id string) {
	h.t.Helper()
	synctest.Wait()
	select {
	case got := <-h.received:
		require.Equal(h.t, id, got)
	default:
		h.t.Fatalf("message %s not delivered", id)
	}
}

// The queue fails under a running consumer: the worker is restarted with
// backoff while degraded, delivers again once the queue is back, and turns
// healthy after the stable window.
func TestPublishMQRestart_QueueFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := newFakeQueue(false)
		h := startFakeConsumer(t, supervisorConfig(config.SupervisorWorkerPublishMQ), q)
		h.awaitStatus(worker.WorkerStatusHealthy, time.Second)
		q.publish(t, "before")
		h.awaitMessage("before")

		q.setBroken(true)
		got := h.awaitStatus(worker.WorkerStatusDegraded, 10*time.Second)
		require.NotNil(t, got.Since)

		q.setBroken(false)
		q.publish(t, "after")
		// The next restart is at most a few backoff steps away.
		time.Sleep(10 * time.Second)
		h.awaitMessage("after")
		// Degraded until the run has stayed up for the 30s stable window.
		require.Equal(t, worker.WorkerStatusDegraded, h.health().Status)
		h.awaitStatus(worker.WorkerStatusHealthy, 35*time.Second)
	})
}

// The queue is broken from the start: the worker is degraded with
// startup_failed and fails after the 5 startup restarts at 1, 2, 4, 8, 16s
// (±20% jitter).
func TestPublishMQRestart_StartupFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		h := startFakeConsumer(t, supervisorConfig(config.SupervisorWorkerPublishMQ), newFakeQueue(true))

		got := h.awaitStatus(worker.WorkerStatusDegraded, time.Second)
		require.Equal(t, worker.ReasonStartupFailed, got.Reason)

		got = h.awaitStatus(worker.WorkerStatusFailed, 60*time.Second)
		elapsed := time.Since(start)
		require.GreaterOrEqual(t, elapsed, time.Duration(float64(31*time.Second)*0.8))
		require.LessOrEqual(t, elapsed, time.Duration(float64(31*time.Second)*1.2)+time.Second)
		require.Equal(t, worker.ReasonStartupFailed, got.Reason)
		require.False(t, h.supervisor.GetHealthTracker().IsHealthy())
	})
}

// Without the opt-in the worker fails on the first error, with no restart.
func TestPublishMQRestart_DisabledByDefault(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startFakeConsumer(t, supervisorConfig(), newFakeQueue(true))

		got := h.awaitStatus(worker.WorkerStatusFailed, time.Second)
		require.Equal(t, worker.WorkerHealth{Status: worker.WorkerStatusFailed}, got)
		require.False(t, h.supervisor.GetHealthTracker().IsHealthy())

		// Still failed well past the first backoff step: nothing restarts it.
		time.Sleep(5 * time.Second)
		require.Equal(t, worker.WorkerStatusFailed, h.awaitStatus(worker.WorkerStatusFailed, time.Second).Status)
	})
}
