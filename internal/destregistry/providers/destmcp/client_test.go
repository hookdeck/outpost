package destmcp_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()

	t.Run("returns redirects instead of following them", func(t *testing.T) {
		t.Parallel()
		target := newRecorder(t, nil)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{})
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusFound, resp.StatusCode)
		assert.Empty(t, target.Requests())
	})

	t.Run("caps response headers", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Big", strings.Repeat("a", 65<<10))
		}))
		t.Cleanup(srv.Close)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{})
		resp, err := client.Get(srv.URL)
		if err == nil {
			resp.Body.Close()
		}
		require.Error(t, err)
	})

	t.Run("does not ask for gzip", func(t *testing.T) {
		t.Parallel()
		got := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got <- r.Header.Get("Accept-Encoding")
		}))
		t.Cleanup(srv.Close)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{})
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Empty(t, <-got)
	})

	t.Run("connects directly without a proxy", func(t *testing.T) {
		t.Parallel()
		srv := newRecorder(t, nil)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{})
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Len(t, srv.Requests(), 1)

		_, err = newClient(t, &netguard.Guard{}, destregistry.HTTPClientConfig{}).Get(srv.URL)
		require.Error(t, err)
		assert.True(t, netguard.IsAddressNotAllowed(err))
	})

	t.Run("goes through the forward proxy chain", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, false)
		srv := newTLSRecorder(t)
		hops, err := proxychain.Parse(hop0.URL + " " + hop1.URL)
		require.NoError(t, err)

		// The default guard refuses loopback: with a proxy it isn't consulted.
		client := newClient(t, &netguard.Guard{}, destregistry.HTTPClientConfig{Proxy: hops, WrapTransport: trustServer(srv.Server)})
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Len(t, srv.Requests(), 1)
		assert.Equal(t, []string{hop1.Listener.Addr().String()}, hop0.Connects())
		assert.Equal(t, []string{proxychaintest.HostOf(srv.URL)}, hop1.Connects())
	})

	t.Run("keeps its limits behind a proxy", func(t *testing.T) {
		t.Parallel()
		proxy := proxychaintest.New(t, false)
		target := newTLSRecorder(t)
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
		}))
		t.Cleanup(srv.Close)
		hops, err := proxychain.Parse(proxy.URL)
		require.NoError(t, err)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{Proxy: hops, WrapTransport: trustServer(srv, target.Server)})
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusFound, resp.StatusCode)
		assert.Empty(t, target.Requests())
	})

	t.Run("requires a guard", func(t *testing.T) {
		t.Parallel()
		_, err := destmcp.NewHTTPClient(nil, destregistry.HTTPClientConfig{})
		require.Error(t, err)
	})
}

func TestRetryPolicy(t *testing.T) {
	t.Parallel()

	t.Run("configured", func(t *testing.T) {
		t.Parallel()
		b := &backoff.ConstantBackoff{Interval: time.Second}
		p := newProvider(t, func(c *destmcp.Config) {
			c.RetryBackoff = b
			c.RetryMaxRetries = 3
		})
		got, max, ok := p.RetryPolicy()
		assert.True(t, ok)
		assert.Equal(t, b, got)
		assert.Equal(t, 3, max)
	})

	t.Run("unset falls back to the global policy", func(t *testing.T) {
		t.Parallel()
		_, _, ok := newProvider(t).RetryPolicy()
		assert.False(t, ok)
	})
}

func newTLSRecorder(t *testing.T) *recorder {
	t.Helper()
	rec := &recorder{}
	rec.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func trustServer(servers ...*httptest.Server) func(*http.Transport, *url.URL) http.RoundTripper {
	pool := x509.NewCertPool()
	for _, s := range servers {
		pool.AddCert(s.Certificate())
	}
	return func(tr *http.Transport, _ *url.URL) http.RoundTripper {
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
		return tr
	}
}
