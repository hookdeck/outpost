package destregistry_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCONNECTRejectingProxy returns an httptest server that responds to any
// CONNECT request with the given status code. Non-CONNECT requests get 200.
func newCONNECTRejectingProxy(t *testing.T, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}

// makeProxiedClient constructs an HTTP client routed through proxyURL using
// the proxyTransport wrapper, matching the MakeHTTPClient flow.
func makeProxiedClient(t *testing.T, proxyURL string) *http.Client {
	t.Helper()
	provider, err := newMockProvider()
	require.NoError(t, err)

	client, err := provider.MakeHTTPClient(destregistry.HTTPClientConfig{
		ProxyURL: &proxyURL,
	})
	require.NoError(t, err)
	return client
}

func TestProxyTransport_ConnectAuthFailure_ReturnsInfraError(t *testing.T) {
	t.Parallel()

	proxy := newCONNECTRejectingProxy(t, http.StatusProxyAuthRequired)
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)

	// HTTPS target triggers CONNECT.
	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var infraErr *destregistry.ErrProxyInfra
	require.True(t, errors.As(err, &infraErr),
		"expected ErrProxyInfra for proxy 407, got: %v", err)
	assert.Equal(t, "example.invalid", infraErr.DestHost)
}

func TestProxyTransport_ConnectBadGateway_ReturnsDestinationError(t *testing.T) {
	t.Parallel()

	proxy := newCONNECTRejectingProxy(t, http.StatusBadGateway)
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)

	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var destErr *destregistry.ErrProxyDestination
	require.True(t, errors.As(err, &destErr),
		"expected ErrProxyDestination for proxy 502, got: %v", err)
	assert.Equal(t, "connection_refused", destErr.Code)
	assert.Equal(t, "example.invalid", destErr.DestHost)

	// Must not be ErrProxyInfra (would cause incorrect nack).
	var infraErr *destregistry.ErrProxyInfra
	assert.False(t, errors.As(err, &infraErr),
		"5xx from proxy must not be infra error")
}

func TestProxyTransport_ConnectServiceUnavailable_ReturnsDestinationError(t *testing.T) {
	t.Parallel()

	proxy := newCONNECTRejectingProxy(t, http.StatusServiceUnavailable)
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)

	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var destErr *destregistry.ErrProxyDestination
	require.True(t, errors.As(err, &destErr),
		"expected ErrProxyDestination for proxy 503, got: %v", err)
	assert.Equal(t, "connection_refused", destErr.Code)
}

func TestProxyTransport_ProxyUnreachable_ReturnsInfraError(t *testing.T) {
	t.Parallel()

	// Bind a port, then close — the address is guaranteed reserved-but-unbound
	// for the rest of the test (avoids hard-coded "unlikely" ports).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	unreachableURL := srv.URL
	srv.Close()

	client := makeProxiedClient(t, unreachableURL)

	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var infraErr *destregistry.ErrProxyInfra
	require.True(t, errors.As(err, &infraErr),
		"expected ErrProxyInfra when proxy is unreachable, got: %v", err)
}

func TestProxyTransport_SuccessfulRequest_PassesThrough(t *testing.T) {
	t.Parallel()

	// Plain-HTTP target so we exercise the proxy's forward path (no CONNECT).
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test", "real-upstream")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// Minimal forward proxy: the client sends absolute-URI requests, so r.URL
	// is the actual target. We just re-dispatch.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		for k, vs := range r.Header {
			for _, v := range vs {
				outReq.Header.Add(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(outReq)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
	}))
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)

	resp, err := client.Get(target.URL + "/somepath")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "real-upstream", resp.Header.Get("X-Test"))
}

func TestProxyTransport_ErrProxyInfra_DoesNotLeakProxyDetails(t *testing.T) {
	t.Parallel()

	proxy := newCONNECTRejectingProxy(t, http.StatusProxyAuthRequired)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)

	client := makeProxiedClient(t, proxy.URL)

	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var infraErr *destregistry.ErrProxyInfra
	require.True(t, errors.As(err, &infraErr))

	// Sanitized message must not contain the proxy host/port.
	assert.NotContains(t, infraErr.Error(), proxyURL.Host,
		"ErrProxyInfra.Error() must not leak proxy address")
	assert.Contains(t, infraErr.Error(), "example.invalid",
		"ErrProxyInfra.Error() should reference the destination host")
}

func TestMapEnvoyResponseFlag(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"UF":      "connection_refused",
		"UH":      "connection_refused",
		"UC":      "connection_refused",
		"LH":      "connection_refused",
		"LR":      "connection_refused",
		"UT":      "timeout",
		"SI":      "timeout",
		"DT":      "timeout",
		"DC":      "dns_error",
		"NR":      "network_unreachable",
		"NC":      "network_unreachable",
		"":        "network_error",
		"unknown": "network_error",
	}
	for flag, want := range cases {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, destregistry.MapEnvoyResponseFlag(flag))
		})
	}
}
