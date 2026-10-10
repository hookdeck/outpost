package mcpevents

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

// HostLimiter caps concurrent requests per callback host:port. It is shared
// by deliveries, verification challenges and terminated notifications, so one
// slow endpoint can't take every slot.
type HostLimiter interface {
	// Acquire takes a slot for hostport, waiting for one until ctx is done
	// (then it returns ctx's error). release must be called once.
	Acquire(ctx context.Context, hostport string) (release func(), err error)
}

// Verifier defaults.
const (
	DefaultVerificationTTL          = 24 * time.Hour
	DefaultVerificationRateLimit    = 10  // uncached attempts per (tenant, principal) per minute
	DefaultVerificationFailureLimit = 120 // failed challenges per (tenant, host) per minute
	DefaultVerificationMaxInFlight  = 64  // concurrent challenges per process
	DefaultVerificationTimeout      = 10 * time.Second
	// MaxChallengeResponseBytes caps how much of a challenge response is read.
	MaxChallengeResponseBytes = 64 << 10
	// HostFailureFactor scales the failure limit into the deployment-wide
	// budget of unanswered challenges per host.
	HostFailureFactor = 10

	inFlightWait = 2 * time.Second
	// challengeHostWait bounds the wait for a host slot held by deliveries
	// or other challenges.
	challengeHostWait = 2 * time.Second
	// claimPoll is how often a challenge another replica is sending is
	// checked for an outcome.
	claimPoll  = 100 * time.Millisecond
	claimSlack = 5 * time.Second
	rateWindow = time.Minute
)

// VerifierConfig configures a Verifier. Zero values take the defaults above;
// a negative RateLimit or FailureLimit disables that limit (other settings
// can't be disabled: non-positive means the default).
type VerifierConfig struct {
	// Client sends challenges. It must be the SSRF-guarded client deliveries
	// use (address checks at dial time, no redirects, no environment proxy).
	Client *http.Client
	Store  VerificationStore
	// HostLimiter is optional.
	HostLimiter HostLimiter
	// TTL is how long a passed challenge is cached (MCP_VERIFICATION_TTL).
	TTL time.Duration
	// RateLimit caps uncached attempts per (tenant, principal) per minute
	// (MCP_VERIFICATION_RATE_LIMIT).
	RateLimit int
	// FailureLimit caps failed challenges per (tenant, host:port) per minute
	// (MCP_VERIFICATION_FAILURE_LIMIT), so a tenant can't use Outpost to
	// probe a host, and HostFailureFactor times it caps unanswered ones
	// (timeout, connection refused, TLS failure) per host:port across the
	// deployment, so a host that doesn't answer stops being contacted
	// however many tenants ask. A host that answers never uses another
	// tenant's budget.
	FailureLimit int
	// Exempt reports hosts (the URL's hostname, IP literals unbracketed)
	// that skip the failure budgets, such as allowlisted ones.
	Exempt func(host string) bool
	// MaxInFlight caps concurrent challenges in this process.
	MaxInFlight int
	// Timeout bounds one challenge, response included.
	Timeout time.Duration
	Logger  *logging.Logger
	// Now is the clock (tests).
	Now func() time.Time
}

// VerifyRequest identifies the (tenant, principal, url) to verify.
type VerifyRequest struct {
	TenantID  string
	Principal string
	// URL is the callback URL; it is normalized again here, so cache and
	// rate-limit keys never depend on the caller's spelling.
	URL string
	// SubscriptionID goes out as X-MCP-Subscription-Id.
	SubscriptionID string
	// Secrets sign the challenge, like a delivery.
	Secrets []Secret
}

var errInvalidSubscriptionID = errors.New("mcpevents: subscription id is not a valid header value")

