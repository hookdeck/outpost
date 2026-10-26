package mcpevents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type verifierFixture struct {
	v     *Verifier
	store *RedisVerificationStore
	clock *fakeClock
	key   []byte
}

func newVerifierFixture(t *testing.T, mutate func(*VerifierConfig)) *verifierFixture {
	t.Helper()
	_, client := newMiniredis(t)
	store := NewRedisVerificationStore(client, "dep")
	clock := newFakeClock()
	cfg := VerifierConfig{
		Client:  guardedLikeClient(t),
		Store:   store,
		Timeout: 2 * time.Second,
		Now:     clock.Now,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	v, err := NewVerifier(cfg)
	require.NoError(t, err)
	_, key := secretOf(32, base64.StdEncoding)
	return &verifierFixture{v: v, store: store, clock: clock, key: key}
}

func (f *verifierFixture) request(url string) VerifyRequest {
	return VerifyRequest{
		TenantID:       "tenant_1",
		Principal:      "user_1",
		URL:            url,
		SubscriptionID: "sub_0123456789abcdef0123456789abcdef",
		Secrets:        []Secret{{Key: f.key}},
	}
}

func requireReason(t *testing.T, err error, reason string) {
	t.Helper()
	requireMCPError(t, err, KindCallbackEndpointError, map[string]any{"reason": reason})
}

func TestVerifier_Success(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, nil)
	rc := newReceiver(t, f.key, echo)

	require.NoError(t, f.v.Verify(context.Background(), f.request(rc.URL+"/hook")))

	reqs := rc.Requests()
	require.Len(t, reqs, 1)
	r := reqs[0]
	assert.True(t, r.Verified, "signed like a delivery")
	assert.Regexp(t, regexp.MustCompile(`^msg_verification_[0-9a-f]{24}$`), r.Header.Get("webhook-id"))
	assert.Equal(t, "sub_0123456789abcdef0123456789abcdef", r.Header.Get("X-MCP-Subscription-Id"))
	assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
	assert.Equal(t, `{"type":"verification","challenge":"`+r.Challenge+`"}`, string(r.Body))
	raw, err := base64.RawURLEncoding.DecodeString(r.Challenge)
	require.NoError(t, err)
	assert.Len(t, raw, 24)
	assert.Equal(t, strconv.FormatInt(f.clock.Now().Unix(), 10), r.Header.Get("webhook-timestamp"))

	ok, err := f.v.Verified(context.Background(), "tenant_1", "user_1", rc.URL+"/hook")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerifier_DualSigned(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, nil)
	_, previous := secretOf(24, base64.StdEncoding)
	rc := newReceiver(t, previous, echo) // only knows the previous secret
	future := f.clock.Now().Add(time.Hour)
	req := f.request(rc.URL)
	req.Secrets = []Secret{{Key: f.key}, {Key: previous, InvalidAt: &future}}
	require.NoError(t, f.v.Verify(context.Background(), req))
	assert.Equal(t, 2, strings.Count(rc.Requests()[0].Header.Get("webhook-signature"), "v1,"))

	// No valid secret at all: nothing is sent.
	past := f.clock.Now().Add(-time.Second)
	req.Secrets = []Secret{{Key: previous, InvalidAt: &past}}
	requireMCPError(t, f.v.Verify(context.Background(), req), KindInvalidParams, map[string]any{"field": FieldSecret, "reason": ReasonInvalidSecret})
	assert.Len(t, rc.Requests(), 1)
}

