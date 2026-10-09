package netguard_test

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClient(t *testing.T, cfg netguard.ClientConfig) *http.Client {
	t.Helper()
	client, err := netguard.NewHTTPClient(cfg)
	require.NoError(t, err)
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func portOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	return port
}

func get(t *testing.T, client *http.Client, rawURL string) (*http.Response, string, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body), nil
}

// The connection goes to the vetted IP while TLS (SNI, certificate check)
// and the Host header keep the original name. httptest's certificate covers
// example.com and *.example.com.
func TestNewHTTPClient_PreservesSNIAndHost(t *testing.T) {
	t.Parallel()
	for _, h2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "http1", true: "http2"}[h2], func(t *testing.T) {
			t.Parallel()
			type seen struct{ serverName, host, ua, remote string }
			var mu sync.Mutex
			var got []seen
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				got = append(got, seen{r.TLS.ServerName, r.Host, r.Header.Get("User-Agent"), r.RemoteAddr})
				mu.Unlock()
				_, _ = io.WriteString(w, r.Proto)
			}))
			srv.EnableHTTP2 = h2
			srv.StartTLS()
			t.Cleanup(srv.Close)
			port := portOf(t, srv)
			roots := x509.NewCertPool()
			roots.AddCert(srv.Certificate())

			res := newResolver().
				set("hooks.example.com", []string{"127.0.0.1"}).
				set("hooks.other.test", []string{"127.0.0.1"})
			guard := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "127.0.0.0/8")}
			var reused []bool
			client := newClient(t, netguard.ClientConfig{
				Guard:        guard,
				UserAgent:    "Outpost-Test/1.0",
				RootCAs:      roots,
				OnConnection: func(r bool) { mu.Lock(); reused = append(reused, r); mu.Unlock() },
			})

			for range 2 {
				resp, body, err := get(t, client, "https://hooks.example.com:"+port+"/hook")
				require.NoError(t, err)
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.Equal(t, map[bool]int{false: 1, true: 2}[h2], resp.ProtoMajor, body)
			}
			mu.Lock()
			require.Len(t, got, 2)
			assert.Equal(t, "hooks.example.com", got[0].serverName)
			assert.Equal(t, "hooks.example.com:"+port, got[0].host)
			assert.Equal(t, "Outpost-Test/1.0", got[0].ua)
			assert.Equal(t, []bool{false, true}, reused, "second request reuses the pooled connection")
			mu.Unlock()
			assert.Equal(t, []string{"hooks.example.com."}, res.Queries(), "a pooled connection isn't re-resolved")

			// The certificate is checked against the name, not the IP: a name
			// the certificate doesn't cover fails even though the IP is fine.
			_, _, err := get(t, client, "https://hooks.other.test:"+port+"/hook")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "x509")

			// And verification is on: without the test root the handshake fails.
			strict := newClient(t, netguard.ClientConfig{Guard: guard})
			_, _, err = get(t, strict, "https://hooks.example.com:"+port+"/hook")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "x509")
		})
	}
}

func TestNewHTTPClient_DoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var landed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		landed.Add(1)
	}))
	t.Cleanup(target.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := http.StatusFound
		if r.Method == http.MethodPost {
			code = http.StatusTemporaryRedirect
		}
		http.Redirect(w, r, target.URL+"/landed", code)
	}))
	t.Cleanup(redirector.Close)

	client := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{Allowlist: mustAllowlist(t, "localhost")}})
	resp, _, err := get(t, client, redirector.URL+"/start")
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, target.URL+"/landed", resp.Header.Get("Location"))

	resp, err = client.Post(redirector.URL+"/start", "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Zero(t, landed.Load(), "the redirect target is never contacted")
}

// Not parallel: it sets process environment.
func TestNewHTTPClient_IgnoresEnvironmentProxy(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		_, _ = io.WriteString(w, "via proxy")
	}))
	t.Cleanup(proxy.Close)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "direct")
	}))
	t.Cleanup(target.Close)
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(k, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	// A name, not 127.0.0.1: Go never proxies loopback literals anyway.
	res := newResolver().set("hooks.example.com", []string{"127.0.0.1"})
	client := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "localhost")}})
	assert.Nil(t, netguard.TransportOf(client).Proxy)

	_, body, err := get(t, client, "http://hooks.example.com:"+portOf(t, target)+"/")
	require.NoError(t, err)
	assert.Equal(t, "direct", body)
	assert.Zero(t, proxied.Load())
}

func TestNewHTTPClient_CapsResponseHeaders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := 16 << 10
		if r.URL.Path == "/big" {
			size = netguard.MaxResponseHeaderBytes + 1
		}
		w.Header().Set("X-Filler", strings.Repeat("a", size))
	}))
	t.Cleanup(srv.Close)
	client := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{Allowlist: mustAllowlist(t, "localhost")}})

	resp, _, err := get(t, client, srv.URL+"/small")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	_, _, err = get(t, client, srv.URL+"/big")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded")
}

