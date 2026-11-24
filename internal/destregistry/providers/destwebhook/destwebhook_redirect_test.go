package destwebhook_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/netguard/netguardtest"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func publishTo(t *testing.T, url string, opts ...destwebhook.Option) (*destregistry.Delivery, error) {
	t.Helper()
	provider := NewTestProvider(t, opts...)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": url}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "test-secret"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
	)
	return publisher.Publish(context.Background(), &event)
}

func TestWebhookPublisher_FollowRedirects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		redirectCode int
		targetCode   int
		wantStatus   string
		wantMethod   string
		wantBody     bool
	}{
		{http.StatusFound, http.StatusOK, "success", http.MethodGet, false},
		{http.StatusMovedPermanently, http.StatusOK, "success", http.MethodGet, false},
		{http.StatusSeeOther, http.StatusOK, "success", http.MethodGet, false},
		{http.StatusTemporaryRedirect, http.StatusOK, "success", http.MethodPost, true},
		{http.StatusPermanentRedirect, http.StatusOK, "success", http.MethodPost, true},
		{http.StatusFound, http.StatusInternalServerError, "failed", http.MethodGet, false},
	} {
		t.Run(fmt.Sprintf("%d_to_%d", tc.redirectCode, tc.targetCode), func(t *testing.T) {
			t.Parallel()

			var targetHits atomic.Int32
			var gotMethod, gotBody, gotSignature string
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetHits.Add(1)
				body, _ := io.ReadAll(r.Body)
				gotMethod, gotBody = r.Method, string(body)
				gotSignature = r.Header.Get("x-outpost-signature")
				w.WriteHeader(tc.targetCode)
			}))
			defer target.Close()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/final", tc.redirectCode)
			}))
			defer server.Close()

			delivery, err := publishTo(t, server.URL+"/webhook", destwebhook.WithFollowRedirects(true))

			require.NotNil(t, delivery)
			assert.Equal(t, tc.wantStatus, delivery.Status)
			assert.Equal(t, fmt.Sprintf("%d", tc.targetCode), delivery.Code)
			if tc.wantStatus == "success" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, int32(1), targetHits.Load())
			assert.Equal(t, tc.wantMethod, gotMethod)
			if tc.wantBody {
				assert.JSONEq(t, `{"key":"value"}`, gotBody)
				assert.NotEmpty(t, gotSignature)
			} else {
				assert.Empty(t, gotBody)
			}
		})
	}
}

func TestWebhookPublisher_FollowRedirectsOffByDefault(t *testing.T) {
	t.Parallel()

	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()

	delivery, err := publishTo(t, server.URL, destwebhook.WithFollowRedirects(false))

	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "failed", delivery.Status)
	assert.Equal(t, "302", delivery.Code)
	assert.Zero(t, targetHits.Load())
}

func TestWebhookPublisher_FollowRedirectsStopsAfterTen(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer server.Close()

	delivery, err := publishTo(t, server.URL, destwebhook.WithFollowRedirects(true))

	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "failed", delivery.Status)
	assert.Equal(t, int32(10), hits.Load())
}

// The address guard sits in the transport, so redirect targets are checked
// too when redirects are followed.
func TestWebhookPublisher_FollowRedirectsToPrivateAddressRefused(t *testing.T) {
	t.Parallel()

	var landed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		landed.Add(1)
	}))
	defer target.Close()
	_, targetPort, _ := net.SplitHostPort(target.Listener.Addr().String())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://internal.example:"+targetPort+"/landed", http.StatusFound)
	}))
	defer server.Close()

	// Only the first hop's address is allowlisted.
	allowlist, err := netguard.ParseAllowlist([]string{"127.0.0.1/32"})
	require.NoError(t, err)
	res := netguardtest.NewDNS(t).Set("internal.example", []string{"127.0.0.2"}).Resolver
	guard := &netguard.Guard{Resolver: res, Allowlist: allowlist}

	delivery, err := publishTo(t, server.URL, destwebhook.WithFollowRedirects(true), destwebhook.WithAddressGuard(guard))

	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "failed", delivery.Status)
	assert.Equal(t, "address_not_allowed", delivery.Code)
	assert.Zero(t, landed.Load())
}
