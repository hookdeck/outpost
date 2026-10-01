package mqs

import (
	"context"
	"log"
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

	sub := &NATSSubscription{
		msgs:       msgs,
		js:         js,
		dlqSubject: q.config.DLQSubject,
		// Read from the live consumer rather than a locally-configured
		// value: with MQS_AUTO_PROVISION=false an operator may have created
		// the consumer with a different MaxDeliver than this process
		// assumes, and nothing else keeps the two in sync — a message would
		// then reach the DLQ early, or never, depending on which way they
		// drifted.
		maxDeliver: consumer.CachedInfo().Config.MaxDeliver,
	}
	return limitBytes(sub, ApplySubscribeOptions(opts).MaxBytes), nil
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
//
// Every delivery is handed back here, including the last one JetStream will
// ever make for this message — unlike RabbitMQ/SQS, nothing here decides a
// message is "exhausted" before the caller gets to see it. That decision
// instead happens in the returned message's own Nack (see natsQueueMessage),
// so the handler gets its full MaxDeliver attempts, the same as every other
// provider, rather than losing the last one to this provider's own
// client-side DLQ handling.
func (s *NATSSubscription) Receive(ctx context.Context) (*Message, error) {
	m, err := s.msgs.Next(jetstream.NextContext(ctx))
	if err != nil {
		return nil, err
	}

	return &Message{
		QueueMessage: &natsQueueMessage{
			msg:        m,
			js:         s.js,
			dlqSubject: s.dlqSubject,
			maxDeliver: s.maxDeliver,
		},
		LoggableID: m.Subject(),
		Body:       m.Data(),
	}, nil
}

func (s *NATSSubscription) Shutdown(ctx context.Context) error {
	s.msgs.Stop()
	return nil
}

type natsQueueMessage struct {
	msg        jetstream.Msg
	js         jetstream.JetStream
	dlqSubject string
	maxDeliver int
}

var _ QueueMessage = &natsQueueMessage{}

func (m *natsQueueMessage) Ack() { m.msg.Ack() }

// Nack redelivers the message normally, unless this is the last delivery
// JetStream will ever make for it (NumDelivered >= MaxDeliver) — gating on
// ">" never fires, since JetStream simply stops redelivering after the
// MaxDeliver-th attempt, so a message that reaches here on its final
// attempt would otherwise be neither acked, nacked nor termed, and (with
// WorkQueue retention and no message limits) sit in the stream forever.
// On that final attempt, this republishes it to the DLQ subject and
// terminates it instead — the equivalent of RabbitMQ's automatic
// dead-letter-exchange routing, done explicitly here since JetStream has no
// broker-side DLQ mechanism of its own.
func (m *natsQueueMessage) Nack() {
	if m.exceededMaxDeliver() {
		m.moveToDLQ()
		return
	}
	m.msg.Nak()
}

func (m *natsQueueMessage) exceededMaxDeliver() bool {
	if m.dlqSubject == "" || m.maxDeliver <= 0 {
		return false
	}
	meta, err := m.msg.Metadata()
	if err != nil {
		return false
	}
	return int(meta.NumDelivered) >= m.maxDeliver
}

// moveToDLQ runs on a message's last allowed delivery, so unlike every
// other Nack, there's no further redelivery for a failed DLQ publish to
// fall back on — a plain Nak here would just be silently dropped by
// JetStream too. A short bounded retry covers a transient failure; one
// that's still failing after that is logged, since that's the only
// remaining way to keep it from disappearing without a trace. Terminated
// either way: leaving it unacked forever (the original bug this fixes) is
// worse than a rare, logged loss.
func (m *natsQueueMessage) moveToDLQ() {
	defer m.msg.Term()

	if m.js == nil {
		return
	}

	ctx := context.Background()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}
		if _, err = m.js.Publish(ctx, m.dlqSubject, m.msg.Data()); err == nil {
			return
		}
	}
	log.Printf("nats: giving up moving exhausted message to DLQ subject %q after retries: %v", m.dlqSubject, err)
}
