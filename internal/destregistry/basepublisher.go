package destregistry

import (
	"sync"
	"sync/atomic"
)

// BasePublisher provides common publisher functionality
type BasePublisher struct {
	active sync.WaitGroup
	closed atomic.Bool
}

// StartPublish returns error if publisher is closed, otherwise adds to waitgroup
func (p *BasePublisher) StartPublish() error {
	if p.closed.Load() {
		return ErrPublisherClosed
	}
	p.active.Add(1)
	return nil
}

// FinishPublish marks a publish operation as complete
func (p *BasePublisher) FinishPublish() {
	p.active.Done()
}

// StartClose marks publisher as closed and waits for active operations
func (p *BasePublisher) StartClose() {
	p.closed.Store(true)
	p.active.Wait()
}
