package destkafka_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProxiedKafkaPublisher(t *testing.T, chain, brokers, useTLS string) destregistry.Publisher {
	t.Helper()
	return newProxiedKafkaTopicPublisher(t, chain, brokers, "test-topic", useTLS)
}

func newProxiedKafkaTopicPublisher(t *testing.T, chain, brokers, topic, useTLS string) destregistry.Publisher {
	t.Helper()
	hops, err := proxychain.Parse(chain)
	require.NoError(t, err)
	provider, err := destkafka.New(testutil.Registry.MetadataLoader(), nil, destkafka.WithProxy(hops))
	require.NoError(t, err)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("kafka"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"brokers":        brokers,
			"topic":          topic,
			"sasl_mechanism": "plain",
			"tls":            useTLS,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "admin",
			"password": "admin-secret",
		}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })
	return publisher
}

func publishKafkaWithTimeout(t *testing.T, publisher destregistry.Publisher, timeout time.Duration) (*destregistry.Delivery, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	event := testutil.EventFactory.Any(testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}))
	return publisher.Publish(ctx, &event)
}

func TestKafkaPublisher_Proxy(t *testing.T) {
	t.Parallel()

	t.Run("tunnels through the chain", func(t *testing.T) {
		t.Parallel()
		broker, got := proxychaintest.FirstBytes(t, 4)
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, false)
		publisher := newProxiedKafkaPublisher(t, hop0.URL+" "+hop1.URL, broker, "false")

		_, err := publishKafkaWithTimeout(t, publisher, time.Second)
		require.Error(t, err)
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			t.Fatal("broker never received a request")
		}
		assert.Contains(t, hop0.Connects(), proxychaintest.HostOf(hop1.URL))
		assert.Contains(t, hop1.Connects(), broker)
	})

	t.Run("TLS runs end to end through the tunnel", func(t *testing.T) {
		t.Parallel()
		broker, got := proxychaintest.FirstBytes(t, 1)
		hop := proxychaintest.New(t, false)
		publisher := newProxiedKafkaPublisher(t, hop.URL, broker, "true")

		_, err := publishKafkaWithTimeout(t, publisher, time.Second)
		require.Error(t, err)
		select {
		case b := <-got:
			assert.Equal(t, byte(0x16), b[0], "TLS handshake record")
		case <-time.After(5 * time.Second):
			t.Fatal("broker never received the TLS ClientHello")
		}
		assert.Contains(t, hop.Connects(), broker)
	})

	t.Run("RBAC deny is a network_unreachable attempt", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) {
			return http.StatusForbidden, http.Header{"X-Envoy-Response-Code-Details": {"rbac_access_denied_matched_policy[none]"}}
		}
		publisher := newProxiedKafkaPublisher(t, hop.URL, "broker.example.com:9092", "false")

		delivery, err := publishKafkaWithTimeout(t, publisher, 5*time.Second)
		require.Error(t, err)
		require.NotNil(t, delivery)
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, "network_unreachable", delivery.Code)
	})

	t.Run("proxy auth rejected nacks", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		publisher := newProxiedKafkaPublisher(t, proxychaintest.WithCreds(hop.URL, "u", "topsecret"), "broker.example.com:9092", "false")

		delivery, err := publishKafkaWithTimeout(t, publisher, 5*time.Second)
		require.Error(t, err)
		assert.Nil(t, delivery)
		assert.NotContains(t, err.Error(), "topsecret")
	})
}

// The bootstrap broker is reached as 127.0.0.1 and advertises itself as
// localhost, so the produce request opens a second tunnel after metadata.
// Rejecting only that one exercises the produce path, where kafka-go reports
// errors per message.
func TestKafkaPublisher_ProxyRejectsLeaderIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	testutil.SkipUnlessDest(t)
	t.Cleanup(testinfra.Start(t))
	brokerAddr := testinfra.EnsureKafka(t)
	require.True(t, strings.HasPrefix(brokerAddr, "localhost:"), "advertised listener is %s", brokerAddr)
	bootstrap := "127.0.0.1:" + strings.TrimPrefix(brokerAddr, "localhost:")
	topic := "test-topic-proxy-leader"
	ensureKafkaTopic(t, brokerAddr, topic)

	for _, tt := range []struct {
		name     string
		status   int
		header   http.Header
		wantCode string
	}{
		{
			name:     "RBAC deny is a network_unreachable attempt",
			status:   http.StatusForbidden,
			header:   http.Header{"X-Envoy-Response-Code-Details": {"rbac_access_denied_matched_policy[none]"}},
			wantCode: "network_unreachable",
		},
		{
			name:   "proxy auth rejected nacks",
			status: http.StatusProxyAuthRequired,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hop := proxychaintest.New(t, false)
			hop.Reject = func(target string) (int, http.Header) {
				if target == bootstrap {
					return 0, nil
				}
				return tt.status, tt.header
			}
			publisher := newProxiedKafkaTopicPublisher(t, hop.URL, bootstrap, topic, "false")

			delivery, err := publishKafkaWithTimeout(t, publisher, 10*time.Second)
			require.Error(t, err)
			assert.Contains(t, hop.Connects(), brokerAddr, "produce went to the advertised leader")
			if tt.wantCode == "" {
				assert.Nil(t, delivery)
				return
			}
			require.NotNil(t, delivery)
			assert.Equal(t, tt.wantCode, delivery.Code)
			assert.NotContains(t, delivery.Response["error"], "proxy ")
		})
	}
}

func TestKafkaPublisher_ProxyTimeoutHidesProxyAddress(t *testing.T) {
	t.Parallel()
	hop := proxychaintest.New(t, false)
	hop.Hold = make(chan struct{})
	t.Cleanup(func() { close(hop.Hold) })
	broker := proxychaintest.ClosedAddr(t)
	publisher := newProxiedKafkaPublisher(t, hop.URL, broker, "false")

	delivery, err := publishKafkaWithTimeout(t, publisher, 300*time.Millisecond)
	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "timeout", delivery.Code)
	assert.Equal(t, "timeout connecting to 127.0.0.1", delivery.Response["error"])
}

func TestKafkaPublisher_ProxyKeepsTLSErrors(t *testing.T) {
	t.Parallel()
	broker := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(broker.Close)
	hop := proxychaintest.New(t, false)
	publisher := newProxiedKafkaPublisher(t, hop.URL, proxychaintest.HostOf(broker.URL), "true")

	delivery, err := publishKafkaWithTimeout(t, publisher, 5*time.Second)
	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "tls_error", delivery.Code)
	assert.Contains(t, delivery.Response["error"], "x509")
}

func TestKafkaPublisher_ProxyMasksWithBrokerHosts(t *testing.T) {
	t.Parallel()
	hop := proxychaintest.New(t, false)
	hop.Hold = make(chan struct{})
	t.Cleanup(func() { close(hop.Hold) })
	publisher := newProxiedKafkaPublisher(t, hop.URL, "a.example.com:9092,b.example.com:9092,a.example.com:9093", "false")

	delivery, err := publishKafkaWithTimeout(t, publisher, 300*time.Millisecond)
	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "timeout connecting to a.example.com,b.example.com", delivery.Response["error"])
}
