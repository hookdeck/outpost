package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestBuilder(t *testing.T, mutate func(*config.Config)) *ServiceBuilder {
	t.Helper()
	cfg := &config.Config{}
	cfg.InitDefaults()
	if mutate != nil {
		mutate(cfg)
	}
	return &ServiceBuilder{cfg: cfg, logger: logging.NewTestLogger(zap.NewNop())}
}

func TestMCPDisabledBuildsNothing(t *testing.T) {
	b := newTestBuilder(t, nil)

	n, err := b.mcpNetwork()
	require.NoError(t, err)
	assert.Nil(t, n)
	provider, verifier, err := b.mcpProvider(nil)
	require.NoError(t, err)
	assert.Nil(t, provider)
	assert.Nil(t, verifier)
	assert.Nil(t, b.mcpNet)
}

func TestMCPDefaultRetrySchedule(t *testing.T) {
	b := newTestBuilder(t, func(c *config.Config) { c.Experimental.MCP.Enabled = true })

	provider, _, err := b.mcpProvider(nil)
	require.NoError(t, err)
	assert.Equal(t, 4, provider.RetryMaxRetries)
	assert.Equal(t, 5*time.Minute, provider.RetryBackoff.Duration(3))
}

func TestMCPEnabledWiring(t *testing.T) {
	b := newTestBuilder(t, func(c *config.Config) {
		c.Experimental.MCP.Enabled = true
		c.Experimental.MCP.RetryDelays = []time.Duration{15 * time.Second, 60 * time.Second}
		c.Experimental.MCP.AllowInsecureCallbacks = true
		c.Destinations.PrivateAddressAllowlist = []string{"localhost"}
	})

	provider, verifier, err := b.mcpProvider(nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Nil(t, verifier, "services without a verifier store build none")
	assert.NotNil(t, provider.Client)
	assert.NotNil(t, provider.Guard)

	n, err := b.mcpNetwork()
	require.NoError(t, err)
	assert.Same(t, n, b.mcpNet, "the network stack is built once")

	assert.True(t, provider.AllowInsecureHTTP)
	require.NotNil(t, provider.RetryBackoff)
	assert.Equal(t, 2, provider.RetryMaxRetries)
	assert.True(t, n.guard.Allowlist.Contains(netip.MustParseAddr("127.0.0.1")), "the destinations allowlist applies")
}

func TestAllowlistExempt(t *testing.T) {
	allow, err := netguard.ParseAllowlist([]string{"localhost", "192.0.2.0/24"})
	require.NoError(t, err)
	exempt := allowlistExempt(allow)
	assert.True(t, exempt("127.0.0.1"))
	assert.True(t, exempt("192.0.2.7"))
	assert.True(t, exempt("localhost"))
	assert.True(t, exempt("LOCALHOST."))
	assert.False(t, exempt("198.51.100.1"))
	assert.False(t, exempt("receiver.example.com"))

	only4, err := netguard.ParseAllowlist([]string{"127.0.0.0/8"})
	require.NoError(t, err)
	assert.False(t, allowlistExempt(only4)("localhost"), "localhost needs both loopbacks allowed")
}

// recordingProxy forwards plain-HTTP proxy requests and records their URLs.
type recordingProxy struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
}

func newRecordingProxy(t *testing.T) *recordingProxy {
	t.Helper()
	p := &recordingProxy{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.seen = append(p.seen, r.URL.String())
		p.mu.Unlock()
		out, err := http.DefaultTransport.RoundTrip(&http.Request{Method: r.Method, URL: r.URL, Header: r.Header, Body: r.Body, Host: r.Host})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer out.Body.Close()
		for k, v := range out.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(out.StatusCode)
		_, _ = io.Copy(w, out.Body)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *recordingProxy) Seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seen...)
}

func newEchoReceiver(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Challenge string `json:"challenge"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"challenge":"`+body.Challenge+`"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMCPForwardProxy(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	receiver := newEchoReceiver(t)
	secret := mcpevents.Secret{Key: []byte(strings.Repeat("k", 32))}

	run := func(t *testing.T, proxyURL string) {
		mr.FlushAll()
		b := newTestBuilder(t, func(c *config.Config) {
			c.Experimental.MCP.Enabled = true
			c.Destinations.ProxyURL = proxyURL
			c.Destinations.PrivateAddressAllowlist = []string{"localhost"}
		})
		provider, verifier, err := b.mcpProvider(rc)
		require.NoError(t, err)

		err = verifier.Verify(t.Context(), mcpevents.VerifyRequest{
			TenantID:       "tenant_1",
			URL:            receiver.URL + "/challenge",
			SubscriptionID: "sub_0123456789abcdef0123456789abcdef",
			Secrets:        []mcpevents.Secret{secret},
		})
		require.NoError(t, err)
		resp, err := provider.Client.Post(receiver.URL+"/delivery", "application/json", strings.NewReader("{}"))
		require.NoError(t, err)
		resp.Body.Close()
	}

	t.Run("deliveries and challenges go through the proxy", func(t *testing.T) {
		proxy := newRecordingProxy(t)
		run(t, proxy.URL)
		assert.Equal(t, []string{receiver.URL + "/challenge", receiver.URL + "/delivery"}, proxy.Seen())
	})

	t.Run("connect directly without one", func(t *testing.T) {
		proxy := newRecordingProxy(t)
		t.Setenv("HTTP_PROXY", proxy.URL)
		t.Setenv("HTTPS_PROXY", proxy.URL)
		run(t, "")
		assert.Empty(t, proxy.Seen())
	})

	t.Run("an invalid proxy URL is an error", func(t *testing.T) {
		b := newTestBuilder(t, func(c *config.Config) {
			c.Experimental.MCP.Enabled = true
			c.Destinations.ProxyURL = "://invalid"
		})
		_, err := b.mcpNetwork()
		require.Error(t, err)
	})
}
