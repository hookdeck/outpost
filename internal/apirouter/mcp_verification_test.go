package apirouter_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// fakeVerificationStore caches nothing and counts nothing; its reads fail
// with the errors set, as during a Redis outage.
type fakeVerificationStore struct {
	verifiedErr, countErr error
}

func (s fakeVerificationStore) Verified(context.Context, string, string, string) (bool, error) {
	return false, s.verifiedErr
}

func (s fakeVerificationStore) MarkVerified(context.Context, string, string, string, time.Duration) error {
	return nil
}

func (s fakeVerificationStore) ClaimVerification(context.Context, string, string, string, time.Duration) (func(), bool, error) {
	return func() {}, true, nil
}

func (s fakeVerificationStore) CountAttempt(context.Context, string, string, int64) (int64, error) {
	return 1, s.countErr
}

func (s fakeVerificationStore) FailureCounts(context.Context, string, string, int64) (int64, int64, error) {
	return 0, 0, nil
}

func (s fakeVerificationStore) CountFailure(context.Context, string, string, int64, bool) error {
	return nil
}

// publicResolver resolves every host to a public address, so callbacks pass
// the address check.
type publicResolver struct{}

func (publicResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

// receiverFunc answers verification challenges in place of the network.
type receiverFunc func(*http.Request) (*http.Response, error)

func (f receiverFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newDestMCPTest is newMCPTest with the real mcp provider, verifying
// callbacks with a real mcpevents.Verifier over store and receiver. A nil
// store leaves the provider without a verifier.
func newDestMCPTest(t *testing.T, store mcpevents.VerificationStore, receiver http.RoundTripper, opts ...mcpTestOption) *mcpTest {
	t.Helper()
	catalog := mcpCatalog(t)
	cfg := destmcp.Config{Catalog: catalog, Guard: &netguard.Guard{Resolver: publicResolver{}}}
	if store != nil {
		verifier, err := mcpevents.NewVerifier(mcpevents.VerifierConfig{Client: &http.Client{Transport: receiver}, Store: store})
		require.NoError(t, err)
		cfg.Verifier = verifier
	}
	provider, err := destmcp.New(metadata.NewMetadataLoader(""), cfg)
	require.NoError(t, err)
	opts = append([]mcpTestOption{withMCPCatalog(catalog), withMCPAPIOptions(withDestRegistry(newMCPRegistry(t, provider)))}, opts...)
	return newMCPTest(t, opts...)
}

// Callback verification that Outpost couldn't run is a server error, never
// an mcp_error blaming the client's callback URL: the client retries.
func TestMCP_Subscribe_VerificationUnavailable(t *testing.T) {
	redisDown := errors.New("dial tcp 10.0.0.5:6379: connect: connection refused")
	notFound := receiverFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	hang := receiverFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})

	t.Run("a verification store outage is 503", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		m := newDestMCPTest(t, fakeVerificationStore{verifiedErr: redisDown}, notFound,
			withMCPAPIOptions(withLogger(logging.NewTestLogger(zap.New(core)))))
		resp := m.subscribe(subscribeBody("p1", "order.created"))
		testutil.RequireErrorResponse(t, resp, http.StatusServiceUnavailable, "service unavailable")
		assert.Nil(t, m.destination(subscriptionID(t, subscribeBody("p1", "order.created"))))

		entries := logs.FilterMessage("request completed").All()
		require.Len(t, entries, 1)
		assert.Equal(t, zap.ErrorLevel, entries[0].Level, "logged as a server error")
		assert.Equal(t, int64(http.StatusServiceUnavailable), entries[0].ContextMap()["status"])
		assert.Contains(t, entries[0].ContextMap()["error"], "connection refused", "the log has the cause")
	})

	t.Run("a failed rate-limit count is 503", func(t *testing.T) {
		m := newDestMCPTest(t, fakeVerificationStore{countErr: errors.New("READONLY You can't write against a read only replica.")}, notFound)
		resp := m.subscribe(subscribeBody("p1", "order.created"))
		testutil.RequireErrorResponse(t, resp, http.StatusServiceUnavailable, "service unavailable")
	})

	t.Run("no verifier is 503", func(t *testing.T) {
		m := newDestMCPTest(t, nil, nil)
		resp := m.subscribe(subscribeBody("p1", "order.created"))
		testutil.RequireErrorResponse(t, resp, http.StatusServiceUnavailable, "service unavailable")
	})

	t.Run("the request's deadline passing during the challenge is 503", func(t *testing.T) {
		m := newDestMCPTest(t, fakeVerificationStore{}, hang)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		req := m.withAPIKey(m.jsonReq(http.MethodPut, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions", subscribeBody("p1", "order.created")))
		resp := m.do(req.WithContext(ctx))
		testutil.RequireErrorResponse(t, resp, http.StatusServiceUnavailable, "service unavailable")
	})

	t.Run("a challenge the receiver fails is the client's", func(t *testing.T) {
		m := newDestMCPTest(t, fakeVerificationStore{}, notFound)
		resp := m.subscribe(subscribeBody("p1", "order.created"))
		requireMCPError(t, resp, "callback_endpoint_error", -32015, map[string]any{"reason": "http_4xx"})
	})
}