// Verifier runs MCP Events endpoint verification: a signed verification
// envelope whose challenge the endpoint must echo, cached per (tenant,
// principal, url). Concurrent verifications of one (tenant, principal, url)
// send one challenge: in this process they wait for the first one, and
// across replicas the store's claim lets one replica at a time send it.
type Verifier struct {
	client       *http.Client
	store        VerificationStore
	hostLimiter  HostLimiter
	ttl          time.Duration
	rateLimit    int
	failureLimit int
	exempt       func(string) bool
	inFlight     chan struct{}
	inFlightWait time.Duration
	hostWait     time.Duration
	claimPoll    time.Duration
	timeout      time.Duration
	logger       *zap.Logger
	now          func() time.Time

	mu      sync.Mutex
	flights map[string]*flight
}

// flight is an uncached verification in progress in this process.
type flight struct {
	done chan struct{}
	keys string // digest of the signing keys
	err  error  // the outcome, set before done is closed
}

// NewVerifier returns a Verifier; Client and Store are required.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	if cfg.Client == nil || cfg.Store == nil {
		return nil, errors.New("mcpevents: verifier needs a client and a store")
	}
	v := &Verifier{
		client:       noRedirects(cfg.Client),
		store:        cfg.Store,
		hostLimiter:  cfg.HostLimiter,
		ttl:          positiveOr(cfg.TTL, DefaultVerificationTTL),
		rateLimit:    orDefault(cfg.RateLimit, DefaultVerificationRateLimit),
		failureLimit: orDefault(cfg.FailureLimit, DefaultVerificationFailureLimit),
		exempt:       cfg.Exempt,
		inFlight:     make(chan struct{}, positiveOr(cfg.MaxInFlight, DefaultVerificationMaxInFlight)),
		inFlightWait: inFlightWait,
		hostWait:     challengeHostWait,
		claimPoll:    claimPoll,
		timeout:      positiveOr(cfg.Timeout, DefaultVerificationTimeout),
		logger:       zapLogger(cfg.Logger),
		now:          cfg.Now,
		flights:      make(map[string]*flight),
	}
	if v.now == nil {
		v.now = time.Now
	}
	return v, nil
}

// noRedirects returns a copy of c that never follows redirects, whatever c
// was configured with: a redirect could point anywhere, past the address
// checks on the original URL.
func noRedirects(c *http.Client) *http.Client {
	cp := *c
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cp
}

func orDefault[T int | time.Duration](v, def T) T {
	if v == 0 {
		return def
	}
	return v
}

