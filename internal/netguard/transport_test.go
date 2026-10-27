package netguard_test

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/netguard/netguardtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func get(t *testing.T, rt http.RoundTripper, rawURL string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	return rt.RoundTrip(req)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type closeTracker struct {
	io.Reader
	closed atomic.Bool
}

func (c *closeTracker) Close() error {
	c.closed.Store(true)
	return nil
}

// proxyRecorder is an httptest server standing in for a forward proxy: it
// records what it is asked to forward and answers 502.
type proxyRecorder struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []string
}

func newProxy(t *testing.T) *proxyRecorder {
	p := &proxyRecorder{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.reqs = append(p.reqs, r.Method+" "+r.Host)
		p.mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *proxyRecorder) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.reqs...)
}

func TestWrapTransport_ProxyMode(t *testing.T) {
	t.Parallel()
	proxy := newProxy(t)
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)

	// The proxy sits on unallowlisted 127.0.0.1 and its own address is not
	// checked.
	res := newResolver().set("public.example", []string{"8.8.8.8"}).set("internal.example", []string{"10.0.0.1"})
	g := &netguard.Guard{Resolver: res}
	base := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	t.Cleanup(base.CloseIdleConnections)
	rt := g.WrapTransport(base)

	// A private target is refused before the proxy sees anything, and the
	// request body is closed.
	body := &closeTracker{Reader: strings.NewReader("x")}
	req, err := http.NewRequest(http.MethodPost, "http://internal.example/hook", body)
	require.NoError(t, err)
	_, err = rt.RoundTrip(req)
	require.Error(t, err)
	assert.True(t, netguard.IsAddressNotAllowed(err))
	assert.True(t, body.closed.Load())
	_, err = get(t, rt, "http://10.0.0.1/")
	assert.True(t, netguard.IsAddressNotAllowed(err))
	assert.Empty(t, proxy.seen())

	// A public target is forwarded by name.
	resp, err := get(t, rt, "http://public.example/hook")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Equal(t, []string{"GET public.example"}, proxy.seen())
}

func TestWrapTransport_NilURL(t *testing.T) {
	t.Parallel()
	g := &netguard.Guard{}
	rt := g.WrapTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("must not be reached")
		return nil, nil
	}))
	_, err := rt.RoundTrip(&http.Request{})
	assert.Error(t, err)
}

// The dial connects to the resolved address while TLS (server name, certificate
// check) and the Host header keep the original name. httptest's certificate
// covers example.com and *.example.com.
func TestDialContext_PreservesSNIAndHost(t *testing.T) {
	t.Parallel()
	for _, h2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "http1", true: "http2"}[h2], func(t *testing.T) {
			t.Parallel()
			type seen struct{ serverName, host string }
			var mu sync.Mutex
			var got []seen
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				got = append(got, seen{r.TLS.ServerName, r.Host})
				mu.Unlock()
				_, _ = io.WriteString(w, r.Proto)
			}))
			srv.EnableHTTP2 = h2
			srv.StartTLS()
			t.Cleanup(srv.Close)
			_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
			roots := x509.NewCertPool()
			roots.AddCert(srv.Certificate())

			dns := netguardtest.NewDNS(t).
				Set("hooks.example.com", []string{"127.0.0.1"}).
				Set("hooks.other.test", []string{"127.0.0.1"})
			g := &netguard.Guard{Resolver: dns.Resolver, Allowlist: mustAllowlist(t, "127.0.0.0/8")}
			base := &http.Transport{
				DialContext:       g.DialContext,
				ForceAttemptHTTP2: true,
				TLSClientConfig:   &tls.Config{RootCAs: roots},
			}
			t.Cleanup(base.CloseIdleConnections)
			rt := base

			resp, err := get(t, rt, "https://hooks.example.com:"+port+"/hook")
			require.NoError(t, err)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			assert.Equal(t, map[bool]int{false: 1, true: 2}[h2], resp.ProtoMajor, string(body))
			mu.Lock()
			require.Len(t, got, 1)
			assert.Equal(t, seen{"hooks.example.com", "hooks.example.com:" + port}, got[0])
			mu.Unlock()

			// The certificate is checked against the name, not the IP.
			_, err = get(t, rt, "https://hooks.other.test:"+port+"/hook")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "x509")
		})
	}
}
