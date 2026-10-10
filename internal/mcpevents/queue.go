package mcpevents

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ErrQueueClosed is returned when enqueueing after Close.
var ErrQueueClosed = errors.New("mcpevents: queue closed")

// workQueue is a bounded queue drained by a fixed pool of workers. Close
// stops intake, lets the workers drain for up to drainTimeout, then cancels
// the context handed to the handler and drops whatever is left.
type workQueue[T any] struct {
	queue        chan T
	closing      chan struct{}
	mu           sync.RWMutex // guards closed against sends on the closed queue
	closed       bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	closeOnce    sync.Once
	drainTimeout time.Duration
	dropped      atomic.Uint64 // items abandoned after the drain deadline
	logger       *zap.Logger
}

func newWorkQueue[T any](size, workers int, drainTimeout time.Duration, logger *zap.Logger, handle func(ctx context.Context, item T)) *workQueue[T] {
	ctx, cancel := context.WithCancel(context.Background())
	q := &workQueue[T]{
		queue:        make(chan T, size),
		closing:      make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		drainTimeout: drainTimeout,
		logger:       logger,
	}
	q.wg.Add(workers)
	for range workers {
		go func() {
			defer q.wg.Done()
			for item := range q.queue {
				if q.ctx.Err() != nil {
					q.dropped.Add(1) // past the drain deadline
					continue
				}
				q.run(handle, item)
			}
		}()
	}
	return q
}

func (q *workQueue[T]) run(handle func(context.Context, T), item T) {
	defer func() {
		if r := recover(); r != nil {
			q.logger.Error("mcpevents: queue handler panicked", zap.Any("panic", r))
		}
	}()
	handle(q.ctx, item)
}

// tryPush queues item without blocking; false means the queue is full or
// closed (the caller counts the drop).
func (q *workQueue[T]) tryPush(item T) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return false
	}
	select {
	case q.queue <- item:
		return true
	default:
		return false
	}
}

// isClosed reports whether close has started.
func (q *workQueue[T]) isClosed() bool {
	select {
	case <-q.closing:
		return true
	default:
		return false
	}
}

// push queues item, waiting for room until ctx is done or the queue closes.
func (q *workQueue[T]) push(ctx context.Context, item T) error {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return ErrQueueClosed
	}
	select {
	case q.queue <- item:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closing:
		return ErrQueueClosed
	}
}

func (q *workQueue[T]) close() {
	q.closeOnce.Do(func() {
		close(q.closing) // releases pushers blocked on a full queue
		q.mu.Lock()
		q.closed = true
		close(q.queue)
		q.mu.Unlock()

		done := make(chan struct{})
		go func() {
			q.wg.Wait()
			close(done)
		}()
		timer := time.NewTimer(q.drainTimeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			q.cancel() // abort in-flight work; workers drop the rest
			<-done
		}
		q.cancel()
	})
}
