package mqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// pumpBackoff is the cooldown between iter.Next() returns and the next
// fetch when the previous call returned a transient error. Prevents
// busy-spinning when the upstream consumer is briefly unhealthy.
const pumpBackoff = 250 * time.Millisecond

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
// Outpost stamp the correct tenant on every event regardless of payload.
type NATSConfig struct {
	// Servers is the NATS cluster URL list (e.g. ["nats://a:4222","nats://b:4222"]).
	Servers []string

	// Accounts is the static list of accounts the queue should consume from.
	// Combined with AccountsDir if both are set.
	Accounts []NATSAccountConfig

	// AccountsDir, when set, makes the queue watch a directory for tenant
	// subdirectories. Each subdirectory contains a meta.yaml describing the
	// account plus a .creds file. Accounts are added and removed at runtime
	// as directories appear and disappear, without restarting Outpost.
	AccountsDir string
}

// NATSAccountConfig is a single NATS Account that Outpost consumes from.
type NATSAccountConfig struct {
	// Name is a short label used for logging, metrics, and account identity
	// inside the queue. Must be unique within a NATSConfig.
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

	mu    sync.Mutex
	conns map[string]*natsConn // by account name
	sub   *natsSubscription    // active subscription, nil before Subscribe

	servers string
	watcher *natsAccountsWatcher
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
	return &NATSQueue{
		config: config,
		conns:  make(map[string]*natsConn),
	}
}

// Init validates the configuration, opens one NATS connection per account,
// verifies each stream + consumer exists, and (if AccountsDir is set) starts
// a filesystem watcher that adds and removes accounts at runtime. The
// returned cleanup function drains every connection and stops the watcher.
func (q *NATSQueue) Init(ctx context.Context) (func(), error) {
	if q.config == nil {
		return nil, errors.New("nats: nil config")
	}
	if len(q.config.Servers) == 0 {
		return nil, errors.New("nats: no servers configured")
	}

	q.servers = strings.Join(q.config.Servers, ",")

	accounts := append([]NATSAccountConfig(nil), q.config.Accounts...)
	if q.config.AccountsDir != "" {
		dirAccounts, err := loadAccountsFromDir(q.config.AccountsDir)
		if err != nil {
			return nil, fmt.Errorf("nats: %w", err)
		}
		accounts = append(accounts, dirAccounts...)
	}
	if len(accounts) == 0 {
		return nil, errors.New("nats: no accounts configured")
	}

	for _, acc := range accounts {
		if err := q.addAccount(ctx, acc); err != nil {
			q.closeAll()
			return nil, err
		}
	}

	if q.config.AccountsDir != "" {
		w, err := newNATSAccountsWatcher(q.config.AccountsDir, q.reconcileFromDir)
		if err != nil {
			q.closeAll()
			return nil, fmt.Errorf("nats: watch accounts_dir: %w", err)
		}
		q.watcher = w
		w.start()
	}

	return func() { q.closeAll() }, nil
}

func (q *NATSQueue) closeAll() {
	if q.watcher != nil {
		q.watcher.stop()
		q.watcher = nil
	}

	q.mu.Lock()
	if q.sub != nil {
		q.sub.stopAll()
	}
	conns := q.conns
	q.conns = make(map[string]*natsConn)
	q.mu.Unlock()

	for _, c := range conns {
		if c.nc != nil {
			_ = c.nc.Drain()
		}
	}
}

