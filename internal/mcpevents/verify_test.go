package mcpevents

import (
	"context"
	"encoding/base64"
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
	"testing"
	"time"

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
	requireMCPError(t, f.v.Verify(context.Background(), req), KindInvalidParams, map[string]any{"field": FieldDeliverySecret, "reason": ReasonInvalidSecret})
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
	b, e := mcpErr.MarshalJSON()
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
		assert.Equal(t, FieldDeliveryURL, mcpErr.Data["field"])
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

func TestVerifier_Cache(t *testing.T) {
	t.Parallel()
	mr, client := newMiniredis(t)
	clock := newFakeClock()
	v, err := NewVerifier(VerifierConfig{
		Client: guardedLikeClient(t),
		Store:  NewRedisVerificationStore(client, ""),
		TTL:    time.Hour,
		Now:    clock.Now,
	})
	require.NoError(t, err)
	_, key := secretOf(32, base64.StdEncoding)
	rc := newReceiver(t, key, echo)
	ctx := context.Background()
	req := VerifyRequest{TenantID: "t1", Principal: "p1", URL: rc.URL + "/hook", SubscriptionID: "sub_1", Secrets: []Secret{{Key: key}}}

	require.NoError(t, v.Verify(ctx, req))
	require.Len(t, rc.Requests(), 1)

	// Hit: same (tenant, principal, url), whatever the arguments or spelling.
	again := req
	again.SubscriptionID = "sub_2"
	again.URL = strings.Replace(rc.URL, "127.0.0.1:", "127.0.0.1.:000", 1) + "/x/../hook"
	require.NoError(t, v.Verify(ctx, again))
	assert.Len(t, rc.Requests(), 1)

	// Miss: another principal, tenant or URL.
	for _, other := range []func(r *VerifyRequest){
		func(r *VerifyRequest) { r.Principal = "p2" },
		func(r *VerifyRequest) { r.TenantID = "t2" },
		func(r *VerifyRequest) { r.URL = rc.URL + "/other" },
	} {
		r := req
		other(&r)
		require.NoError(t, v.Verify(ctx, r))
	}
	assert.Len(t, rc.Requests(), 4)

	// Expiry.
	mr.FastForward(time.Hour + time.Second)
	require.NoError(t, v.Verify(ctx, req))
	assert.Len(t, rc.Requests(), 5)
}

func TestVerifier_PrincipalRateLimit(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) {
		c.RateLimit = 3
		c.FailureLimit = -1
	})
	rc := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) { w.WriteHeader(http.StatusInternalServerError) })
	ctx := context.Background()

	// Each uncached attempt counts, whatever the URL.
	for i := range 3 {
		req := f.request(rc.URL + "/" + string(rune('a'+i)))
		requireReason(t, f.v.Verify(ctx, req), ReasonHTTP5xx)
	}
	err := f.v.Verify(ctx, f.request(rc.URL+"/d"))
	var mcpErr *Error
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, KindResourceExhausted, mcpErr.Kind)
	assert.Equal(t, LimitVerificationRate, mcpErr.Data["limit"])
	assert.Equal(t, 3, mcpErr.Data["max"])
	assert.Equal(t, int64(30000), mcpErr.Data["retryAfterMs"], "until the minute ends")
	assert.Len(t, rc.Requests(), 3, "the limited attempt sends nothing")

	// Another principal or tenant has its own budget.
	other := f.request(rc.URL)
	other.Principal = "user_2"
	requireReason(t, f.v.Verify(ctx, other), ReasonHTTP5xx)
	other = f.request(rc.URL)
	other.TenantID = "tenant_2"
	requireReason(t, f.v.Verify(ctx, other), ReasonHTTP5xx)

	// The next minute starts fresh.
	f.clock.Advance(time.Minute)
	requireReason(t, f.v.Verify(ctx, f.request(rc.URL)), ReasonHTTP5xx)
}

func TestVerifier_CacheHitsDontCount(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) { c.RateLimit = 1 })
	rc := newReceiver(t, f.key, echo)
	for range 5 {
		require.NoError(t, f.v.Verify(context.Background(), f.request(rc.URL)))
	}
	assert.Len(t, rc.Requests(), 1)
}