func TestNewHTTPClient_RebindingRejectedAtDial(t *testing.T) {
	t.Parallel()
	res := newResolver().set("rebind.example.com", []string{"93.184.216.34"}, []string{"10.0.0.1"})
	guard := &netguard.Guard{Resolver: res}
	rec := &dialRecorder{}
	netguard.SetDial(guard, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		return nil, errors.New("unexpected dial")
	})
	client := newClient(t, netguard.ClientConfig{Guard: guard})

	require.NoError(t, guard.CheckURL(context.Background(), mustURL(t, "https://rebind.example.com/hook")))
	_, _, err := get(t, client, "https://rebind.example.com/hook")
	e := requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.Equal(t, netip.MustParseAddr("10.0.0.1"), e.Addr)
	assert.Empty(t, rec.dialed())
}

// The http allowance is decided per connection: a name that pointed at an
// allowlisted loopback address when it was checked can't later carry
// cleartext to the internet.
func TestNewHTTPClient_HTTPPolicyAtDialTime(t *testing.T) {
	t.Parallel()
	res := newResolver().set("tunnel.example.com", []string{"127.0.0.1"}, []string{"8.8.8.8"})
	guard := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "localhost")}
	rec := &dialRecorder{}
	netguard.SetDial(guard, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		return nil, errors.New("unexpected dial")
	})
	client := newClient(t, netguard.ClientConfig{Guard: guard})

	require.NoError(t, guard.CheckURL(context.Background(), mustURL(t, "http://tunnel.example.com/hook")))
	_, _, err := get(t, client, "http://tunnel.example.com/hook")
	requireNotAllowed(t, err, netguard.ReasonHTTPSRequired)
	assert.Empty(t, rec.dialed())

	// Same address over https is fine: the scheme reaches the dialer.
	res.set("public.example.com", []string{"8.8.8.8"})
	netguard.SetDial(guard, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		return nil, errors.New("refused")
	})
	_, _, err = get(t, client, "https://public.example.com/hook")
	require.Error(t, err)
	assert.False(t, netguard.IsAddressNotAllowed(err))
	assert.Equal(t, []string{"8.8.8.8:443"}, rec.dialed())
}

// The request deadline reaches the dialer even though http.Transport dials
// with a context stripped of it, so a blackholed first address gets only its
// share of the budget.
func TestNewHTTPClient_PerAddressTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	port := portOf(t, srv)

	res := newResolver().set("hooks.example.com", []string{"127.0.0.2", "127.0.0.1"})
	guard := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "localhost")}
	rec := &dialRecorder{}
	var blackholeBudget atomic.Int64
	netguard.SetDial(guard, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		if strings.HasPrefix(address, "127.0.0.2:") {
			dl, _ := ctx.Deadline()
			blackholeBudget.Store(int64(time.Until(dl)))
			<-ctx.Done()
			return nil, &net.OpError{Op: "dial", Net: network, Err: ctx.Err()}
		}
		return d.DialContext(ctx, network, address)
	})
	client := newClient(t, netguard.ClientConfig{Guard: guard})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://hooks.example.com:"+port+"/", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, []string{"127.0.0.2:" + port, "127.0.0.1:" + port}, rec.dialed())
	budget := time.Duration(blackholeBudget.Load())
	assert.InDelta(t, float64(500*time.Millisecond), float64(budget), float64(200*time.Millisecond), "budget %s", budget)
}

type closeTracker struct {
	io.Reader
	closed atomic.Bool
}

func (c *closeTracker) Close() error {
	c.closed.Store(true)
	return nil
}

func TestNewHTTPClient_ProxyMode(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Method == http.MethodConnect {
			seen = append(seen, "CONNECT "+r.Host)
		} else {
			seen = append(seen, r.Method+" "+r.URL.String())
		}
		mu.Unlock()
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, "proxied")
	}))
	t.Cleanup(proxy.Close)
	proxyURL := mustURL(t, proxy.URL)

	res := newResolver().
		set("hooks.example.com", []string{"203.0.113.7"}).
		set("internal.example.com", []string{"10.0.0.1"}).
		set("secure.example.com", []string{"93.184.216.34"})
	// The proxy listens on 127.0.0.1, which is not allowlisted: dialing the
	// proxy is not guarded, only the target is checked.
	guard := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "203.0.113.0/24"), AllowInsecure: true}
	client := newClient(t, netguard.ClientConfig{Guard: guard, ProxyURL: proxyURL})

	_, body, err := get(t, client, "http://hooks.example.com/x")
	require.NoError(t, err)
	assert.Equal(t, "proxied", body)

	// A target that fails the check never reaches the proxy, and the request
	// body is closed as the RoundTripper contract requires.
	reqBody := &closeTracker{Reader: strings.NewReader("{}")}
	req, err := http.NewRequest(http.MethodPost, "http://internal.example.com/x", reqBody)
	require.NoError(t, err)
	_, err = client.Do(req)
	requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.True(t, reqBody.closed.Load())

	// https targets are checked, then tunnelled with CONNECT by name.
	_, _, err = get(t, client, "https://secure.example.com/x")
	require.Error(t, err)
	assert.False(t, netguard.IsAddressNotAllowed(err))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"GET http://hooks.example.com/x", "CONNECT secure.example.com:443"}, seen)
}

