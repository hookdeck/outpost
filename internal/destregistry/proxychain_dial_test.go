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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectProxy is an httptest forward proxy: CONNECT is hijacked and piped
// to the requested authority, plain requests are forwarded absolute-URI
// style. It records every CONNECT authority and Proxy-Authorization it saw.
type connectProxy struct {
	*httptest.Server
	mu       sync.Mutex
	connects []string
	auths    []string
	// reject, if set, is consulted for each CONNECT; a non-zero status short
	// circuits with that status and headers.
	reject func(target string) (int, http.Header)
	// hold, if set, is closed by the test to release a CONNECT that is
	// deliberately left unanswered.
	hold chan struct{}
}

func newConnectProxy(t *testing.T, useTLS bool) *connectProxy {
	t.Helper()
	p := &connectProxy{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			out, err := http.DefaultTransport.RoundTrip(&http.Request{
				Method: r.Method, URL: r.URL, Header: r.Header, Body: r.Body, Host: r.Host,
			})
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
			return
		}
		p.mu.Lock()
		p.connects = append(p.connects, r.Host)
		p.auths = append(p.auths, r.Header.Get("Proxy-Authorization"))
		p.mu.Unlock()

		if p.hold != nil {
			<-p.hold
		}
		if p.reject != nil {
			if status, h := p.reject(r.Host); status != 0 {
				for k, v := range h {
					w.Header()[k] = v
				}
				w.WriteHeader(status)
				return
			}
		}
		upstream, err := net.DialTimeout("tcp", r.Host, 5*time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		w.WriteHeader(http.StatusOK)
		conn, buf, err := hj.Hijack()
		require.NoError(t, err)
		defer conn.Close()
		_ = buf.Flush()
		done := make(chan struct{}, 2)
		// Read through buf: the server may already hold the client's first
		// post-CONNECT bytes there, and reading the raw conn would drop them.
		go func() { _, _ = io.Copy(upstream, buf); done <- struct{}{} }()
		go func() { _, _ = io.Copy(conn, upstream); done <- struct{}{} }()
		<-done
	})
	if useTLS {
		p.Server = httptest.NewTLSServer(handler)
	} else {
		p.Server = httptest.NewServer(handler)
	}
	t.Cleanup(p.Close)
	return p
}

func (p *connectProxy) sawConnects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.connects...)
}

func (p *connectProxy) sawAuths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.auths...)
}

// withCreds rewrites a server URL to carry userinfo.
func withCreds(rawURL, user, pass string) string {
	u, _ := url.Parse(rawURL)
	u.User = url.UserPassword(user, pass)
	return u.String()
}

func hostOf(rawURL string) string {
	u, _ := url.Parse(rawURL)
	return u.Host
}

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
	hops, err := destregistry.ParseProxyURL(chain)
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
	hop0 := newConnectProxy(t, false)
	hop1 := newConnectProxy(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, hop0.URL+" "+hop1.URL, rootPool(dest), nil)
	resp, err := client.Post(dest.URL, "text/plain", strings.NewReader("hello"))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, "hello", resp.Header.Get("X-Echo"))

	assert.Equal(t, []string{hostOf(hop1.URL)}, hop0.sawConnects(), "hop 0 only tunnels to hop 1")
	assert.Equal(t, []string{hostOf(dest.URL)}, hop1.sawConnects(), "hop 1 tunnels to the destination")
}

func TestChainDialer_TwoHops_HTTPDestination(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, false)
	hop1 := newConnectProxy(t, false)
	dest := newDestination(t, false)

	client := newChainClient(t, hop0.URL+" "+withCreds(hop1.URL, "last", "secret"), nil, nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, []string{hostOf(hop1.URL)}, hop0.sawConnects())
	assert.Empty(t, hop1.sawConnects(), "plain-http target is forwarded absolute-URI, no CONNECT at the last hop")
}

