package mqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSConfig configures a NATS JetStream publish source.
//
// NATS JetStream is supported as a publish-mq only (Outpost reads events
// from one or more pre-provisioned JetStream consumers). Outpost does not
// create the underlying stream or consumer — the operator is expected to
// manage that lifecycle, typically alongside per-tenant NATS Account
// provisioning.
//
// One queue instance can consume from multiple NATS Accounts in parallel.
// Each account holds its own credentials and is dialled on its own NATS
// connection. With one Account per tenant, set Account.TenantID to make
// Outpost stamp the correct tenant on every event regardless of payload —
// see Variant 1 in the design notes.
type NATSConfig struct {
	// Servers is the NATS cluster URL list (e.g. ["nats://a:4222","nats://b:4222"]).
	Servers []string

	// Accounts lists the NATS Accounts this queue should consume from.
	Accounts []NATSAccountConfig
}

// NATSAccountConfig is a single NATS Account that Outpost consumes from.
type NATSAccountConfig struct {
	// Name is a short label used for logging and metrics.
	Name string

	// CredentialsFile points at a NATS .creds file (JWT + NKey seed).
	CredentialsFile string

	// Stream is the JetStream stream name the consumer reads from.
	Stream string

	// Consumer is the durable JetStream consumer name. Must be pre-created.
	Consumer string

	// TenantID, when set, overrides the tenant_id field on every event
	// from this account. Recommended pattern: one Account per Outpost tenant.
	// Leave empty to trust whatever tenant_id is in the payload.
	TenantID string
}

// NATSQueue is a publish-mq driver backed by NATS JetStream.
type NATSQueue struct {
	config *NATSConfig
	conns  []*natsConn
}

type natsConn struct {
	account NATSAccountConfig
	nc      *nats.Conn
	js      jetstream.JetStream
}

var _ Queue = (*NATSQueue)(nil)

// NewNATSQueue constructs (but does not connect) a NATS JetStream queue.
// Call Init to open the connections.
func NewNATSQueue(config *NATSConfig) *NATSQueue {
	return &NATSQueue{config: config}
}

// Init validates the configuration, opens one NATS connection per account,
// and verifies each stream + consumer exists. The returned cleanup function
// drains and closes every connection.
func (q *NATSQueue) Init(ctx context.Context) (func(), error) {
	if q.config == nil {
		return nil, errors.New("nats: nil config")
	}
	if len(q.config.Servers) == 0 {
		return nil, errors.New("nats: no servers configured")
	}
	if len(q.config.Accounts) == 0 {
		return nil, errors.New("nats: no accounts configured")
	}

	servers := strings.Join(q.config.Servers, ",")
	for _, acc := range q.config.Accounts {
		if err := acc.validate(); err != nil {
			q.closeAll()
			return nil, fmt.Errorf("nats: account %q: %w", acc.Name, err)
		}

		nc, err := nats.Connect(
			servers,
			nats.UserCredentials(acc.CredentialsFile),
			nats.Name(fmt.Sprintf("outpost:%s", acc.Name)),
			nats.MaxReconnects(-1),
		)
		if err != nil {
			q.closeAll()
			return nil, fmt.Errorf("nats: account %q: connect: %w", acc.Name, err)
		}

		js, err := jetstream.New(nc)
		if err != nil {
			nc.Close()
			q.closeAll()
			return nil, fmt.Errorf("nats: account %q: jetstream: %w", acc.Name, err)
		}

		if _, err := js.Stream(ctx, acc.Stream); err != nil {
			nc.Close()
			q.closeAll()
			return nil, fmt.Errorf("nats: account %q: stream %q: %w", acc.Name, acc.Stream, err)
		}
		if _, err := js.Consumer(ctx, acc.Stream, acc.Consumer); err != nil {
			nc.Close()
			q.closeAll()
			return nil, fmt.Errorf("nats: account %q: consumer %q: %w", acc.Name, acc.Consumer, err)
		}

		q.conns = append(q.conns, &natsConn{account: acc, nc: nc, js: js})
	}

	return func() { q.closeAll() }, nil
}

func (q *NATSQueue) closeAll() {
	for _, c := range q.conns {
		if c.nc != nil {
			_ = c.nc.Drain()
		}
	}
	q.conns = nil
}