func TestVerifier_HostFailureBudget(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) {
		c.RateLimit = -1
		c.FailureLimit = 2
	})
	failing := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) { w.WriteHeader(http.StatusNotFound) })
	healthy := newReceiver(t, f.key, echo)
	ctx := context.Background()

	for i := range 2 {
		req := f.request(failing.URL)
		req.Principal = "attacker_" + string(rune('a'+i))
		requireReason(t, f.v.Verify(ctx, req), ReasonHTTP4xx)
	}
	// Spelling variants of the same host:port share the bucket.
	_, port, _ := net.SplitHostPort(hostOf(failing.URL))
	req := f.request("http://127.0.0.1.:00" + port + "/x")
	req.Principal = "victim"
	err := f.v.Verify(ctx, req)
	var mcpErr *Error
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, KindResourceExhausted, mcpErr.Kind)
	assert.Equal(t, LimitVerificationRate, mcpErr.Data["limit"])
	assert.NotContains(t, mcpErr.Data, "max")
	assert.Len(t, failing.Requests(), 2, "an exhausted host is not contacted")

	// A host that answers is never limited by its successes.
	for i := range 5 {
		req := f.request(healthy.URL)
		req.Principal = "user_" + string(rune('a'+i))
		require.NoError(t, f.v.Verify(ctx, req))
	}

	// The budget is per minute.
	f.clock.Advance(time.Minute)
	requireReason(t, f.v.Verify(ctx, req), ReasonHTTP4xx)
}

func TestVerifier_ExemptHost(t *testing.T) {
	t.Parallel()
	var asked []string
	var mu sync.Mutex
	f := newVerifierFixture(t, func(c *VerifierConfig) {
		c.RateLimit = -1
		c.FailureLimit = 1
		c.Exempt = func(host string) bool {
			mu.Lock()
			defer mu.Unlock()
			asked = append(asked, host)
			return host == "127.0.0.1"
		}
	})
	rc := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) { w.WriteHeader(http.StatusServiceUnavailable) })
	for range 4 {
		requireReason(t, f.v.Verify(context.Background(), f.request(rc.URL)), ReasonHTTP5xx)
	}
	assert.Len(t, rc.Requests(), 4)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "127.0.0.1", asked[0], "the hook gets the bare hostname")
}

func TestVerifier_InFlightCap(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) { c.MaxInFlight = 1 })
	f.v.inFlightWait = 50 * time.Millisecond
	entered := make(chan struct{})
	release := make(chan struct{})
	rc := newReceiver(t, f.key, func(w http.ResponseWriter, r receivedRequest) {
		close(entered)
		<-release
		echo(w, r)
	})
	done := make(chan error, 1)
	go func() { done <- f.v.Verify(context.Background(), f.request(rc.URL+"/1")) }()
	<-entered

	err := f.v.Verify(context.Background(), f.request(rc.URL+"/2"))
	requireMCPError(t, err, KindResourceExhausted, map[string]any{"limit": LimitVerificationRate, "retryAfterMs": int64(1000)})

	// A canceled caller doesn't wait for the slot.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, f.v.Verify(ctx, f.request(rc.URL+"/3")), context.Canceled)

	close(release)
	require.NoError(t, <-done)
	assert.Len(t, rc.Requests(), 1)
}

func TestVerifier_HostLimiter(t *testing.T) {
	t.Parallel()
	limiter := newCountingLimiter(1)
	f := newVerifierFixture(t, func(c *VerifierConfig) { c.HostLimiter = limiter })
	rc := newReceiver(t, f.key, echo)

	require.NoError(t, f.v.Verify(context.Background(), f.request(rc.URL)))
	acquired, released, _ := limiter.counts()
	assert.Equal(t, 1, acquired)
	assert.Equal(t, 1, released)

	// Host at its limit (a delivery holds the slot).
	hold, ok := limiter.TryAcquire(HostPort(mustURL(t, rc.URL)))
	require.True(t, ok)
	err := f.v.Verify(context.Background(), f.request(rc.URL+"/other"))
	requireMCPError(t, err, KindResourceExhausted, map[string]any{"limit": LimitVerificationRate, "retryAfterMs": int64(1000)})
	hold()
	assert.Len(t, rc.Requests(), 1)
	// The process-wide slot was returned too.
	assert.Empty(t, f.v.inFlight)
}

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
	n, err := f.store.HostFailures(context.Background(), HostPort(mustURL(t, rc.URL)), f.clock.Now().Unix()/60)
	require.NoError(t, err)
	assert.Zero(t, n)
}

// failingStore fails every call.
type failingStore struct{ VerificationStore }

var errStore = errors.New("store down")

func (failingStore) Verified(context.Context, string, string, string) (bool, error) {
	return false, errStore
}

func TestVerifier_StoreErrors(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	rc := newReceiver(t, key, echo)
	v, err := NewVerifier(VerifierConfig{Client: guardedLikeClient(t), Store: failingStore{}})
	require.NoError(t, err)
	err = v.Verify(context.Background(), VerifyRequest{TenantID: "t", Principal: "p", URL: rc.URL, SubscriptionID: "sub", Secrets: []Secret{{Key: key}}})
	assert.ErrorIs(t, err, errStore)
	var mcpErr *Error
	assert.False(t, errors.As(err, &mcpErr), "infrastructure errors are not MCP errors")
	assert.Empty(t, rc.Requests())

	_, err = NewVerifier(VerifierConfig{Store: failingStore{}})
	assert.Error(t, err)
	_, err = NewVerifier(VerifierConfig{Client: http.DefaultClient})
	assert.Error(t, err)
}

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

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
