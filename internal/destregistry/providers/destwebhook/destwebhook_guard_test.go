package destwebhook_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/netguard/netguardtest"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func guardWith(t *testing.T, res *net.Resolver, allow ...string) *netguard.Guard {
	t.Helper()
	al, err := netguard.ParseAllowlist(allow)
	require.NoError(t, err)
	return &netguard.Guard{Resolver: res, Allowlist: al}
}

func webhookTo(rawURL string) models.Destination {
	return testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": rawURL}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "test-secret"}),
	)
}

func requireURLError(t *testing.T, err error, errType string) {
	t.Helper()
	var validationErr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &validationErr)
	require.Len(t, validationErr.Errors, 1)
	assert.Equal(t, "config.url", validationErr.Errors[0].Field)
	assert.Equal(t, errType, validationErr.Errors[0].Type)
}

func TestWebhookDestination_Validate_AddressGuard(t *testing.T) {
	t.Parallel()
	res := netguardtest.NewDNS(t).Set("public.example", []string{"8.8.8.8"}).Set("internal.example", []string{"10.0.0.1"}).Resolver
	guarded := NewTestProvider(t, destwebhook.WithAddressGuard(&netguard.Guard{Resolver: res}))

	for _, u := range []string{"http://10.0.0.1/", "http://127.0.0.1/", "http://[::1]/", "https://internal.example/hook", "https://unknown.example/hook"} {
		d := webhookTo(u)
		requireURLError(t, guarded.Validate(context.Background(), &d), "address_not_allowed")
	}
	// The schema rejects the second before the guard runs.
	for u, errType := range map[string]string{"http://[::1": "invalid", "http://": "pattern"} {
		d := webhookTo(u)
		requireURLError(t, guarded.Validate(context.Background(), &d), errType)
	}
	for _, u := range []string{"https://8.8.8.8/hook", "https://public.example/hook"} {
		d := webhookTo(u)
		assert.NoError(t, guarded.Validate(context.Background(), &d), u)
	}

	allowing := NewTestProvider(t, destwebhook.WithAddressGuard(guardWith(t, res, "10.0.0.0/8")))
	d := webhookTo("https://internal.example/hook")
	assert.NoError(t, allowing.Validate(context.Background(), &d))
}

func TestWebhookDestination_Validate_NoGuardAllowsPrivateAddresses(t *testing.T) {
	t.Parallel()
	d := webhookTo("http://127.0.0.1/")
	assert.NoError(t, NewTestProvider(t).Validate(context.Background(), &d))
}

func TestWebhookPublisher_AddressGuard(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	t.Cleanup(srv.Close)
	event := testutil.EventFactory.Any(testutil.EventFactory.WithDataMap(map[string]interface{}{"k": "v"}))

	publish := func(t *testing.T, provider *destwebhook.WebhookDestination, target string) (*destregistry.Delivery, error) {
		t.Helper()
		d := webhookTo(target)
		publisher, err := provider.CreatePublisher(context.Background(), &d)
		require.NoError(t, err)
		t.Cleanup(func() { _ = publisher.Close() })
		return publisher.Publish(context.Background(), &event)
	}

	t.Run("allowlisted address is delivered to", func(t *testing.T) {
		provider := NewTestProvider(t, destwebhook.WithAddressGuard(guardWith(t, nil, "127.0.0.1/32")))
		delivery, err := publish(t, provider, srv.URL)
		require.NoError(t, err)
		assert.Equal(t, "success", delivery.Status)
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("private address fails the attempt", func(t *testing.T) {
		hits.Store(0)
		provider := NewTestProvider(t, destwebhook.WithAddressGuard(&netguard.Guard{}))
		delivery, err := publish(t, provider, srv.URL)
		require.Error(t, err)
		var attemptErr *destregistry.ErrDestinationPublishAttempt
		require.True(t, errors.As(err, &attemptErr))
		require.NotNil(t, delivery)
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, "address_not_allowed", delivery.Code)
		assert.Zero(t, hits.Load())
	})

	t.Run("forward proxy: the target is checked before the request", func(t *testing.T) {
		var proxied atomic.Int32
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxied.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(proxy.Close)
		proxyURL, err := url.Parse(proxy.URL)
		require.NoError(t, err)
		res := netguardtest.NewDNS(t).Set("hooks.example", []string{"8.8.8.8"}).Resolver
		provider := NewTestProvider(t,
			destwebhook.WithProxy([]*url.URL{proxyURL}),
			destwebhook.WithAddressGuard(&netguard.Guard{Resolver: res}),
		)

		delivery, err := publish(t, provider, "http://10.0.0.1/hook")
		require.Error(t, err)
		require.NotNil(t, delivery)
		assert.Equal(t, "address_not_allowed", delivery.Code)
		assert.Zero(t, proxied.Load(), "the proxy saw nothing")

		delivery, err = publish(t, provider, "http://hooks.example/hook")
		require.Error(t, err)
		require.NotNil(t, delivery)
		assert.Equal(t, "502", delivery.Code)
		assert.EqualValues(t, 1, proxied.Load())
	})
}

func TestClassifyNetworkError_AddressNotAllowed(t *testing.T) {
	t.Parallel()
	err := &netguard.AddressNotAllowedError{Host: "internal.example", Reason: netguard.ReasonNotGlobal}
	assert.Equal(t, "address_not_allowed", destwebhook.ClassifyNetworkError(err))
	assert.Equal(t, "address_not_allowed", destwebhook.ClassifyNetworkError(&url.Error{Op: "Get", URL: "http://x", Err: err}))
}
