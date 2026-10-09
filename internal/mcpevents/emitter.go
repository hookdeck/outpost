package mcpevents

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/opevents"
	"go.uber.org/zap"
)

// AsyncEmitter defaults.
const (
	DefaultEmitterQueueSize    = 4096
	DefaultEmitterWorkers      = 4
	DefaultEmitterTimeout      = 5 * time.Second
	DefaultEmitterDrainTimeout = 5 * time.Second
)

// ErrQueueFull is returned by AsyncEmitter.Emit when the event was dropped.
var ErrQueueFull = errors.New("mcpevents: queue full")

// EventEmitter is the operator-event sink AsyncEmitter wraps
// (opevents.Emitter satisfies it).
type EventEmitter interface {
	Emit(ctx context.Context, ev opevents.Event) error
}

// AsyncEmitterConfig configures an AsyncEmitter. Zero values take the
// defaults above.
type AsyncEmitterConfig struct {
	QueueSize    int
	Workers      int
	Timeout      time.Duration // per emit, retries included
	DrainTimeout time.Duration // on Close
	Logger       *logging.Logger
}

// AsyncEmitter moves operator events (mcp.subscription.expired,
// tenant.subscription.updated) off request paths and sweeper loops: Emit
// queues and returns, workers deliver with a per-event timeout, and a full
// queue drops and counts instead of blocking. It satisfies opevents.Emitter.
type AsyncEmitter struct {
	inner   EventEmitter
	timeout time.Duration
	logger  *zap.Logger
	q       *workQueue[opevents.Event]
	dropped atomic.Uint64
	failed  atomic.Uint64
}

var _ opevents.Emitter = (*AsyncEmitter)(nil)

// NewAsyncEmitter starts the workers delivering to inner.
func NewAsyncEmitter(inner EventEmitter, cfg AsyncEmitterConfig) *AsyncEmitter {
	e := &AsyncEmitter{
		inner:   inner,
		timeout: positiveOr(cfg.Timeout, DefaultEmitterTimeout),
		logger:  zapLogger(cfg.Logger),
	}
	e.q = newWorkQueue(
		positiveOr(cfg.QueueSize, DefaultEmitterQueueSize),
		positiveOr(cfg.Workers, DefaultEmitterWorkers),
		positiveOr(cfg.DrainTimeout, DefaultEmitterDrainTimeout),
		e.logger,
		e.emit,
	)
	return e
}

// Emit queues ev and returns at once; the ctx is not used for delivery, which
// runs detached under the emitter's own timeout. A dropped event is counted
// and returns ErrQueueFull, or ErrQueueClosed after Close.
func (e *AsyncEmitter) Emit(_ context.Context, ev opevents.Event) error {
	if !e.Enabled(ev.Topic) {
		return nil
	}
	if e.q.tryPush(ev) {
		return nil
	}
	total := e.dropped.Add(1)
	err := ErrQueueFull
	if e.q.isClosed() {
		err = ErrQueueClosed
	}
	e.logger.Warn("operator event dropped",
		zap.String("topic", ev.Topic),
		zap.String("tenant_id", ev.TenantID),
		zap.Error(err),
		zap.Uint64("dropped_total", total))
	return err
}

// Enabled delegates to the wrapped emitter when it filters topics.
func (e *AsyncEmitter) Enabled(topic string) bool {
	if f, ok := e.inner.(interface{ Enabled(string) bool }); ok {
		return f.Enabled(topic)
	}
	return true
}

// Dropped returns how many events were dropped (queue full or closed, or
// abandoned after the drain timeout).
func (e *AsyncEmitter) Dropped() uint64 {
	return e.dropped.Load() + e.q.dropped.Load()
}

// Failed returns how many events the wrapped emitter failed to deliver.
func (e *AsyncEmitter) Failed() uint64 {
	return e.failed.Load()
}

// Close stops intake and drains for up to the drain timeout. Safe to call
// more than once.
func (e *AsyncEmitter) Close() error {
	e.q.close()
	return nil
}

func (e *AsyncEmitter) emit(ctx context.Context, ev opevents.Event) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	if err := e.inner.Emit(ctx, ev); err != nil {
		e.failed.Add(1)
		e.logger.Warn("operator event not delivered",
			zap.String("topic", ev.Topic),
			zap.String("tenant_id", ev.TenantID),
			zap.Error(err))
	}
}
