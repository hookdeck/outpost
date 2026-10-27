package destregistry_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/netguard/netguardtest"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func guardAllowing(t *testing.T, res *net.Resolver, entries ...string) *netguard.Guard {
	t.Helper()
	al, err := netguard.ParseAllowlist(entries)
	require.NoError(t, err)
	return &netguard.Guard{Resolver: res, Allowlist: al}
}

func newGuardedClient(t *testing.T, cfg destregistry.HTTPClientConfig) *http.Client {
	t.Helper()
	client, err := destregistry.NewHTTPClient(cfg)
	require.NoError(t, err)
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func getBody(t *testing.T, client *http.Client, rawURL string) (string, error) {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b), nil
}

func TestHTTPClientGuard_DirectDialsThroughGuard(t *testing.T) {
	t.Parallel()
	dest := newDestination(t, false)

	blocked := newGuardedClient(t, destregistry.HTTPClientConfig{Guard: &netguard.Guard{}})
	_, err := getBody(t, blocked, dest.URL)
	require.Error(t, err)
	assert.True(t, netguard.IsAddressNotAllowed(err))

	allowed := newGuardedClient(t, destregistry.HTTPClientConfig{Guard: guardAllowing(t, nil, "127.0.0.1/32")})
	body, err := getBody(t, allowed, dest.URL)
	require.NoError(t, err)
	assert.Equal(t, "ok", body)
}

// Not parallel: it sets process environment.
func TestHTTPClientGuard_IgnoresEnvironmentProxy(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		_, _ = io.WriteString(w, "via proxy")
	}))
	t.Cleanup(proxy.Close)
	dest := newDestination(t, false)
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(k, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	// A name, not 127.0.0.1: Go never proxies loopback literals anyway.
	_, port, _ := net.SplitHostPort(dest.Listener.Addr().String())
	res := netguardtest.NewDNS(t).Set("hooks.example.com", []string{"127.0.0.1"}).Resolver
	client := newGuardedClient(t, destregistry.HTTPClientConfig{Guard: guardAllowing(t, res, "localhost")})
	body, err := getBody(t, client, "http://hooks.example.com:"+port+"/")
	require.NoError(t, err)
	assert.Equal(t, "ok", body)
	assert.Zero(t, proxied.Load())
}

func TestHTTPClientGuard_ProxyChain(t *testing.T) {
	t.Parallel()
	for name, hops := range map[string]int{"one hop": 1, "two hops": 2} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var proxies []*proxychaintest.Proxy
			var urls []string
			for range hops {
				p := proxychaintest.New(t, false)
				proxies = append(proxies, p)
				urls = append(urls, p.URL)
			}
			chain, err := proxychain.Parse(strings.Join(urls, " "))
			require.NoError(t, err)
			// No target is dialed: the last proxy answers every CONNECT.
			proxies[hops-1].Reject = func(string) (int, http.Header) { return http.StatusForbidden, nil }

			res := netguardtest.NewDNS(t).Set("public.example", []string{"8.8.8.8"}).Set("internal.example", []string{"10.0.0.1"}).Resolver
			client := newGuardedClient(t, destregistry.HTTPClientConfig{Proxy: chain, Guard: &netguard.Guard{Resolver: res}})

			// The proxies are on unallowlisted 127.0.0.1 and are reached.
			_, err = getBody(t, client, "https://public.example/hook")
			require.Error(t, err)
			assert.False(t, netguard.IsAddressNotAllowed(err))
			assert.Equal(t, []string{"public.example:443"}, proxies[hops-1].Connects())

			// A private target is refused before any CONNECT.
			for _, target := range []string{"https://internal.example/hook", "https://10.0.0.1/hook", "https://[::1]/hook"} {
				_, err = getBody(t, client, target)
				require.Error(t, err, target)
				assert.True(t, netguard.IsAddressNotAllowed(err), "%s: %v", target, err)
			}
			assert.Len(t, proxies[hops-1].Connects(), 1)
		})
	}
}

// The guard sits in the transport, so a followed redirect is checked at
// every hop even though the client has no CheckRedirect.
func TestHTTPClientGuard_RedirectToPrivateAddressRefused(t *testing.T) {
	t.Parallel()
	var landed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		landed.Add(1)
	}))
	t.Cleanup(target.Close)
	_, targetPort, _ := net.SplitHostPort(target.Listener.Addr().String())
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://internal.example:"+targetPort+"/landed", http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	// Only the first hop's address is allowlisted.
	res := netguardtest.NewDNS(t).Set("internal.example", []string{"127.0.0.2"}).Resolver
	client := newGuardedClient(t, destregistry.HTTPClientConfig{Guard: guardAllowing(t, res, "127.0.0.1/32")})
	_, err := getBody(t, client, redirector.URL)
	require.Error(t, err)
	assert.True(t, netguard.IsAddressNotAllowed(err))
	assert.Zero(t, landed.Load())
}

// With a forward proxy the guard checks every request, so a redirect to a
// private target is refused before it reaches the proxy.
func TestHTTPClientGuard_ProxyRedirectToPrivateAddressRefused(t *testing.T) {
	t.Parallel()
	var seen atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		http.Redirect(w, r, "http://internal.example/landed", http.StatusFound)
	}))
	t.Cleanup(proxy.Close)
	hop, err := url.Parse(proxy.URL)
	require.NoError(t, err)

	res := netguardtest.NewDNS(t).Set("public.example", []string{"8.8.8.8"}).Set("internal.example", []string{"10.0.0.1"}).Resolver
	client := newGuardedClient(t, destregistry.HTTPClientConfig{Proxy: []*url.URL{hop}, Guard: &netguard.Guard{Resolver: res}})
	_, err = getBody(t, client, "http://public.example/start")
	require.Error(t, err)
	assert.True(t, netguard.IsAddressNotAllowed(err))
	assert.EqualValues(t, 1, seen.Load(), "only the first hop reached the proxy")
}

func TestHTTPClientGuard_NilGuardLeavesTransportAlone(t *testing.T) {
	t.Parallel()
	client, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{})
	require.NoError(t, err)
	assert.Nil(t, client.Transport)

	dest := newDestination(t, false)
	body, err := getBody(t, newGuardedClient(t, destregistry.HTTPClientConfig{OnConnection: func(bool) {}}), dest.URL)
	require.NoError(t, err)
	assert.Equal(t, "ok", body)
}