func TestChainDialer_ThreeHops(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, false)
	hop1 := newConnectProxy(t, false)
	hop2 := newConnectProxy(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, strings.Join([]string{hop0.URL, hop1.URL, hop2.URL}, " "), rootPool(dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, []string{hostOf(hop1.URL)}, hop0.sawConnects())
	assert.Equal(t, []string{hostOf(hop2.URL)}, hop1.sawConnects())
	assert.Equal(t, []string{hostOf(dest.URL)}, hop2.sawConnects())
}

func TestChainDialer_HTTPSFirstHop(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, true)
	hop1 := newConnectProxy(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, hop0.URL+" "+hop1.URL, rootPool(hop0.Server, dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, []string{hostOf(hop1.URL)}, hop0.sawConnects())
	assert.Equal(t, []string{hostOf(dest.URL)}, hop1.sawConnects())
}

func TestChainDialer_HTTPSIntermediateHop(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, false)
	hop1 := newConnectProxy(t, true)
	hop2 := newConnectProxy(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, strings.Join([]string{hop0.URL, hop1.URL, hop2.URL}, " "), rootPool(hop1.Server, dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, []string{hostOf(hop2.URL)}, hop1.sawConnects(), "TLS to hop 1 runs inside hop 0's tunnel")
}

func TestChainDialer_CredentialsPerHop(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, false)
	hop1 := newConnectProxy(t, false)
	dest := newDestination(t, true)

	chain := withCreds(hop0.URL, "first", "pw0") + " " + withCreds(hop1.URL, "last", "pw1")
	client := newChainClient(t, chain, rootPool(dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, []string{"Basic Zmlyc3Q6cHcw"}, hop0.sawAuths(), "first:pw0")
	assert.Equal(t, []string{"Basic bGFzdDpwdzE="}, hop1.sawAuths(), "last:pw1, written by Go's transport")
}

func TestChainDialer_TunnelReused(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, false)
	hop1 := newConnectProxy(t, false)
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
	assert.Len(t, hop0.sawConnects(), 1, "one tunnel serves both requests")
}

func TestChainDialer_FirstHopUnreachable(t *testing.T) {
	t.Parallel()
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	hop1 := newConnectProxy(t, false)

	client := newChainClient(t, withCreds(deadURL, "u", "topsecret")+" "+hop1.URL, nil, nil)
	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)
	var opErr *net.OpError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, "proxyconnect", opErr.Op)
	assert.NotContains(t, err.Error(), "topsecret")
}

func TestChainDialer_IntermediateHopRejects(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, false)
	hop0.reject = func(string) (int, http.Header) {
		return http.StatusServiceUnavailable, http.Header{"X-Envoy-Response-Flags": {"UF"}}
	}
	hop1 := newConnectProxy(t, false)

	client := newChainClient(t, withCreds(hop0.URL, "u", "topsecret")+" "+hop1.URL, nil, nil)
	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var connectErr *destregistry.ProxyConnectError
	require.True(t, errors.As(err, &connectErr), "got %v", err)
	assert.Equal(t, http.StatusServiceUnavailable, connectErr.Status)
	assert.Equal(t, "UF", connectErr.Header.Get("X-Envoy-Response-Flags"))
	assert.Equal(t, hostOf(hop1.URL), connectErr.Next)
	assert.Equal(t, "http://"+hostOf(hop0.URL), connectErr.Hop)
	assert.NotContains(t, err.Error(), "topsecret")
	assert.Empty(t, hop1.sawConnects())
}

func TestChainDialer_ContextTimeoutDuringConnect(t *testing.T) {
	t.Parallel()
	hop0 := newConnectProxy(t, false)
	hop0.hold = make(chan struct{})
	defer close(hop0.hold)
	hop1 := newConnectProxy(t, false)

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
	hop0 := newConnectProxy(t, false)
	dest := newDestination(t, true)

	client := newChainClient(t, hop0.URL, rootPool(dest), nil)
	resp, err := client.Get(dest.URL)
	require.NoError(t, err)
	resp.Body.Close()
	tr := client.Transport.(*http.Transport)
	assert.NotNil(t, tr.Proxy)
	assert.Equal(t, []string{hostOf(dest.URL)}, hop0.sawConnects())
}