func TestVerifier_Failures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		respond func(w http.ResponseWriter, r receivedRequest)
		reason  string
	}{
		{"wrong challenge", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `{"challenge":"`+strings.Repeat("A", len(r.Challenge))+`"}`)
		}, ReasonChallengeFailed},
		{"challenge prefix", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge[:10]+`"}`)
		}, ReasonChallengeFailed},
		{"empty 200", func(w http.ResponseWriter, r receivedRequest) {}, ReasonChallengeFailed},
		{"non-JSON", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, r.Challenge)
		}, ReasonChallengeFailed},
		{"JSON string", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `"`+r.Challenge+`"`)
		}, ReasonChallengeFailed},
		{"challenge not a string", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `{"challenge":["`+r.Challenge+`"]}`)
		}, ReasonChallengeFailed},
		{"key case differs", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `{"Challenge":"`+r.Challenge+`"}`)
		}, ReasonChallengeFailed},
		{"duplicate key", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`","challenge":"x"}`)
		}, ReasonChallengeFailed},
		{"trailing garbage", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`"} x`)
		}, ReasonChallengeFailed},
		{"reflected request body", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = w.Write(r.Body) // a JSON store or echo endpoint, no signature check
		}, ReasonChallengeFailed},
		{"204 without body", func(w http.ResponseWriter, r receivedRequest) {
			w.WriteHeader(http.StatusNoContent)
		}, ReasonChallengeFailed},
		{"3xx", func(w http.ResponseWriter, r receivedRequest) {
			w.Header().Set("Location", "http://169.254.169.254/latest/meta-data/")
			w.WriteHeader(http.StatusFound)
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`"}`)
		}, ReasonChallengeFailed},
		{"4xx", func(w http.ResponseWriter, r receivedRequest) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`"}`)
		}, ReasonHTTP4xx},
		{"410", func(w http.ResponseWriter, r receivedRequest) { w.WriteHeader(http.StatusGone) }, ReasonHTTP4xx},
		{"5xx", func(w http.ResponseWriter, r receivedRequest) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`"}`)
		}, ReasonHTTP5xx},
		{"huge body", func(w http.ResponseWriter, r receivedRequest) {
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`","pad":"`+strings.Repeat("x", MaxChallengeResponseBytes)+`"}`)
		}, ReasonChallengeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newVerifierFixture(t, nil)
			rc := newReceiver(t, f.key, tt.respond)
			verifyErr := f.v.Verify(context.Background(), f.request(rc.URL))
			requireReason(t, verifyErr, tt.reason)
			assert.Len(t, rc.Requests(), 1, "never follows redirects or retries")

			// Not cached.
			ok, err := f.v.Verified(context.Background(), "tenant_1", "user_1", rc.URL)
			require.NoError(t, err)
			assert.False(t, ok)

			// The error carries only the category.
			body, err := mcpErrorBody(verifyErr)
			require.NoError(t, err)
			assert.NotContains(t, body, "169.254")
			assert.NotContains(t, body, "127.0.0.1")
		})
	}
}

func mcpErrorBody(err error) (string, error) {
	var mcpErr *Error
	if !errors.As(err, &mcpErr) {
		return "", errors.New("not an *Error")
	}
	b, e := json.Marshal(map[string]any{"kind": mcpErr.Kind, "message": mcpErr.Message, "data": mcpErr.Data})
	return string(b), e
}

func TestVerifier_NeverFollowsRedirects(t *testing.T) {
	t.Parallel()
	// Even with a client that would follow them.
	f := newVerifierFixture(t, func(c *VerifierConfig) { c.Client = &http.Client{} })
	target := newReceiver(t, f.key, echo)
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		redirect := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
		})
		requireReason(t, f.v.Verify(context.Background(), f.request(redirect.URL)), ReasonChallengeFailed)
		assert.Len(t, redirect.Requests(), 1)
	}
	assert.Empty(t, target.Requests())
}

func TestVerifier_AcceptedEcho(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		f := newVerifierFixture(t, nil)
		rc := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) {
			w.WriteHeader(status)
			// Extra members are fine; so is a type other than verification.
			_, _ = io.WriteString(w, `{"type":"ack","challenge":"`+r.Challenge+`","ok":true}`)
		})
		assert.NoError(t, f.v.Verify(context.Background(), f.request(rc.URL)), "status %d", status)
	}
}

