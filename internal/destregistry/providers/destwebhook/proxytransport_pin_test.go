package destwebhook_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/stretchr/testify/require"
)

// TestProxyTransport_PinProxyconnectWording guards a load-bearing assumption
// in proxytransport.go: Go's net/http transport wraps dial-to-proxy failures
// in a *net.OpError whose Op field is "proxyconnect", and that token appears
// in the resulting error's Error() string. This is not a public/typed API —
// it's an internal stdlib convention that has been stable for many Go
// versions but could change.
//
// If this test fails after a Go upgrade, the proxyTransport.classifyTransportError
// detector for proxy-unreachable failures needs to be updated to whatever the
// new wording is. Without this pin, that breakage would be silent and result
// in proxy outages no longer being attributed to infra (instead they would
// fall through and be classified as ordinary network errors against the
// destination).
//
// Pinned for go version recorded in go.mod.
func TestProxyTransport_PinProxyconnectWording(t *testing.T) {
	t.Parallel()

	// Bind a port, then close — guaranteed unbound for the rest of the test.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	unreachable := srv.URL
	srv.Close()

	// Bypass the wrapper: build a raw transport so we observe the stdlib
	// error directly.
	rawTransport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	rt := rawTransport.Clone()
	proxyURL, err := url.Parse(unreachable)
	require.NoError(t, err)
	rt.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: rt}

	_, err = client.Get("https://example.invalid/")
	require.Error(t, err)
	require.True(t,
		strings.Contains(err.Error(), "proxyconnect"),
		"stdlib changed CONNECT-failure wording; update proxyTransport detector. err = %q", err.Error(),
	)

	// Also pin: the wrapper still classifies this as ErrProxyInfra.
	wrappedClient := makeProxiedClient(t, unreachable)
	_, err = wrappedClient.Get("https://example.invalid/")
	require.Error(t, err)
	require.True(t, destwebhook.IsProxyInfraError(err),
		"wrapper failed to identify proxy-unreachable as infra; err = %v", err)
}