// addAccount connects to NATS for a single account, verifies stream and
// consumer, registers the connection, and (if a subscription is active)
// starts a pump for it. Caller must not hold q.mu.
func (q *NATSQueue) addAccount(ctx context.Context, acc NATSAccountConfig) error {
	if err := acc.validate(); err != nil {
		return fmt.Errorf("nats: account %q: %w", acc.Name, err)
	}

	q.mu.Lock()
	if _, exists := q.conns[acc.Name]; exists {
		q.mu.Unlock()
		return nil
	}
	q.mu.Unlock()

	opts := []nats.Option{
		nats.Name(fmt.Sprintf("outpost:%s", acc.Name)),
		nats.MaxReconnects(-1),
	}
	if acc.CredentialsFile != "" {
		opts = append(opts, nats.UserCredentials(acc.CredentialsFile))
	}
	nc, err := nats.Connect(q.servers, opts...)
	if err != nil {
		return fmt.Errorf("nats: account %q: connect: %w", acc.Name, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return fmt.Errorf("nats: account %q: jetstream: %w", acc.Name, err)
	}

	if _, err := js.Stream(ctx, acc.Stream); err != nil {
		nc.Close()
		return fmt.Errorf("nats: account %q: stream %q: %w", acc.Name, acc.Stream, err)
	}
	if _, err := js.Consumer(ctx, acc.Stream, acc.Consumer); err != nil {
		nc.Close()
		return fmt.Errorf("nats: account %q: consumer %q: %w", acc.Name, acc.Consumer, err)
	}

	conn := &natsConn{account: acc, nc: nc, js: js}

	q.mu.Lock()
	q.conns[acc.Name] = conn
	sub := q.sub
	q.mu.Unlock()

	if sub != nil {
		if err := sub.startPump(ctx, conn); err != nil {
			q.removeAccount(acc.Name)
			return err
		}
	}
	return nil
}

// removeAccount stops the pump (if any) and drains the connection for the
// named account. No-op if the account is not currently registered.
func (q *NATSQueue) removeAccount(name string) {
	q.mu.Lock()
	conn, ok := q.conns[name]
	if !ok {
		q.mu.Unlock()
		return
	}
	delete(q.conns, name)
	sub := q.sub
	q.mu.Unlock()

	if sub != nil {
		sub.stopPump(name)
	}
	if conn.nc != nil {
		_ = conn.nc.Drain()
	}
}

// reconcileFromDir is invoked by the watcher on every directory change.
// It diffs the current set of dir-derived accounts against q.conns and
// adds/removes as needed. Static accounts (from q.config.Accounts) are
// preserved.
func (q *NATSQueue) reconcileFromDir() {
	desired, err := loadAccountsFromDir(q.config.AccountsDir)
	if err != nil {
		return
	}

	staticNames := make(map[string]struct{}, len(q.config.Accounts))
	for _, a := range q.config.Accounts {
		staticNames[a.Name] = struct{}{}
	}

	desiredSet := make(map[string]NATSAccountConfig, len(desired))
	for _, a := range desired {
		desiredSet[a.Name] = a
	}

	q.mu.Lock()
	current := make(map[string]NATSAccountConfig, len(q.conns))
	for name, c := range q.conns {
		current[name] = c.account
	}
	q.mu.Unlock()

	for name := range current {
		if _, isStatic := staticNames[name]; isStatic {
			continue
		}
		if _, stillThere := desiredSet[name]; !stillThere {
			q.removeAccount(name)
		}
	}

	for _, acc := range desired {
		if _, alreadyHave := current[acc.Name]; alreadyHave {
			continue
		}
		if err := q.addAccount(context.Background(), acc); err != nil {
			// Surface the failure so operators can spot bad creds, missing
			// streams, or unreachable servers. The next FS event re-triggers
			// reconcile, so a transient failure self-heals.
			log.Printf("nats: reconcile add account %q failed: %v", acc.Name, err)
		}
	}
}

// Publish is intentionally not implemented. JetStream is a publish-mq
// source only; events enter Outpost from publishers outside Outpost.
func (q *NATSQueue) Publish(ctx context.Context, msg IncomingMessage) error {
	return errors.New("nats: publish is not supported by the JetStream publish-mq driver")
}

// Subscribe opens a pull-based JetStream consumer per registered account
// and fans messages into a single multiplexed Subscription. Accounts added
// later (via the directory watcher) automatically get a pump started too.
func (q *NATSQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	q.mu.Lock()
	if len(q.conns) == 0 {
		q.mu.Unlock()
		return nil, errors.New("nats: queue not initialized")
	}
	if q.sub != nil {
		q.mu.Unlock()
		return nil, errors.New("nats: already subscribed")
	}

	options := ApplySubscribeOptions(opts)
	perAccount := options.Concurrency
	if perAccount <= 0 {
		perAccount = 1
	}

	sub := &natsSubscription{
		msgs:       make(chan *Message),
		done:       make(chan struct{}),
		iters:      make(map[string]jetstream.MessagesContext),
		perAccount: perAccount,
	}
	q.sub = sub

	conns := make([]*natsConn, 0, len(q.conns))
	for _, c := range q.conns {
		conns = append(conns, c)
	}
	q.mu.Unlock()

	for _, c := range conns {
		if err := sub.startPump(ctx, c); err != nil {
			_ = sub.Shutdown(context.Background())
			return nil, err
		}
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

	perAccount int

	itersMu sync.Mutex
	iters   map[string]jetstream.MessagesContext // by account name
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
	s.stopAll()
	return nil
}

func (s *natsSubscription) stopAll() {
	select {
	case <-s.done:
		return
	default:
	}
	close(s.done)

	s.itersMu.Lock()
	for _, iter := range s.iters {
		iter.Stop()
	}
	s.iters = make(map[string]jetstream.MessagesContext)
	s.itersMu.Unlock()

	s.wg.Wait()
}

func (s *natsSubscription) startPump(ctx context.Context, conn *natsConn) error {
	consumer, err := conn.js.Consumer(ctx, conn.account.Stream, conn.account.Consumer)
	if err != nil {
		return fmt.Errorf("nats: account %q: open consumer: %w", conn.account.Name, err)
	}
	iter, err := consumer.Messages(jetstream.PullMaxMessages(s.perAccount))
	if err != nil {
		return fmt.Errorf("nats: account %q: messages: %w", conn.account.Name, err)
	}

	s.itersMu.Lock()
	if existing, ok := s.iters[conn.account.Name]; ok {
		existing.Stop()
	}
	s.iters[conn.account.Name] = iter
	s.itersMu.Unlock()

	s.wg.Add(1)
	go s.pump(conn.account, iter)
	return nil
}

func (s *natsSubscription) stopPump(name string) {
	s.itersMu.Lock()
	iter, ok := s.iters[name]
	if ok {
		delete(s.iters, name)
	}
	s.itersMu.Unlock()
	if ok {
		iter.Stop()
	}
}

func (s *natsSubscription) pump(account NATSAccountConfig, iter jetstream.MessagesContext) {
	defer s.wg.Done()

	for {
		select {
		case <-s.done:
			return
		default:
		}

		jmsg, err := iter.Next()
		if err != nil {
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) {
				return
			}
			// Transient error (connection blip, server restart, consumer
			// leader change). Back off briefly so we don't peg CPU and
			// give the cluster room to recover, but stay responsive to
			// shutdown.
			select {
			case <-time.After(pumpBackoff):
			case <-s.done:
				return
			}
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
	if a.Name == "" {
		return errors.New("name is required")
	}
	if a.Stream == "" {
		return errors.New("stream is required")
	}
	if a.Consumer == "" {
		return errors.New("consumer is required")
	}
	// credentials_file is optional: a NATS deployment may use no-auth,
	// nkey-via-server-config, or token-via-URL.
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