func TestNewHTTPClient_Config(t *testing.T) {
	t.Parallel()
	_, err := netguard.NewHTTPClient(netguard.ClientConfig{})
	assert.Error(t, err)

	for _, raw := range []string{"ftp://user:s3cret@proxy.example:21", "http://", "file:///tmp/sock"} {
		_, err := netguard.NewHTTPClient(netguard.ClientConfig{Guard: &netguard.Guard{}, ProxyURL: mustURL(t, raw)})
		require.Error(t, err, raw)
		assert.NotContains(t, err.Error(), "s3cret")
	}

	client := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{}, MaxIdleConns: 50, MaxIdleConnsPerHost: 7})
	tr := netguard.TransportOf(client)
	assert.Nil(t, tr.Proxy)
	assert.NotNil(t, tr.DialContext)
	assert.Nil(t, tr.DialTLSContext, "TLS stays with the transport so SNI is the URL host")
	assert.Equal(t, 10*time.Second, tr.TLSHandshakeTimeout)
	assert.Equal(t, 8*time.Second, tr.ResponseHeaderTimeout)
	assert.EqualValues(t, 64<<10, tr.MaxResponseHeaderBytes)
	assert.True(t, tr.ForceAttemptHTTP2)
	assert.True(t, tr.DisableCompression)
	assert.Equal(t, 50, tr.MaxIdleConns)
	assert.Equal(t, 7, tr.MaxIdleConnsPerHost)
	assert.False(t, tr.TLSClientConfig.InsecureSkipVerify)
	assert.Zero(t, client.Timeout, "callers bound requests with their context")
	assert.Nil(t, client.Jar)

	defaults := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{}})
	assert.Equal(t, 100, netguard.TransportOf(defaults).MaxIdleConns)
}

func TestNewHTTPClient_LeavesRequestUntouched(t *testing.T) {
	t.Parallel()
	var ua atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua.Store(r.Header.Get("User-Agent"))
	}))
	t.Cleanup(srv.Close)
	client := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{Allowlist: mustAllowlist(t, "localhost")}, UserAgent: "Outpost-Test/1.0"})

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "caller")
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "Outpost-Test/1.0", ua.Load())
	assert.Equal(t, "caller", req.Header.Get("User-Agent"))

	// A bare request with no Header map is fine too.
	resp, err = client.Transport.RoundTrip(&http.Request{Method: http.MethodGet, URL: mustURL(t, srv.URL)})
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "Outpost-Test/1.0", ua.Load())
}

// Plain-http to an allowlisted address goes through only with an allowlisted
// loopback (or AllowInsecure), end to end.
func TestNewHTTPClient_PlainHTTPPolicy(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)

	none := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{}})
	_, _, err := get(t, none, srv.URL)
	requireNotAllowed(t, err, netguard.ReasonHTTPSRequired)

	loopback := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{Allowlist: mustAllowlist(t, "127.0.0.1")}})
	resp, _, err := get(t, loopback, srv.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	_, _, err = get(t, loopback, strings.Replace(srv.URL, "127.0.0.1", "127.0.0.2", 1))
	requireNotAllowed(t, err, netguard.ReasonNotGlobal)
}

// With no Resolver the guard uses the system resolver; "localhost." comes
// from the hosts file, so this needs no network. The answer may lead with
// ::1, which the server doesn't listen on: the dialer falls through to
// 127.0.0.1.
func TestNewHTTPClient_DefaultResolver(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Host)
	}))
	t.Cleanup(srv.Close)
	port := portOf(t, srv)

	client := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{Allowlist: mustAllowlist(t, "localhost")}})
	_, body, err := get(t, client, "http://localhost:"+port+"/")
	require.NoError(t, err)
	assert.Equal(t, "localhost:"+port, body)

	// Without the allowlist the same name is refused.
	strict := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{}})
	_, _, err = get(t, strict, "https://localhost:"+port+"/")
	requireNotAllowed(t, err, netguard.ReasonNotGlobal)
}

func TestNewHTTPClient_NilURL(t *testing.T) {
	t.Parallel()
	client := newClient(t, netguard.ClientConfig{Guard: &netguard.Guard{}})
	body := &closeTracker{Reader: strings.NewReader("x")}
	_, err := client.Transport.RoundTrip(&http.Request{Method: http.MethodPost, Body: body})
	require.Error(t, err)
	assert.True(t, body.closed.Load())
}
