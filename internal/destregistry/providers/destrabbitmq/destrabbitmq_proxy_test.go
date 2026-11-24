package destrabbitmq_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProxiedPublisher(t *testing.T, chain, serverURL, useTLS string) destregistry.Publisher {
	t.Helper()
	hops, err := proxychain.Parse(chain)
	require.NoError(t, err)
	provider, err := destrabbitmq.New(testutil.Registry.MetadataLoader(), nil, destrabbitmq.WithProxy(hops))
	require.NoError(t, err)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": serverURL,
			"exchange":   "test-exchange",
			"tls":        useTLS,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "guest",
			"password": "guest",
		}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })
	return publisher
}

func publishWithTimeout(t *testing.T, publisher destregistry.Publisher) (*destregistry.Delivery, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event := testutil.EventFactory.Any()
	return publisher.Publish(ctx, &event)
}

func TestRabbitMQPublisher_Proxy(t *testing.T) {
	t.Parallel()

	t.Run("amqp tunnels through the chain", func(t *testing.T) {
		t.Parallel()
		broker, got := proxychaintest.FirstBytes(t, 8)
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, false)
		publisher := newProxiedPublisher(t, hop0.URL+" "+hop1.URL, broker, "false")

		// The broker hangs up after the protocol header, so the publish fails.
		_, err := publishWithTimeout(t, publisher)
		require.Error(t, err)
		select {
		case b := <-got:
			assert.Equal(t, []byte("AMQP\x00\x00\x09\x01"), b)
		case <-time.After(5 * time.Second):
			t.Fatal("broker never received the AMQP protocol header")
		}
		assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects())
		assert.Equal(t, []string{broker}, hop1.Connects())
	})

	t.Run("amqps runs TLS end to end through the tunnel", func(t *testing.T) {
		t.Parallel()
		broker, got := proxychaintest.FirstBytes(t, 1)
		hop := proxychaintest.New(t, false)
		publisher := newProxiedPublisher(t, hop.URL, broker, "true")

		_, err := publishWithTimeout(t, publisher)
		require.Error(t, err)
		select {
		case b := <-got:
			assert.Equal(t, byte(0x16), b[0], "TLS handshake record")
		case <-time.After(5 * time.Second):
			t.Fatal("broker never received the TLS ClientHello")
		}
		assert.Equal(t, []string{broker}, hop.Connects())
	})

	t.Run("RBAC deny is a network_unreachable attempt", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) {
			return http.StatusForbidden, http.Header{"X-Envoy-Response-Code-Details": {"rbac_access_denied_matched_policy[none]"}}
		}
		publisher := newProxiedPublisher(t, hop.URL, "broker.example.com:5672", "false")

		delivery, err := publishWithTimeout(t, publisher)
		require.Error(t, err)
		require.NotNil(t, delivery)
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, "network_unreachable", delivery.Code)
	})

	t.Run("proxy auth rejected nacks", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		publisher := newProxiedPublisher(t, proxychaintest.WithCreds(hop.URL, "u", "topsecret"), "broker.example.com:5672", "false")

		delivery, err := publishWithTimeout(t, publisher)
		require.Error(t, err)
		assert.Nil(t, delivery)
		assert.NotContains(t, err.Error(), "topsecret")
	})
}

func TestRabbitMQPublisher_ProxyTimeoutHidesProxyAddress(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		broker func(t *testing.T) string
		hold   bool
	}{
		{name: "CONNECT never answered", broker: func(*testing.T) string { return "127.0.0.1:5672" }, hold: true},
		{name: "handshake stalls in the tunnel", broker: newSilentBroker},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hop := proxychaintest.New(t, false)
			if tt.hold {
				hop.Hold = make(chan struct{})
				t.Cleanup(func() { close(hop.Hold) })
			}
			publisher := newProxiedPublisher(t, hop.URL, tt.broker(t), "false")

			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			event := testutil.EventFactory.Any()
			delivery, err := publisher.Publish(ctx, &event)
			require.Error(t, err)
			require.NotNil(t, delivery)
			assert.Equal(t, "timeout", delivery.Code)
			assert.Equal(t, "timeout connecting to 127.0.0.1", delivery.Response["error"])
			assert.Contains(t, err.Error(), proxychaintest.HostOf(hop.URL), "operator error keeps the detail")
		})
	}
}

func TestRabbitMQPublisher_ProxyKeepsTLSErrors(t *testing.T) {
	t.Parallel()
	broker := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(broker.Close)
	hop := proxychaintest.New(t, false)
	publisher := newProxiedPublisher(t, hop.URL, proxychaintest.HostOf(broker.URL), "true")

	delivery, err := publishWithTimeout(t, publisher)
	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "tls_error", delivery.Code)
	assert.Contains(t, delivery.Response["error"], "x509")
}
