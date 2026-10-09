package mcpevents

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

// Notifier defaults.
const (
	DefaultNotifierQueueSize    = 1024
	DefaultNotifierWorkers      = 8
	DefaultNotifierTimeout      = 10 * time.Second
	DefaultNotifierDrainTimeout = 5 * time.Second

	// maxDiscardBytes bounds how much of a response is read (and dropped) to
	// keep the connection reusable.
	maxDiscardBytes = 64 << 10
	// A Termination whose host stays at its in-flight limit waits up to
	// hostWait in the worker, then goes back to the end of the queue, at
	// most maxDeferrals times, so one slow host can't hold every worker.
	hostWait     = time.Second
	maxDeferrals = 5
)

// ErrInvalidTermination is returned for a Termination without a URL, a
// header-safe subscription ID or an error.
var ErrInvalidTermination = errors.New("mcpevents: termination needs a url, a subscription id and an error")

// NotifierConfig configures a Notifier. Zero values take the defaults above.
type NotifierConfig struct {
	// Client sends the envelopes. It must be the SSRF-guarded client
	// deliveries use; required unless Disabled.
	Client *http.Client
	// HostLimiter is optional.
	HostLimiter HostLimiter
	// Profile selects the error codes in the envelope (MCP_ERROR_CODES).
	Profile CodeProfile
	// Disabled turns every enqueue into a no-op (MCP_SEND_TERMINATED=false).
	Disabled     bool
	QueueSize    int
	Workers      int
	Timeout      time.Duration // per send
	DrainTimeout time.Duration // on Close
	Logger       *logging.Logger
	// Now is the clock (tests).
	Now func() time.Time
}

// Termination is one terminated envelope to send, best effort and once, to a
// subscription that has just been deleted.
type Termination struct {
	TenantID       string
	SubscriptionID string
	// URL is the subscription's callback URL.
	URL string
	// Secrets sign the envelope like a delivery: the current secret and,
	// during rotation, the previous one until its InvalidAt.
	Secrets []Secret
	// CreatedAt is the subscription's creation time; with SubscriptionID it
	// makes the webhook-id deterministic (TerminatedMessageID).
	CreatedAt time.Time
	// Error is AccessRevoked(), EventEnded() or SchemaChanged(); its codes
	// come from the Notifier's profile.
	Error *Error
}

// NotifierStats counts outcomes since start.
type NotifierStats struct {
	Sent    uint64 // answered 2xx
	Failed  uint64 // sent and refused, or not sendable
	Dropped uint64 // queue full, closed, or deferred too often
}

type termination struct {
	Termination
	deferrals int
}

// Notifier sends terminated envelopes from a bounded queue. Request paths use
// Enqueue, which never blocks (a full queue drops and counts); background
// producers use EnqueueWait, which waits for room so a mass termination is
// paced by the Notifier instead of dropped.
type Notifier struct {
	client      *http.Client
	hostLimiter HostLimiter
	hostWait    time.Duration
	profile     CodeProfile
	timeout     time.Duration
	logger      *zap.Logger
	now         func() time.Time
	q           *workQueue[termination]
	sent        atomic.Uint64
	failed      atomic.Uint64
	extraDrops  atomic.Uint64
}

// NewNotifier starts a Notifier's workers (none when disabled).
func NewNotifier(cfg NotifierConfig) (*Notifier, error) {
	if cfg.Disabled {
		return &Notifier{}, nil
	}
	if cfg.Client == nil {
		return nil, errors.New("mcpevents: notifier needs a client")
	}
	n := &Notifier{
		client:      noRedirects(cfg.Client),
		hostLimiter: cfg.HostLimiter,
		hostWait:    hostWait,
		profile:     cfg.Profile,
		timeout:     positiveOr(cfg.Timeout, DefaultNotifierTimeout),
		logger:      zapLogger(cfg.Logger),
		now:         cfg.Now,
	}
	if n.now == nil {
		n.now = time.Now
	}
	n.q = newWorkQueue(
		positiveOr(cfg.QueueSize, DefaultNotifierQueueSize),
		positiveOr(cfg.Workers, DefaultNotifierWorkers),
		positiveOr(cfg.DrainTimeout, DefaultNotifierDrainTimeout),
		n.logger,
		n.handle,
	)
	return n, nil
}

// Enabled reports whether envelopes are sent at all.
func (n *Notifier) Enabled() bool {
	return n.q != nil
}