// positiveOr is orDefault for settings that can't be disabled.
func positiveOr[T int | time.Duration](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

func zapLogger(l *logging.Logger) *zap.Logger {
	if l == nil || l.Logger == nil {
		return zap.NewNop()
	}
	return l.Logger
}

// Verified reports whether (tenant, principal, url) is in the verification
// cache, without sending anything. Callers use it to skip subscribe-time
// address checks on refreshes.
func (v *Verifier) Verified(ctx context.Context, tenantID, principal, rawURL string) (bool, error) {
	_, key, err := NormalizeCallbackURL(rawURL)
	if err != nil {
		return false, err
	}
	return v.store.Verified(ctx, tenantID, principal, key)
}

// Verify returns nil when (tenant, principal, url) passed a challenge within
// the TTL or passes one now. MCP-level failures are *Error:
// invalid_params (URL, no valid secret); resource_exhausted with limit
// "verification_rate" (the principal's challenges, or the tenant's failed
// challenges to the host, this minute) or "callback_host_busy" (the host is
// at its in-flight limit, or left too many challenges unanswered this
// minute); and callback_endpoint_error {reason} with reason one of
// http_5xx, http_4xx, challenge_failed, timeout, tls_error and
// connection_refused (which includes address-guard rejections at dial time).
// Store failures, a done ctx and a malformed SubscriptionID (a caller bug)
// are returned as plain errors.
func (v *Verifier) Verify(ctx context.Context, req VerifyRequest) error {
	u, key, err := NormalizeCallbackURL(req.URL)
	if err != nil {
		return err
	}
	if !visibleASCII(req.SubscriptionID, MaxEventIDBytes) {
		return errInvalidSubscriptionID
	}
	keys := ActiveKeys(req.Secrets, v.now())
	if len(keys) == 0 {
		return InvalidParams(FieldDeliverySecret, ReasonInvalidSecret)
	}

	ok, err := v.store.Verified(ctx, req.TenantID, req.Principal, key)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return v.coalesce(ctx, &pending{req: req, url: u, key: key, keys: keys})
}

// pending is one uncached verification.
type pending struct {
	req  VerifyRequest
	url  *url.URL
	key  string // the normalized URL
	keys [][]byte
}

// errFlightAborted is a flight's outcome until its leader returns; waiters
// see it only if the leader panicked, and go again.
var errFlightAborted = errors.New("mcpevents: verification aborted")

// coalesce runs p as the leader of the flight of its (tenant, principal,
// url), or waits for the flight in progress and returns its outcome when it
// holds for p too. Otherwise p goes again, after it.
func (v *Verifier) coalesce(ctx context.Context, p *pending) error {
	id := hashParts(p.req.TenantID, p.req.Principal, p.key)
	digest := keysDigest(p.keys)
	for {
		v.mu.Lock()
		f, waiting := v.flights[id]
		if !waiting {
			f = &flight{done: make(chan struct{}), keys: digest, err: errFlightAborted}
			v.flights[id] = f
		}
		v.mu.Unlock()

		if !waiting {
			return v.lead(ctx, id, f, p)
		}
		select {
		case <-f.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if f.sharedWith(digest) {
			return f.err
		}
	}
}

// sharedWith reports whether the flight's outcome holds for a waiter signing
// with keys: a pass covers every secret, like the cache; a challenge the host
// never answered doesn't depend on the secret; and the same keys fare the
// same. Anything else (an answer that may reject only the leader's secret,
// the leader's caller giving up, a store failure) isn't shared.
func (f *flight) sharedWith(keys string) bool {
	if f.err == nil {
		return true
	}
	var e *Error
	if !errors.As(f.err, &e) {
		return false
	}
	if f.keys == keys {
		return true
	}
	reason, _ := e.Data["reason"].(string)
	return e.Kind == KindCallbackEndpointError && unanswered(reason)
}

// keysDigest identifies a set of signing keys.
func keysDigest(keys [][]byte) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = string(k)
	}
	return hashParts(parts...)
}

// unanswered reports the challenge failures where the host never answered.
func unanswered(reason string) bool {
	switch reason {
	case ReasonTimeout, ReasonConnectionRefused, ReasonTLSError:
		return true
	}
	return false
}

// lead runs flight f for p: it takes the store's claim on (tenant,
// principal, url), so one replica at a time challenges it, re-checks the
// cache and sends the challenge.
func (v *Verifier) lead(ctx context.Context, id string, f *flight, p *pending) error {
	defer func() {
		v.mu.Lock()
		delete(v.flights, id)
		v.mu.Unlock()
		close(f.done)
	}()
	release, verified, err := v.claim(ctx, p)
	if err != nil || verified {
		f.err = err
		return err
	}
	defer release()
	f.err = v.verify(ctx, p)
	return f.err
}

// claim takes the store's claim on p's (tenant, principal, url), waiting
// while another replica holds it, and reports verified instead when the
// cache shows a pass by then.
func (v *Verifier) claim(ctx context.Context, p *pending) (release func(), verified bool, err error) {
	// Long enough for one verify: both slot waits and the challenge.
	ttl := v.inFlightWait + v.hostWait + v.timeout + claimSlack
	for {
		release, ok, err := v.store.ClaimVerification(ctx, p.req.TenantID, p.req.Principal, p.key, ttl)
		if err != nil {
			return nil, false, err
		}
		if ok {
			// A challenge may have passed since the first look.
			verified, err := v.store.Verified(ctx, p.req.TenantID, p.req.Principal, p.key)
			if err != nil || verified {
				release()
				return nil, verified, err
			}
			return release, false, nil
		}
		timer := time.NewTimer(v.claimPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-timer.C:
		}
		verified, err := v.store.Verified(ctx, p.req.TenantID, p.req.Principal, p.key)
		if err != nil || verified {
			return nil, verified, err
		}
	}
}