func TestVerifier_HugeBodyNotRead(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, nil)
	var written int64
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat("x", 32<<10))
		for range 1024 { // 32 MiB if anyone read it all
			n, err := w.Write(chunk)
			mu.Lock()
			written += int64(n)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	start := time.Now()
	requireReason(t, f.v.Verify(context.Background(), f.request(srv.URL)), ReasonChallengeFailed)
	assert.Less(t, time.Since(start), time.Second)
	srv.Close()
	mu.Lock()
	defer mu.Unlock()
	assert.Less(t, written, int64(8<<20), "the verifier stopped reading at the cap")
}

func TestVerifier_Timeout(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) { c.Timeout = 100 * time.Millisecond })
	release := make(chan struct{})
	rc := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) { <-release })
	start := time.Now()
	requireReason(t, f.v.Verify(context.Background(), f.request(rc.URL)), ReasonTimeout)
	assert.Less(t, time.Since(start), time.Second)

	// A body that trickles past the deadline is a timeout too.
	slowBody := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slowBody.Close)
	// Cleanups run last-in first-out: release the handlers before the
	// servers wait for them.
	t.Cleanup(func() { close(release) })
	requireReason(t, f.v.Verify(context.Background(), f.request(slowBody.URL)), ReasonTimeout)
}

func TestVerifier_TLSAndConnectionErrors(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, nil)

	// Untrusted certificate.
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(tlsSrv.Close)
	requireReason(t, f.v.Verify(context.Background(), f.request(tlsSrv.URL)), ReasonTLSError)

	// https to a plain-HTTP server.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(plain.Close)
	requireReason(t, f.v.Verify(context.Background(), f.request("https://"+hostOf(plain.URL))), ReasonTLSError)

	// Nothing listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	requireReason(t, f.v.Verify(context.Background(), f.request("http://"+addr)), ReasonConnectionRefused)

	// Connection reset mid-response.
	reset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.SetLinger(0)
			}
			_ = conn.Close()
		}
	}))
	t.Cleanup(reset.Close)
	requireReason(t, f.v.Verify(context.Background(), f.request(reset.URL)), ReasonConnectionRefused)
}

// A verification challenge spells its headers as deliveries do on the wire.
func TestVerifier_HeaderSpellingOnTheWire(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, nil)
	url, got := newRawReceiver(t, func(body []byte) string {
		var env struct {
			Challenge string `json:"challenge"`
		}
		_ = json.Unmarshal(body, &env)
		return `{"challenge":"` + env.Challenge + `"}`
	})

	require.NoError(t, f.v.Verify(context.Background(), f.request(url+"/hook")))
	req := receiveRaw(t, got)
	assert.Equal(t, []string{"Content-Type", "webhook-id", "webhook-timestamp", "webhook-signature", "X-MCP-Subscription-Id"},
		sortedLike(req.deliveryHeaderNames()))
	assert.Equal(t, "sub_0123456789abcdef0123456789abcdef", req.header.Get("X-MCP-Subscription-Id"))
	verifyWith(t, f.key, req.body, req.header, true)
}

// addressNotAllowedError stands in for the address guard's typed error.
type addressNotAllowedError struct{ ip string }

func (e *addressNotAllowedError) Error() string { return "address " + e.ip + " is not allowed" }

func TestVerifier_AddressGuardErrorIsConnectionRefused(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) {
		c.Client = &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, &addressNotAllowedError{ip: "10.0.0.1"}
			},
		}}
	})
	err := f.v.Verify(context.Background(), f.request("https://internal.example/hook"))
	requireReason(t, err, ReasonConnectionRefused)
	body, jerr := mcpErrorBody(err)
	require.NoError(t, jerr)
	assert.NotContains(t, body, "10.0.0.1")
	assert.NotContains(t, body, "internal.example")
}

