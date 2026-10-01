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
//
// NATS servers default max_payload to 1MiB. A larger event passes Outpost's
// own API validation and then fails at publish time here, so a server
// handling larger events needs max_payload raised in its own config.
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
		// Must track mqinfra's own default (Policy.RetryLimit's fallback of
		// 5, +2 — see infraNATS.Declare) since nothing currently threads the
		// real Policy.RetryLimit through to this config: a mismatch here
		// makes exceededMaxDeliver trigger a delivery before the JetStream
		// consumer's actual MaxDeliver is reached, moving a message to the
		// DLQ (and terming it) while the broker would still have redelivered
		// it at least once more.
		config.MaxDeliver = 7
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
	// ReconnectBufSize(-1) disables the client-side reconnect buffer: without
	// it, a Publish made while disconnected is silently queued and flushed
	// on reconnect *after* this call has already returned an error to the
	// caller — so a caller that retries on that error ends up delivering
	// the message twice. Disabling the buffer makes Publish fail fast while
	// disconnected instead, matching the at-least-once (not "silently more
	// than once") guarantee the other providers give.
	nc, err := nats.Connect(q.config.ServerURL, nats.MaxReconnects(-1), nats.ReconnectBufSize(-1))
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

	// Look up the consumer mqinfra already provisioned instead of
	// re-declaring it here — CreateOrUpdateConsumer would silently overwrite
	// MaxDeliver/AckWait with whatever this process happens to have
	// configured, even when MQS_AUTO_PROVISION=false, letting the live
	// consumer drift from what mqinfra declared on every restart.
	consumer, err := js.Consumer(ctx, q.config.Stream, durableName(q.config.Subject))
	if err != nil {
		return nil, err
	}

	// PullMaxMessages(1): Messages buffers up to 500 messages client-side by
	// default. Receive only ever processes one at a time, so under backlog
	// the rest of that buffer sits unacked long enough for AckWait to lapse
	// — JetStream redelivers them elsewhere while this buffer still goes on
	// to process the original copies too, delivering both.
	msgs, err := consumer.Messages(jetstream.PullMaxMessages(1))
	if err != nil {
		return nil, err
	}

	return &NATSSubscription{
		msgs:       msgs,
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
	msgs       jetstream.MessagesContext
	js         jetstream.JetStream
	dlqSubject string
	maxDeliver int
}

var _ Subscription = &NATSSubscription{}

// Receive uses the Messages() iterator rather than repeated single-message
// Fetch calls: Fetch doesn't take a context, so a Receive blocked waiting on
// it can hold up shutdown for its full FetchMaxWait (30s here); Next, via
// NextContext, returns as soon as ctx is done. Messages also lets JetStream
// overlap pull requests instead of paying a full round-trip per message,
// which Fetch(1, ...) caps regardless of consumer concurrency settings.
func (s *NATSSubscription) Receive(ctx context.Context) (*Message, error) {
	for {
		m, err := s.msgs.Next(jetstream.NextContext(ctx))
		if err != nil {
			return nil, err
		}

		if s.exceededMaxDeliver(m) {
			s.moveToDLQ(ctx, m)
			continue
		}

		return &Message{
			QueueMessage: &natsQueueMessage{msg: m},
			LoggableID:   m.Subject(),
			Body:         m.Data(),
		}, nil
	}
}

// exceededMaxDeliver reports whether this is the last delivery JetStream
// will make. NumDelivered never exceeds MaxDeliver — JetStream simply stops
// redelivering after the MaxDeliver-th attempt — so gating on ">" never
// fires; the message is then neither acked, nacked nor termed, and (with
// WorkQueue retention and no message limits) sits in the stream forever.
// ">=" catches it on that final allowed delivery instead.
func (s *NATSSubscription) exceededMaxDeliver(m jetstream.Msg) bool {
	if s.dlqSubject == "" || s.maxDeliver <= 0 {
		return false
	}
	meta, err := m.Metadata()
	if err != nil {
		return false
	}
	return int(meta.NumDelivered) >= s.maxDeliver
}

// moveToDLQ republishes an exhausted message to the DLQ subject and
// terminates it, so JetStream stops redelivering it — the equivalent of
// RabbitMQ's automatic dead-letter-exchange routing, done explicitly here
// since JetStream has no broker-side DLQ mechanism of its own. If the DLQ
// publish itself fails, Term would drop the message with no record of it
// anywhere; Nak instead so it's redelivered and gets another chance to
// reach the DLQ, rather than being silently lost.
func (s *NATSSubscription) moveToDLQ(ctx context.Context, m jetstream.Msg) {
	if s.js == nil {
		m.Nak()
		return
	}
	if _, err := s.js.Publish(ctx, s.dlqSubject, m.Data()); err != nil {
		m.Nak()
		return
	}
	m.Term()
}

func (s *NATSSubscription) Shutdown(ctx context.Context) error {
	s.msgs.Stop()
	return nil
}

type natsQueueMessage struct {
	msg jetstream.Msg
}

var _ QueueMessage = &natsQueueMessage{}

func (m *natsQueueMessage) Ack()  { m.msg.Ack() }
func (m *natsQueueMessage) Nack() { m.msg.Nak() }
