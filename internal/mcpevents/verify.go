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
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

// HostLimiter caps concurrent requests per callback host:port. It is shared
// by deliveries, verification challenges and terminated notifications, so one
// slow endpoint can't take every slot.
type HostLimiter interface {
	// TryAcquire takes a slot for hostport without blocking; ok is false
	// when the host is at its limit. release must be called once.
	TryAcquire(hostport string) (release func(), ok bool)
}

// Verifier defaults.
const (
	DefaultVerificationTTL          = 24 * time.Hour
	DefaultVerificationRateLimit    = 10  // uncached attempts per (tenant, principal) per minute
	DefaultVerificationFailureLimit = 120 // failed challenges per host per minute
	DefaultVerificationMaxInFlight  = 64  // concurrent challenges per process
	DefaultVerificationTimeout      = 10 * time.Second
	// MaxChallengeResponseBytes caps how much of a challenge response is read.
	MaxChallengeResponseBytes = 64 << 10

	inFlightWait = 2 * time.Second
	rateWindow   = time.Minute
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
	// FailureLimit caps failed or unanswered challenges per host:port per
	// minute (MCP_VERIFICATION_FAILURE_LIMIT). A host that answers its
	// challenges never uses it; one that doesn't stops being probed.
	FailureLimit int
	// Exempt reports hosts (the URL's hostname, IP literals unbracketed)
	// that skip the failure budget, such as allowlisted ones.
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
// principal, url).
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
	timeout      time.Duration
	logger       *zap.Logger
	now          func() time.Time
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
		timeout:      positiveOr(cfg.Timeout, DefaultVerificationTimeout),
		logger:       zapLogger(cfg.Logger),
		now:          cfg.Now,
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
// invalid_params (URL, no valid secret), resource_exhausted
// {limit: "verification_rate"} and callback_endpoint_error {reason} with
// reason one of http_5xx, http_4xx, challenge_failed, timeout, tls_error and
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
	now := v.now()
	keys := ActiveKeys(req.Secrets, now)
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
	hostport := HostPort(u)
	budgeted := v.failureLimit > 0 && (v.exempt == nil || !v.exempt(u.Hostname()))
	if budgeted {
		n, err := v.store.HostFailures(ctx, hostport, window)
		if err != nil {
			return err
		}
		if n >= int64(v.failureLimit) {
			return ResourceExhausted(LimitVerificationRate, 0, retryAfter)
		}
	}

	release, err := v.acquire(ctx, hostport)
	if err != nil {
		return err
	}
	reason := v.challenge(ctx, u, req.SubscriptionID, keys, now)
	release()

	if ctxErr := ctx.Err(); ctxErr != nil {
		// The caller gave up (or its deadline passed first): not the
		// endpoint's failure.
		return ctxErr
	}
	log := v.logger.With(zap.String("tenant_id", req.TenantID), zap.String("subscription_id", req.SubscriptionID), zap.String("host", hostport))
	if reason == "" {
		if err := v.store.MarkVerified(ctx, req.TenantID, req.Principal, key, v.ttl); err != nil {
			// The challenge passed; the next refresh just verifies again.
			log.Warn("mcp verification passed but could not be cached", zap.Error(err))
		}
		log.Info("mcp callback verified")
		return nil
	}
	if budgeted {
		if _, err := v.store.CountHostFailure(ctx, hostport, window); err != nil {
			log.Warn("mcp verification failure not counted", zap.Error(err))
		}
	}
	log.Info("mcp callback verification failed", zap.String("reason", reason))
	return CallbackEndpointError(reason)
}

// rateWindowAt returns the Unix-minute window of t and the time left in it.
func rateWindowAt(t time.Time) (int64, time.Duration) {
	start := t.Truncate(rateWindow)
	return start.Unix() / int64(rateWindow/time.Second), start.Add(rateWindow).Sub(t)
}

// acquire takes a process-wide in-flight slot (waiting briefly) and a host
// slot. Both exhausted cases are resource_exhausted: the endpoint did nothing
// wrong.
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
		r, ok := v.hostLimiter.TryAcquire(hostport)
		if !ok {
			<-v.inFlight
			return nil, ResourceExhausted(LimitVerificationRate, 0, time.Second)
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
