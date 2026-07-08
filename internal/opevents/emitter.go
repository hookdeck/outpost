package opevents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
)

const (
	maxRetries    = 3
	initialDelay  = 100 * time.Millisecond
	backoffFactor = 2
)

// Event is a request to emit an operator event. Callers (alert eval, apirouter)
// build it and hand it to Emit, which owns envelope construction and delivery.
type Event struct {
	Topic    string
	TenantID string
	Data     any
}

// Emitter is the interface for emitting operator events.
type Emitter interface {
	Emit(ctx context.Context, ev Event) error
}

// emitter is the default Emitter implementation.
type emitter struct {
	sink         Sink
	deploymentID string
	topicFilter  map[string]bool // nil means accept all ("*")
}

// NewEmitter creates an Emitter that filters by topics, builds the envelope, and
// delegates to the provided Sink. If topics contains "*", all topics are accepted.
// If topics is empty, a noop emitter is returned.
func NewEmitter(sink Sink, deploymentID string, topics []string) Emitter {
	if len(topics) == 0 {
		return &noopEmitter{}
	}

	var filter map[string]bool
	for _, t := range topics {
		if t == "*" {
			filter = nil
			break
		}
		if filter == nil {
			filter = make(map[string]bool, len(topics))
		}
		filter[t] = true
	}

	return &emitter{
		sink:         sink,
		deploymentID: deploymentID,
		topicFilter:  filter,
	}
}

func (e *emitter) Emit(ctx context.Context, ev Event) error {
	// Topic filtering: nil filter means accept all ("*")
	if e.topicFilter != nil && !e.topicFilter[ev.Topic] {
		return nil
	}

	rawData, err := json.Marshal(ev.Data)
	if err != nil {
		return fmt.Errorf("opevents: failed to marshal data: %w", err)
	}

	event := &OperatorEvent{
		ID:           idgen.String(),
		Topic:        ev.Topic,
		Time:         time.Now(),
		DeploymentID: e.deploymentID,
		TenantID:     ev.TenantID,
		Data:         rawData,
	}

	return e.sendWithRetry(ctx, event)
}

func (e *emitter) sendWithRetry(ctx context.Context, event *OperatorEvent) error {
	delay := initialDelay
	var lastErr error

	for attempt := range maxRetries {
		if err := e.sink.Send(ctx, event); err != nil {
			lastErr = err
			// Don't sleep after the last attempt
			if attempt < maxRetries-1 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				delay *= backoffFactor
			}
			continue
		}
		return nil
	}

	return fmt.Errorf("opevents: send failed after %d attempts: %w", maxRetries, lastErr)
}

// noopEmitter discards all events. Used when operator events are disabled.
type noopEmitter struct{}

func (e *noopEmitter) Emit(ctx context.Context, ev Event) error {
	return nil
}
