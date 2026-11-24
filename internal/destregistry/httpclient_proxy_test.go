package destregistry_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rootPool trusts every httptest TLS server's certificate.
func rootPool(servers ...*httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, s := range servers {
		pool.AddCert(s.Certificate())
	}
	return pool
}

func newChainClient(t *testing.T, chain string, roots *x509.CertPool, onConn func(bool)) *http.Client {
	t.Helper()
	hops, err := proxychain.Parse(chain)
	require.NoError(t, err)
	client, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{
		Proxy:        hops,
		OnConnection: onConn,
		WrapTransport: func(tr *http.Transport, _ *url.URL) http.RoundTripper {
			if roots != nil {
				tr.TLSClientConfig = &tls.Config{RootCAs: roots}
			}
			return tr
		},
	})
	require.NoError(t, err)
	return client
}

func newDestination(t *testing.T, useTLS bool) *httptest.Server {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo", string(body))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	var s *httptest.Server
	if useTLS {
		s = httptest.NewTLSServer(h)
	} else {
		s = httptest.NewServer(h)
	}
	t.Cleanup(s.Close)
	return s
}

func TestChainDialer_TwoHops_HTTPSDestination(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, hop0.URL+" "+hop1.URL, rootPool(dest), nil)
	resp, err := client.Post(dest.URL, "text/plain", strings.NewReader("hello"))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, "hello", resp.Header.Get("X-Echo"))

	assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects(), "hop 0 only tunnels to hop 1")
	assert.Equal(t, []string{proxychaintest.HostOf(dest.URL)}, hop1.Connects(), "hop 1 tunnels to the destination")
}

func TestChainDialer_TwoHops_HTTPDestination(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	dest := newDestination(t, false)

	client := newChainClient(t, hop0.URL+" "+proxychaintest.WithCreds(hop1.URL, "last", "secret"), nil, nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects())
	assert.Empty(t, hop1.Connects(), "plain-http target is forwarded absolute-URI, no CONNECT at the last hop")
}

func TestChainDialer_ThreeHops(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	hop2 := proxychaintest.New(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, strings.Join([]string{hop0.URL, hop1.URL, hop2.URL}, " "), rootPool(dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects())
	assert.Equal(t, []string{proxychaintest.HostOf(hop2.URL)}, hop1.Connects())
	assert.Equal(t, []string{proxychaintest.HostOf(dest.URL)}, hop2.Connects())
}

func TestChainDialer_HTTPSFirstHop(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, true)
	hop1 := proxychaintest.New(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, hop0.URL+" "+hop1.URL, rootPool(hop0.Server, dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects())
	assert.Equal(t, []string{proxychaintest.HostOf(dest.URL)}, hop1.Connects())
}

func TestChainDialer_HTTPSIntermediateHop(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, true)
	hop2 := proxychaintest.New(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, strings.Join([]string{hop0.URL, hop1.URL, hop2.URL}, " "), rootPool(hop1.Server, dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, []string{proxychaintest.HostOf(hop2.URL)}, hop1.Connects(), "TLS to hop 1 runs inside hop 0's tunnel")
}

func TestChainDialer_CredentialsPerHop(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	dest := newDestination(t, true)

	chain := proxychaintest.WithCreds(hop0.URL, "first", "pw0") + " " + proxychaintest.WithCreds(hop1.URL, "last", "pw1")
	client := newChainClient(t, chain, rootPool(dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, []string{"Basic Zmlyc3Q6cHcw"}, hop0.Auths(), "first:pw0")
	assert.Equal(t, []string{"Basic bGFzdDpwdzE="}, hop1.Auths(), "last:pw1, written by Go's transport")
}

func TestChainDialer_TunnelReused(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	dest := newDestination(t, true)

	var reused []bool
	var mu sync.Mutex
	client := newChainClient(t, hop0.URL+" "+hop1.URL, rootPool(dest), func(r bool) {
		mu.Lock()
		reused = append(reused, r)
		mu.Unlock()
	})
	for range 2 {
		resp, err := client.Get(dest.URL)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	assert.Equal(t, []bool{false, true}, reused)
	assert.Len(t, hop0.Connects(), 1, "one tunnel serves both requests")
}

func TestChainDialer_FirstHopUnreachable(t *testing.T) {
	t.Parallel()
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	hop1 := proxychaintest.New(t, false)

	client := newChainClient(t, proxychaintest.WithCreds(deadURL, "u", "topsecret")+" "+hop1.URL, nil, nil)
	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)
	var opErr *net.OpError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, "proxyconnect", opErr.Op)
	assert.NotContains(t, err.Error(), "topsecret")
}

func TestChainDialer_IntermediateHopRejects(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop0.Reject = func(string) (int, http.Header) {
		return http.StatusServiceUnavailable, http.Header{"X-Envoy-Response-Flags": {"UF"}}
	}
	hop1 := proxychaintest.New(t, false)

	client := newChainClient(t, proxychaintest.WithCreds(hop0.URL, "u", "topsecret")+" "+hop1.URL, nil, nil)
	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var connectErr *proxychain.ConnectError
	require.True(t, errors.As(err, &connectErr), "got %v", err)
	assert.Equal(t, http.StatusServiceUnavailable, connectErr.Status)
	assert.Equal(t, "UF", connectErr.Header.Get("X-Envoy-Response-Flags"))
	assert.Equal(t, proxychaintest.HostOf(hop1.URL), connectErr.Next)
	assert.Equal(t, "http://"+proxychaintest.HostOf(hop0.URL), connectErr.Hop)
	assert.NotContains(t, err.Error(), "topsecret")
	assert.Empty(t, hop1.Connects())
}

func TestChainDialer_ContextTimeoutDuringConnect(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	hop0.Hold = make(chan struct{})
	defer close(hop0.Hold)
	hop1 := proxychaintest.New(t, false)

	client := newChainClient(t, hop0.URL+" "+hop1.URL, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/", nil)
	start := time.Now()
	_, err := client.Do(req)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline"), "got %v", err)
}

func TestChainDialer_SingleHopUnchanged(t *testing.T) {
	t.Parallel()
	hop0 := proxychaintest.New(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, hop0.URL, rootPool(dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	tr := client.Transport.(*http.Transport)
	assert.NotNil(t, tr.Proxy)
	assert.Equal(t, []string{proxychaintest.HostOf(dest.URL)}, hop0.Connects())
}