// Publish is intentionally not implemented. JetStream is a publish-mq source
// only; events enter Outpost from publishers outside Outpost.
func (q *NATSQueue) Publish(ctx context.Context, msg IncomingMessage) error {
	return errors.New("nats: publish is not supported by the JetStream publish-mq driver")
}

// Subscribe opens a pull-based JetStream consumer per configured account
// and fans messages into a single multiplexed Subscription.
func (q *NATSQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	if len(q.conns) == 0 {
		return nil, errors.New("nats: queue not initialized")
	}

	options := ApplySubscribeOptions(opts)
	perAccount := options.Concurrency
	if perAccount <= 0 {
		perAccount = 1
	}

	sub := &natsSubscription{
		msgs: make(chan *Message),
		done: make(chan struct{}),
	}

	for _, c := range q.conns {
		consumer, err := c.js.Consumer(ctx, c.account.Stream, c.account.Consumer)
		if err != nil {
			_ = sub.Shutdown(context.Background())
			return nil, fmt.Errorf("nats: account %q: open consumer: %w", c.account.Name, err)
		}

		iter, err := consumer.Messages(jetstream.PullMaxMessages(perAccount))
		if err != nil {
			_ = sub.Shutdown(context.Background())
			return nil, fmt.Errorf("nats: account %q: messages: %w", c.account.Name, err)
		}

		sub.wg.Add(1)
		go sub.pump(c.account, iter)
	}

	return sub, nil
}

// SupportsConcurrency tells the upstream consumer to skip its own semaphore
// since pull concurrency is already bounded by PullMaxMessages per account.
func (q *NATSQueue) SupportsConcurrency() bool { return true }

// natsSubscription multiplexes messages from N per-account pull loops
// into a single Receive channel.
type natsSubscription struct {
	msgs chan *Message
	done chan struct{}
	wg   sync.WaitGroup
}

var (
	_ Subscription           = (*natsSubscription)(nil)
	_ ConcurrentSubscription = (*NATSQueue)(nil)
)

func (s *natsSubscription) Receive(ctx context.Context) (*Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, errors.New("nats: subscription closed")
	case msg := <-s.msgs:
		return msg, nil
	}
}

func (s *natsSubscription) Shutdown(_ context.Context) error {
	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
	}
	s.wg.Wait()
	return nil
}

func (s *natsSubscription) pump(account NATSAccountConfig, iter jetstream.MessagesContext) {
	defer s.wg.Done()
	defer iter.Stop()

	for {
		select {
		case <-s.done:
			return
		default:
		}

		jmsg, err := iter.Next()
		if err != nil {
			// Stop() / context cancellation propagate as iterator-closed errors.
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) {
				return
			}
			// Transient (e.g. missed heartbeat). Loop and re-fetch.
			continue
		}

		body := jmsg.Data()
		if account.TenantID != "" {
			if rewritten, rerr := overrideTenantID(body, account.TenantID); rerr == nil {
				body = rewritten
			}
		}

		out := &Message{
			QueueMessage: &natsQueueMessage{msg: jmsg},
			LoggableID:   formatLoggableID(account, jmsg),
			Body:         body,
		}

		select {
		case s.msgs <- out:
		case <-s.done:
			_ = jmsg.Nak()
			return
		}
	}
}

type natsQueueMessage struct {
	msg jetstream.Msg
}

func (m *natsQueueMessage) Ack()  { _ = m.msg.Ack() }
func (m *natsQueueMessage) Nack() { _ = m.msg.Nak() }

func (a NATSAccountConfig) validate() error {
	if a.CredentialsFile == "" {
		return errors.New("credentials_file is required")
	}
	if a.Stream == "" {
		return errors.New("stream is required")
	}
	if a.Consumer == "" {
		return errors.New("consumer is required")
	}
	return nil
}

func formatLoggableID(account NATSAccountConfig, jmsg jetstream.Msg) string {
	md, err := jmsg.Metadata()
	if err != nil || md == nil {
		return account.Name
	}
	return fmt.Sprintf("%s/%s:%d", account.Name, md.Stream, md.Sequence.Stream)
}

// overrideTenantID rewrites the tenant_id field on a JSON event payload.
// If the body is not a JSON object, it is returned unchanged.
func overrideTenantID(body []byte, tenantID string) ([]byte, error) {
	var event map[string]any
	if err := json.Unmarshal(body, &event); err != nil {
		return body, err
	}
	event["tenant_id"] = tenantID
	return json.Marshal(event)
}
