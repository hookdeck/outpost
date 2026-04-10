package opevents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
)

// MQSink sends operator events to a message queue via mqs.Queue.
type MQSink struct {
	queue     mqs.Queue
	cleanupFn func()
}

// NewMQSink creates a sink that publishes events to the given queue.
func NewMQSink(queue mqs.Queue) *MQSink {
	return &MQSink{queue: queue}
}

func (s *MQSink) Init(ctx context.Context) error {
	cleanup, err := s.queue.Init(ctx)
	if err != nil {
		return fmt.Errorf("opevents: failed to init MQ sink: %w", err)
	}
	s.cleanupFn = cleanup
	return nil
}

func (s *MQSink) Send(ctx context.Context, event *OperatorEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("opevents: failed to marshal event: %w", err)
	}
	msg := &eventMessage{data: data}
	if err := s.queue.Publish(ctx, msg); err != nil {
		return fmt.Errorf("opevents: failed to publish event: %w", err)
	}
	return nil
}

func (s *MQSink) Close() error {
	if s.cleanupFn != nil {
		s.cleanupFn()
	}
	return nil
}

// eventMessage adapts a serialized OperatorEvent to mqs.IncomingMessage.
type eventMessage struct {
	data []byte
}

func (m *eventMessage) ToMessage() (*mqs.Message, error) {
	return &mqs.Message{Body: m.data}, nil
}

func (m *eventMessage) FromMessage(msg *mqs.Message) error {
	m.data = msg.Body
	return nil
}
