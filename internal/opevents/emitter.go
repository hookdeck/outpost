package opevents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
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
	// LogFields is caller context attached to the delivery audit line —
	// typically set by the payload constructors, not at emit call sites.
	LogFields []zap.Field
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
	logger       *logging.Logger
}

// NewEmitter creates an Emitter that filters by topics, builds the envelope, and
// delegates to the provided Sink. If topics contains "*", all topics are accepted.
// If topics is empty, a noop emitter is returned. The emitter owns the delivery
// audit log: a line is written iff an event was actually sent — filtered topics
// and the noop emitter return nil without logging.
func NewEmitter(sink Sink, deploymentID string, topics []string, logger *logging.Logger) Emitter {
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
		logger:       logger,
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

	if err := e.sendWithRetry(ctx, event); err != nil {
		return err
	}

	e.logger.Ctx(ctx).Audit("opevent delivered",
		append([]zap.Field{
			zap.String("opevent_id", event.ID),
			zap.String("topic", event.Topic),
			zap.String("tenant_id", event.TenantID),
		}, ev.LogFields...)...)
	return nil
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
