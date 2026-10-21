package logmq

import (
	"errors"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
)

var errBatcherClosed = errors.New("logmq: batcher is shut down")

// batcher hands queued messages to process when itemCount is reached or on the
// next delay tick. process runs with mu held, so batches are serialized and add
// blocks while one is processed.
type batcher struct {
	process   func([]*mqs.Message)
	itemCount int
	delay     time.Duration

	mu     sync.Mutex
	items  []*mqs.Message
	ticker *time.Ticker
	closed bool

	done chan struct{}
	wg   sync.WaitGroup
}

func newBatcher(itemCount int, delay time.Duration, process func([]*mqs.Message)) (*batcher, error) {
	if process == nil {
		return nil, errors.New("logmq: batcher requires a processor")
	}
	if delay <= 0 {
		return nil, errors.New("logmq: batcher requires a positive delay")
	}
	b := &batcher{
		process:   process,
		itemCount: itemCount,
		delay:     delay,
		ticker:    time.NewTicker(delay),
		done:      make(chan struct{}),
	}
	b.wg.Add(1)
	go b.run()
	return b, nil
}

func (b *batcher) run() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		case <-b.ticker.C:
			b.mu.Lock()
			if !b.closed {
				b.flush()
			}
			b.mu.Unlock()
		}
	}
}

func (b *batcher) add(msg *mqs.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errBatcherClosed
	}
	b.items = append(b.items, msg)
	if b.itemCount > 0 && len(b.items) >= b.itemCount {
		b.flush()
	}
	return nil
}

// flush must be called with mu held.
func (b *batcher) flush() {
	if len(b.items) == 0 {
		return
	}
	b.ticker.Reset(b.delay)
	items := b.items
	b.items = nil
	b.process(items)
}

func (b *batcher) shutdown() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.done)
		b.ticker.Stop()
		b.flush()
	}
	b.mu.Unlock()
	b.wg.Wait()
}
