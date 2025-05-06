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

func (p *BasePublisher) MakeMetadata(event *models.Event, timestamp time.Time) map[string]string {
	systemMetadata := map[string]string{
		"timestamp": fmt.Sprintf("%d", timestamp.UnixMilli()),
		"event-id":  event.ID,
		"topic":     event.Topic,
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
