package destwebhook_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
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
// the proxyTransport wrapper, matching the NewHTTPClient flow.
func makeProxiedClient(t *testing.T, proxyURL string) *http.Client {
	t.Helper()
	client, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{
		ProxyURL:      &proxyURL,
		WrapTransport: destwebhook.WrapTransport,
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

	var infraErr *destwebhook.ErrProxyInfra
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

	var destErr *destwebhook.ErrProxyDestination
	require.True(t, errors.As(err, &destErr),
		"expected ErrProxyDestination for proxy 502, got: %v", err)
	assert.Equal(t, "connection_refused", destErr.Code)
	assert.Equal(t, "example.invalid", destErr.DestHost)

	// Must not be ErrProxyInfra (would cause incorrect nack).
	var infraErr *destwebhook.ErrProxyInfra
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

	var destErr *destwebhook.ErrProxyDestination
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

	var infraErr *destwebhook.ErrProxyInfra
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

	var infraErr *destwebhook.ErrProxyInfra
	require.True(t, errors.As(err, &infraErr))

	// Sanitized message must not contain the proxy host/port.
	assert.NotContains(t, infraErr.Error(), proxyURL.Host,
		"ErrProxyInfra.Error() must not leak proxy address")
	assert.Contains(t, infraErr.Error(), "example.invalid",
		"ErrProxyInfra.Error() should reference the destination host")
}

// newEnvoySynthesizedProxy returns a forwarding proxy that, instead of
// forwarding, responds with an Envoy-synthesized 5xx and the configured
// response flag.
func newEnvoySynthesizedProxy(t *testing.T, status int, flag string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("server", "envoy")
		w.Header().Set("x-envoy-response-flags", flag)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("upstream connect error or disconnect/reset before headers"))
	}))
}

func TestProxyTransport_EnvoySynthesizedResponse_UF_ReturnsConnectionRefused(t *testing.T) {
	t.Parallel()

	proxy := newEnvoySynthesizedProxy(t, http.StatusBadGateway, "UF")
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)
	_, err := client.Get("http://example.invalid/hook")
	require.Error(t, err)

	var destErr *destwebhook.ErrProxyDestination
	require.True(t, errors.As(err, &destErr),
		"expected ErrProxyDestination for envoy UF flag, got: %v", err)
	assert.Equal(t, "connection_refused", destErr.Code)
	assert.Equal(t, "example.invalid", destErr.DestHost)
}

func TestProxyTransport_EnvoySynthesizedResponse_UT_ReturnsTimeout(t *testing.T) {
	t.Parallel()

	proxy := newEnvoySynthesizedProxy(t, http.StatusGatewayTimeout, "UT")
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)
	_, err := client.Get("http://example.invalid/hook")
	require.Error(t, err)

	var destErr *destwebhook.ErrProxyDestination
	require.True(t, errors.As(err, &destErr))
	assert.Equal(t, "timeout", destErr.Code)
}

func TestProxyTransport_EnvoySynthesizedResponse_DC_ReturnsDNSError(t *testing.T) {
	t.Parallel()

	proxy := newEnvoySynthesizedProxy(t, http.StatusServiceUnavailable, "DC")
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)
	_, err := client.Get("http://example.invalid/hook")
	require.Error(t, err)

	var destErr *destwebhook.ErrProxyDestination
	require.True(t, errors.As(err, &destErr))
	assert.Equal(t, "dns_error", destErr.Code)
}

func TestProxyTransport_EnvoyPlaceholderFlag_PassesResponseThrough(t *testing.T) {
	t.Parallel()

	// "-" is the placeholder Envoy emits when no flag fired (pass-through
	// success path). Must not be treated as synthesized.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-envoy-response-flags", "-")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("real upstream body"))
	}))
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)
	resp, err := client.Get("http://example.invalid/hook")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "real upstream body", string(body))
	// Header stripped on success.
	assert.Empty(t, resp.Header.Get("x-envoy-response-flags"))
}

func TestProxyTransport_StripsEnvoyHeaders_OnSuccess(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("server", "envoy")
		w.Header().Set("x-envoy-upstream-service-time", "12")
		w.Header().Set("x-envoy-attempt-count", "1")
		w.Header().Set("x-real-header", "kept")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)
	resp, err := client.Get("http://example.invalid/hook")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Empty(t, resp.Header.Get("server"))
	assert.Empty(t, resp.Header.Get("x-envoy-upstream-service-time"))
	assert.Empty(t, resp.Header.Get("x-envoy-attempt-count"))
	assert.Equal(t, "kept", resp.Header.Get("x-real-header"))
}

func TestProxyTransport_EnvoyConnectFlag_RefinesInfraErrorCode(t *testing.T) {
	t.Parallel()

	// Envoy CONNECT failure with a response-flag header. The
	// OnProxyConnectResponse callback sees the headers and refines the
	// destination error code from the flag instead of the generic default.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.Header().Set("x-envoy-response-flags", "DC")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	client := makeProxiedClient(t, proxy.URL)
	_, err := client.Get("https://example.invalid/")
	require.Error(t, err)

	var destErr *destwebhook.ErrProxyDestination
	require.True(t, errors.As(err, &destErr),
		"expected ErrProxyDestination, got: %v", err)
	assert.Equal(t, "dns_error", destErr.Code,
		"envoy response-flag DC should refine generic connection_refused to dns_error")
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
		"unknown": "network_error",
	}
	for flag, want := range cases {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, destwebhook.MapEnvoyResponseFlag(flag))
		})
	}
}