// verify sends p's challenge within the verification limits and caches a
// pass.
func (v *Verifier) verify(ctx context.Context, p *pending) error {
	req := p.req
	now := v.now()
	window, retryAfter := rateWindowAt(now)
	if v.rateLimit > 0 {
		n, err := v.store.CountAttempt(ctx, req.TenantID, req.Principal, window)
		if err != nil {
			return err
		}
		if n > int64(v.rateLimit) {
			return ResourceExhausted(LimitVerificationRate, v.rateLimit, retryAfter)
		}
	}
	hostport := HostPort(p.url)
	budgeted := v.failureLimit > 0 && (v.exempt == nil || !v.exempt(p.url.Hostname()))
	if budgeted {
		tenant, unansweredHost, err := v.store.FailureCounts(ctx, req.TenantID, hostport, window)
		if err != nil {
			return err
		}
		if tenant >= int64(v.failureLimit) {
			return ResourceExhausted(LimitVerificationRate, 0, retryAfter)
		}
		if unansweredHost >= v.hostFailureLimit() {
			return ResourceExhausted(LimitCallbackHostBusy, 0, retryAfter)
		}
	}

	release, err := v.acquire(ctx, hostport)
	if err != nil {
		return err
	}
	reason := v.challenge(ctx, p.url, req.SubscriptionID, p.keys, now)
	release()

	if ctxErr := ctx.Err(); ctxErr != nil {
		// The caller gave up (or its deadline passed first): not the
		// endpoint's failure.
		return ctxErr
	}
	log := v.logger.With(zap.String("tenant_id", req.TenantID), zap.String("subscription_id", req.SubscriptionID), zap.String("host", hostport))
	if reason == "" {
		if err := v.store.MarkVerified(ctx, req.TenantID, req.Principal, p.key, v.ttl); err != nil {
			// The challenge passed; the next refresh just verifies again.
			log.Warn("mcp verification passed but could not be cached", zap.Error(err))
		}
		log.Info("mcp callback verified")
		return nil
	}
	if budgeted {
		if err := v.store.CountFailure(ctx, req.TenantID, hostport, window, unanswered(reason)); err != nil {
			log.Warn("mcp verification failure not counted", zap.Error(err))
		}
	}
	log.Info("mcp callback verification failed", zap.String("reason", reason))
	return CallbackEndpointError(reason)
}

// hostFailureLimit is the deployment-wide budget of unanswered challenges per
// host:port and minute.
func (v *Verifier) hostFailureLimit() int64 {
	if int64(v.failureLimit) > math.MaxInt64/HostFailureFactor {
		return math.MaxInt64
	}
	return int64(v.failureLimit) * HostFailureFactor
}

// rateWindowAt returns the Unix-minute window of t and the time left in it.
func rateWindowAt(t time.Time) (int64, time.Duration) {
	start := t.Truncate(rateWindow)
	return start.Unix() / int64(rateWindow/time.Second), start.Add(rateWindow).Sub(t)
}

// acquire takes a process-wide in-flight slot and a host slot, waiting
// briefly for each. Neither exhausted case is the endpoint's failure: a busy
// process is verification_rate, and a host whose slots deliveries or other
// challenges hold (MCP_MAX_INFLIGHT_PER_HOST) is callback_host_busy.
func (v *Verifier) acquire(ctx context.Context, hostport string) (func(), error) {
	timer := time.NewTimer(v.inFlightWait)
	defer timer.Stop()
	select {
	case v.inFlight <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ResourceExhausted(LimitVerificationRate, 0, time.Second)
	}
	releaseHost := func() {}
	if v.hostLimiter != nil {
		hostCtx, cancel := context.WithTimeout(ctx, v.hostWait)
		r, err := v.hostLimiter.Acquire(hostCtx, hostport)
		cancel()
		if err != nil {
			<-v.inFlight
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, ResourceExhausted(LimitCallbackHostBusy, 0, time.Second)
		}
		releaseHost = r
	}
	return func() {
		releaseHost()
		<-v.inFlight
	}, nil
}