// Enqueue queues t without blocking. It returns false when t was not queued:
// the notifier is disabled or closed, t is invalid, or the queue is full (a
// counted, logged drop).
func (n *Notifier) Enqueue(t Termination) bool {
	if n.q == nil || !validTermination(t) {
		return false
	}
	if !n.q.tryPush(termination{Termination: t}) {
		n.extraDrops.Add(1)
		n.logDrop(t, "queue full or closed")
		return false
	}
	return true
}

// EnqueueWait queues t, waiting for room until ctx is done. It returns nil
// when disabled, ErrQueueClosed after Close and ErrInvalidTermination for an
// invalid t.
func (n *Notifier) EnqueueWait(ctx context.Context, t Termination) error {
	if n.q == nil {
		return nil
	}
	if !validTermination(t) {
		return ErrInvalidTermination
	}
	return n.q.push(ctx, termination{Termination: t})
}

func validTermination(t Termination) bool {
	return t.URL != "" && visibleASCII(t.SubscriptionID, MaxEventIDBytes) && t.Error != nil
}

// Close stops intake and drains the queue for up to the drain timeout; sends
// still pending then are abandoned. Safe to call more than once.
func (n *Notifier) Close() error {
	if n.q != nil {
		n.q.close()
	}
	return nil
}

// Stats returns the outcome counters.
func (n *Notifier) Stats() NotifierStats {
	if n.q == nil {
		return NotifierStats{}
	}
	return NotifierStats{
		Sent:    n.sent.Load(),
		Failed:  n.failed.Load(),
		Dropped: n.q.dropped.Load() + n.extraDrops.Load(),
	}
}

func (n *Notifier) logDrop(t Termination, why string) {
	n.logger.Warn("mcp terminated envelope dropped",
		zap.String("tenant_id", t.TenantID),
		zap.String("subscription_id", t.SubscriptionID),
		zap.String("reason", why),
		zap.Uint64("dropped_total", n.Stats().Dropped))
}

func (n *Notifier) handle(ctx context.Context, t termination) {
	u, _, err := NormalizeCallbackURL(t.URL)
	if err != nil {
		n.failed.Add(1)
		n.logger.Warn("mcp terminated envelope not sent: invalid url", zap.String("tenant_id", t.TenantID), zap.String("subscription_id", t.SubscriptionID))
		return
	}
	release, ok := n.acquireHost(ctx, HostPort(u))
	if !ok {
		if ctx.Err() != nil {
			n.extraDrops.Add(1) // past the drain deadline
			return
		}
		t.deferrals++
		if t.deferrals > maxDeferrals || !n.q.tryPush(t) {
			n.extraDrops.Add(1)
			n.logDrop(t.Termination, "host at its in-flight limit")
		}
		return
	}
	defer release()

	log := n.logger.With(zap.String("tenant_id", t.TenantID), zap.String("subscription_id", t.SubscriptionID))
	status, err := n.send(ctx, u.String(), t.Termination)
	switch {
	case errors.Is(err, ErrNoSigningKey):
		n.failed.Add(1)
		log.Warn("mcp terminated envelope not sent: no valid secret")
	case err != nil:
		n.failed.Add(1)
		log.Info("mcp terminated envelope not delivered", zap.String("reason", ClassifyRequestError(err)))
	case status < 200 || status > 299:
		n.failed.Add(1)
		log.Info("mcp terminated envelope refused", zap.Int("status", status))
	default:
		n.sent.Add(1)
		log.Info("mcp terminated envelope delivered")
	}
}

// acquireHost takes a host slot, polling for up to n.hostWait.
func (n *Notifier) acquireHost(ctx context.Context, hostport string) (func(), bool) {
	if n.hostLimiter == nil {
		return func() {}, true
	}
	deadline := time.Now().Add(n.hostWait)
	backoff := 25 * time.Millisecond
	for {
		if release, ok := n.hostLimiter.TryAcquire(hostport); ok {
			return release, true
		}
		if time.Now().Add(backoff).After(deadline) {
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// send POSTs the envelope with delivery headers and returns the status.
func (n *Notifier) send(ctx context.Context, target string, t Termination) (int, error) {
	now := n.now()
	keys := ActiveKeys(t.Secrets, now)
	if len(keys) == 0 {
		return 0, ErrNoSigningKey
	}
	body, err := TerminatedEnvelope(t.Error.WithProfile(n.profile))
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if err := SetHeaders(req.Header, TerminatedMessageID(t.SubscriptionID, t.CreatedAt.UnixMilli()), t.SubscriptionID, now, body, keys); err != nil {
		return 0, err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDiscardBytes))
	return resp.StatusCode, nil
}