func TestVerifier_InvalidRequest(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, nil)
	for _, u := range []string{"", "ftp://a.example/", "https://u:p@a.example/", "https://a.example/#f"} {
		err := f.v.Verify(context.Background(), f.request(u))
		var mcpErr *Error
		require.ErrorAs(t, err, &mcpErr, u)
		assert.Equal(t, KindInvalidParams, mcpErr.Kind)
		assert.Equal(t, FieldURL, mcpErr.Data["field"])
	}

	// A subscription ID that can't be a header value is a caller bug.
	rc := newReceiver(t, f.key, echo)
	for _, id := range []string{"", "sub_1\r\nX-Injected: 1", "sub 1"} {
		req := f.request(rc.URL)
		req.SubscriptionID = id
		require.ErrorIs(t, f.v.Verify(context.Background(), req), errInvalidSubscriptionID)
	}
	assert.Empty(t, rc.Requests())
}

// roundTripFunc is an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, _, err := NormalizeCallbackURL(raw)
	require.NoError(t, err)
	return u
}

func TestVerifier_CallerCanceled(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) { c.FailureLimit = 1 })
	ctx, cancel := context.WithCancel(context.Background())
	rc := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	assert.ErrorIs(t, f.v.Verify(ctx, f.request(rc.URL)), context.Canceled)

	// Not counted against the host.
	failures, err := f.store.FailureCount(context.Background(), "tenant_1", HostPort(mustURL(t, rc.URL)), f.clock.Now().Unix()/60)
	require.NoError(t, err)
	assert.Zero(t, failures)
}

var errStore = errors.New("store down")

func TestClassifyRequestError(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ReasonTimeout, ClassifyRequestError(context.DeadlineExceeded))
	assert.Equal(t, ReasonTimeout, ClassifyRequestError(&net.OpError{Op: "dial", Err: timeoutErr{}}))
	assert.Equal(t, ReasonTLSError, ClassifyRequestError(errors.New("remote error: tls: handshake failure")))
	assert.Equal(t, ReasonTLSError, ClassifyRequestError(errors.New("x509: certificate has expired")))
	assert.Equal(t, ReasonConnectionRefused, ClassifyRequestError(errors.New("dial tcp: lookup nope.invalid: no such host")))
	assert.Equal(t, ReasonConnectionRefused, ClassifyRequestError(&addressNotAllowedError{ip: "10.0.0.1"}))
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }

func (timeoutErr) Timeout() bool { return true }

func (timeoutErr) Temporary() bool { return true }

// echoAny answers the challenge when the signature verifies with any of
// keys, like a receiver holding per-subscription secrets not yet tied to an
// id, after delay. It records the peak number of challenges in flight.
type echoAny struct {
	keys    [][]byte
	delay   time.Duration
	current atomic.Int32
	peak    atomic.Int32
}

func (e *echoAny) respond(w http.ResponseWriter, r receivedRequest) {
	n := e.current.Add(1)
	defer e.current.Add(-1)
	for p := e.peak.Load(); n > p && !e.peak.CompareAndSwap(p, n); p = e.peak.Load() {
	}
	time.Sleep(e.delay)
	for _, k := range e.keys {
		wh, err := standardwebhooks.NewWebhookRaw(k)
		if err == nil && wh.VerifyIgnoringTimestamp(r.Body, r.Header) == nil {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`"}`)
			return
		}
	}
	http.Error(w, "bad signature", http.StatusUnauthorized)
}

func distinctKeys(n int) [][]byte {
	keys := make([][]byte, n)
	for i := range keys {
		keys[i] = []byte(strings.Repeat(string(rune('A'+i)), 32))
	}
	return keys
}

// verifyAll runs one Verify per request at once, request i on verifier(i),
// and returns the results in order.
func verifyAll(verifier func(int) *Verifier, reqs []VerifyRequest) []error {
	errs := make([]error, len(reqs))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Go(func() {
			<-start
			errs[i] = verifier(i).Verify(context.Background(), req)
		})
	}
	close(start)
	wg.Wait()
	return errs
}
