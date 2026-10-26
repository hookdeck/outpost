package mcpevents

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// A shared receiver: one host:port, answering 404 on
// unknown paths and the challenge on /hook.
func sharedReceiver(t *testing.T, key []byte) *receiver {
	return newReceiver(t, key, func(w http.ResponseWriter, r receivedRequest) {
		if r.Header.Get("X-MCP-Subscription-Id") == "sub_unknown" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		echo(w, r)
	})
}

// MCP_VERIFICATION_FAILURE_LIMIT is a budget per (tenant, host:port): failed
// challenges in one tenant never block another tenant's challenges to the
// same host.
func TestVerifier_TenantHostFailureBudget(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, func(c *VerifierConfig) {
		c.RateLimit = -1
		c.FailureLimit = 2
	})
	rc := sharedReceiver(t, f.key)
	ctx := context.Background()
	failing := func(tenantID, principal, rawURL string) VerifyRequest {
		req := f.request(rawURL)
		req.TenantID, req.Principal, req.SubscriptionID = tenantID, principal, "sub_unknown"
		return req
	}

	// Two principals of tenant "evil" use up its budget for the host.
	for i := range 2 {
		requireReason(t, f.v.Verify(ctx, failing("evil", "attacker_"+strconv.Itoa(i), rc.URL+"/x"+strconv.Itoa(i))), ReasonHTTP4xx)
	}
	// Spelling variants of the same host:port share the budget.
	_, port, _ := net.SplitHostPort(hostOf(rc.URL))
	err := f.v.Verify(ctx, failing("evil", "attacker_2", "http://127.0.0.1.:00"+port+"/x"))
	requireMCPError(t, err, KindResourceExhausted, map[string]any{"limit": LimitVerificationRate, "retryAfterMs": int64(30000)})
	// Even a challenge that would pass: the budget counts failures, and is
	// spent.
	good := f.request(rc.URL + "/hook")
	good.TenantID = "evil"
	requireMCPError(t, f.v.Verify(ctx, good), KindResourceExhausted, map[string]any{"limit": LimitVerificationRate, "retryAfterMs": int64(30000)})
	assert.Len(t, rc.Requests(), 2, "an exhausted budget sends nothing")

	// Tenant "victim" verifies against the same host as usual.
	victim := f.request(rc.URL + "/hook")
	victim.TenantID = "victim"
	require.NoError(t, f.v.Verify(ctx, victim))
	assert.Len(t, rc.Requests(), 3)

	// Successes never use the budget.
	for i := range 5 {
		req := f.request(rc.URL + "/hook")
		req.TenantID, req.Principal = "victim", "user_"+strconv.Itoa(i)
		require.NoError(t, f.v.Verify(ctx, req))
	}

	// The budget is per minute.
	f.clock.Advance(time.Minute)
	requireReason(t, f.v.Verify(ctx, failing("evil", "attacker_3", rc.URL+"/y")), ReasonHTTP4xx)
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

func TestVerifier_PurgeTenant(t *testing.T) {
	t.Parallel()
	f := newVerifierFixture(t, nil)
	rc := newReceiver(t, f.key, echo)
	ctx := context.Background()

	req := f.request(rc.URL)
	require.NoError(t, f.v.Verify(ctx, req))
	other := f.request(rc.URL)
	other.TenantID = "tenant_2"
	require.NoError(t, f.v.Verify(ctx, other))
	assert.Len(t, rc.Requests(), 2)

	require.NoError(t, f.v.PurgeTenant(ctx, "tenant_1"))
	ok, err := f.v.Verified(ctx, "tenant_1", req.Principal, rc.URL)
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = f.v.Verified(ctx, "tenant_2", req.Principal, rc.URL)
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, f.v.Verify(ctx, req))
	assert.Len(t, rc.Requests(), 3, "a purged tenant is challenged again")
}

// failingStore fails every call.
type failingStore struct{ VerificationStore }

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
