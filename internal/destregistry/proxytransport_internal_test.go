package destregistry

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestProxyTransport_HTTPSResponse_PassesEnvoyHeadersThrough is the core
// regression test for destinations that themselves sit behind Envoy. After
// CONNECT succeeds the forward proxy is byte-blind to the response, so any
// envoy headers we see belong to the destination. The wrapper must not
// touch them.
//
// Constructed via stubbed RoundTripper rather than a real TLS-capable proxy
// because the only contract under test is "do not modify the response when
// scheme is https".
func TestProxyTransport_HTTPSResponse_PassesEnvoyHeadersThrough(t *testing.T) {
	t.Parallel()

	stub := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("server", "envoy")
		h.Set("x-envoy-response-flags", "UF")
		h.Set("x-envoy-upstream-service-time", "42")
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader("destination's real 503 body")),
			Request:    req,
		}, nil
	})
	proxyURL, _ := url.Parse("http://example.proxy:8080")
	wrapper := newProxyTransport(stub, proxyURL)

	req, err := http.NewRequest(http.MethodPost, "https://api.dest.example/hook", nil)
	require.NoError(t, err)

	resp, err := wrapper.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "destination's real 503 body", string(body),
		"destination's response body must reach Outpost unchanged")

	assert.Equal(t, "envoy", resp.Header.Get("server"),
		"destination's server header must not be stripped")
	assert.Equal(t, "UF", resp.Header.Get("x-envoy-response-flags"),
		"destination's response-flag header must survive (it's their observability data)")
	assert.Equal(t, "42", resp.Header.Get("x-envoy-upstream-service-time"),
		"destination's upstream-service-time must survive")
}

// TestProxyTransport_PlainHTTPResponse_StillStripsEnvoyHeaders asserts the
// counterpart: on the plain-HTTP forwarding path our forward proxy *is* in
// the byte path on the response, so envoy-fingerprint stripping still
// applies. This documents the residual limitation noted in
// webhook-proxy.mdoc: a plain-HTTP destination that is itself behind Envoy
// will have some observability headers stripped here. Attribution stays
// correct because x-envoy-response-flags is overwritten by the forward
// Envoy via OVERWRITE_IF_EXISTS_OR_ADD.
func TestProxyTransport_PlainHTTPResponse_StillStripsEnvoyHeaders(t *testing.T) {
	t.Parallel()

	stub := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("server", "envoy")
		h.Set("x-envoy-response-flags", "-") // pass-through placeholder (set by our forward Envoy)
		h.Set("x-envoy-upstream-service-time", "42")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})
	proxyURL, _ := url.Parse("http://example.proxy:8080")
	wrapper := newProxyTransport(stub, proxyURL)

	req, err := http.NewRequest(http.MethodPost, "http://api.dest.example/hook", nil)
	require.NoError(t, err)

	resp, err := wrapper.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Empty(t, resp.Header.Get("server"))
	assert.Empty(t, resp.Header.Get("x-envoy-response-flags"))
	assert.Empty(t, resp.Header.Get("x-envoy-upstream-service-time"))
}
