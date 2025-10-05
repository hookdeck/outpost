package destregistry

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// BasePublisher provides common publisher functionality
type BasePublisher struct {
	active                      sync.WaitGroup
	closed                      atomic.Bool
	includeMillisecondTimestamp bool
}

// BasePublisherOption is a functional option for configuring BasePublisher
type BasePublisherOption func(*BasePublisher)

// WithMillisecondTimestamp enables millisecond-precision timestamp in metadata
func WithMillisecondTimestamp(enabled bool) BasePublisherOption {
	return func(p *BasePublisher) {
		p.includeMillisecondTimestamp = enabled
	}
}

// NewBasePublisher creates a new BasePublisher with the given options
func NewBasePublisher(opts ...BasePublisherOption) *BasePublisher {
	p := &BasePublisher{}
	for _, opt := range opts {
		opt(p)
	}
	return p
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

func (p *BasePublisher) MakeMetadata(event *models.Event, timestamp time.Time) map[string]string {
	systemMetadata := map[string]string{
		"timestamp": fmt.Sprintf("%d", timestamp.Unix()),
		"event-id":  event.ID,
		"topic":     event.Topic,
	}

	// Add millisecond timestamp if enabled
	if p.includeMillisecondTimestamp {
		systemMetadata["timestamp-ms"] = fmt.Sprintf("%d", timestamp.UnixMilli())
	}

	metadata := make(map[string]string)
	for k, v := range systemMetadata {
		metadata[k] = v
	}
	for k, v := range event.Metadata {
		metadata[k] = v
	}
	return metadata
}
