package mqs

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSConfig configures a JetStream-backed queue. Unlike core NATS pub/sub,
// JetStream persists messages and supports at-least-once delivery via
// explicit ack/nak — the same durability guarantee RabbitMQ provides here.
type NATSConfig struct {
	ServerURL  string
	Stream     string
	Subject    string
	DLQSubject string // optional; exhausted messages are moved here instead of being lost
	MaxDeliver int
	AckWait    time.Duration
}

type NATSQueue struct {
	config *NATSConfig
	mu     sync.Mutex
	nc     *nats.Conn
	js     jetstream.JetStream
}

var _ Queue = &NATSQueue{}

func NewNATSQueue(config *NATSConfig) *NATSQueue {
	if config.MaxDeliver == 0 {
		config.MaxDeliver = 5
	}
	if config.AckWait == 0 {
		config.AckWait = 60 * time.Second
	}
	return &NATSQueue{config: config}
}

// ensureConnected lazily connects on first use and reconnects if the
// connection was lost — some callers (e.g. the log service's consumer
// worker) call Subscribe directly without ever calling Init first, exactly
// like RabbitMQQueue.ensureConnected already handles for that driver.
func (q *NATSQueue) ensureConnected() (jetstream.JetStream, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.js != nil && q.nc != nil && !q.nc.IsClosed() {
		return q.js, nil
	}
	nc, err := nats.Connect(q.config.ServerURL, nats.MaxReconnects(-1))
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	q.nc = nc
	q.js = js
	return js, nil
}

func (q *NATSQueue) Init(ctx context.Context) (func(), error) {
	if _, err := q.ensureConnected(); err != nil {
		return nil, err
	}
	return func() {
		q.mu.Lock()
		nc := q.nc
		q.mu.Unlock()
		if nc != nil {
			nc.Close()
		}
	}, nil
}

func (q *NATSQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	msg, err := incomingMessage.ToMessage()
	if err != nil {
		return err
	}

	js, err := q.ensureConnected()
	if err != nil {
		return err
	}

	_, err = js.Publish(ctx, q.config.Subject, msg.Body)
	return err
}

func (q *NATSQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	js, err := q.ensureConnected()
	if err != nil {
		return nil, err
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, q.config.Stream, jetstream.ConsumerConfig{
		Durable:       durableName(q.config.Subject),
		FilterSubject: q.config.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       q.config.AckWait,
		MaxDeliver:    q.config.MaxDeliver,
	})
	if err != nil {
		return nil, err
	}

	return &NATSSubscription{
		consumer:   consumer,
		js:         js,
		dlqSubject: q.config.DLQSubject,
		maxDeliver: q.config.MaxDeliver,
	}, nil
}

// durableName derives a valid JetStream durable consumer name from a subject.
// Durable names can't contain '.', '*', '>' or whitespace.
func durableName(subject string) string {
	name := strings.NewReplacer(".", "-", "*", "-", ">", "-", " ", "-").Replace(subject)
	return name + "-consumer"
}

// ============================== NATS Subscription ==============================

type NATSSubscription struct {
	consumer   jetstream.Consumer
	js         jetstream.JetStream
	dlqSubject string
	maxDeliver int
}

var _ Subscription = &NATSSubscription{}

func (s *NATSSubscription) Receive(ctx context.Context) (*Message, error) {
	for {
		batch, err := s.consumer.Fetch(1, jetstream.FetchMaxWait(30*time.Second))
		if err != nil {
			return nil, err
		}

		var result *Message
		for m := range batch.Messages() {
			if s.exceededMaxDeliver(m) {
				s.moveToDLQ(ctx, m)
				continue
			}
			result = &Message{
				QueueMessage: &natsQueueMessage{msg: m},
				LoggableID:   m.Subject(),
				Body:         m.Data(),
			}
			break
		}
		if err := batch.Error(); err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func (s *NATSSubscription) exceededMaxDeliver(m jetstream.Msg) bool {
	if s.dlqSubject == "" || s.maxDeliver <= 0 {
		return false
	}
	meta, err := m.Metadata()
	if err != nil {
		return false
	}
	return int(meta.NumDelivered) > s.maxDeliver
}

// moveToDLQ republishes an exhausted message to the DLQ subject and
// terminates it, so JetStream stops redelivering it — the equivalent of
// RabbitMQ's automatic dead-letter-exchange routing, done explicitly here
// since JetStream has no broker-side DLQ mechanism of its own.
func (s *NATSSubscription) moveToDLQ(ctx context.Context, m jetstream.Msg) {
	if s.js != nil {
		s.js.Publish(ctx, s.dlqSubject, m.Data())
	}
	m.Term()
}

func (s *NATSSubscription) Shutdown(ctx context.Context) error {
	return nil
}

type natsQueueMessage struct {
	msg jetstream.Msg
}

var _ QueueMessage = &natsQueueMessage{}

func (m *natsQueueMessage) Ack()  { m.msg.Ack() }
func (m *natsQueueMessage) Nack() { m.msg.Nak() }