// NewChallenge returns a fresh challenge: 24 random bytes, base64url.
func NewChallenge() string {
	var b [24]byte
	_, _ = rand.Read(b[:]) // never fails
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// challenge sends one verification envelope and returns "" on a matching
// echo, else the failure category. Nothing from the response is kept.
func (v *Verifier) challenge(ctx context.Context, u *url.URL, subscriptionID string, keys [][]byte, now time.Time) string {
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	challenge := NewChallenge()
	body := VerificationEnvelope(challenge)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return ReasonConnectionRefused
	}
	if err := SetHeaders(req.Header, MessageID(EnvelopeVerification), subscriptionID, now, body, keys); err != nil {
		return ReasonConnectionRefused
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return ClassifyRequestError(err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 500:
		return ReasonHTTP5xx
	case resp.StatusCode >= 400:
		return ReasonHTTP4xx
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		// A redirect is never followed; the endpoint didn't answer.
		return ReasonChallengeFailed
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxChallengeResponseBytes+1))
	if err != nil {
		return ClassifyRequestError(err)
	}
	if len(raw) > MaxChallengeResponseBytes {
		return ReasonChallengeFailed
	}
	if !challengeEchoed(raw, challenge) {
		return ReasonChallengeFailed
	}
	return ""
}

// challengeEchoed reports whether raw is a JSON object whose "challenge"
// member (exact key, no duplicates) equals challenge, compared in constant
// time. A body that is our own verification envelope reflected back is
// rejected: an endpoint that echoes any request body (a JSON store, a
// debugging echo) hasn't checked the signature and hasn't consented.
func challengeEchoed(raw []byte, challenge string) bool {
	v, err := decodeStrict(raw, 32)
	if err != nil {
		return false
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if typ, ok := obj["type"].(string); ok && typ == EnvelopeVerification {
		return false
	}
	got, ok := obj["challenge"].(string)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// ClassifyRequestError maps an HTTP client error to a lastError category:
// timeout, tls_error, or connection_refused for everything else (refused,
// reset, DNS, unreachable, and the address guard's rejections, whose details
// must not reach the client).
func ClassifyRequestError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ReasonTimeout
	}
	if isTLSError(err) {
		return ReasonTLSError
	}
	return ReasonConnectionRefused
}

func isTLSError(err error) bool {
	var (
		verifyErr    *tls.CertificateVerificationError
		recordErr    tls.RecordHeaderError
		alertErr     tls.AlertError
		unknownCA    x509.UnknownAuthorityError
		hostnameErr  x509.HostnameError
		invalidCert  x509.CertificateInvalidError
		systemRoots  x509.SystemRootsError
		constraint   x509.ConstraintViolationError
		echRejection *tls.ECHRejectionError
	)
	switch {
	case errors.As(err, &verifyErr), errors.As(err, &recordErr), errors.As(err, &alertErr),
		errors.As(err, &unknownCA), errors.As(err, &hostnameErr), errors.As(err, &invalidCert),
		errors.As(err, &systemRoots), errors.As(err, &constraint), errors.As(err, &echRejection):
		return true
	}
	// Handshake failures reported as plain errors, including net/http's
	// rewrite of a plain-HTTP answer to a TLS hello.
	msg := err.Error()
	return strings.Contains(msg, "tls: ") || strings.Contains(msg, "x509: ") ||
		strings.Contains(msg, "server gave HTTP response to HTTPS client")
}
