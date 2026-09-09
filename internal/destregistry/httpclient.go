package destregistry

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"
)

type HTTPClientConfig struct {
	Timeout   *time.Duration
	UserAgent *string
	// Proxy lists the forward proxies to go through, nearest first. Empty
	// means a direct connection.
	Proxy []*url.URL
	// WrapTransport, if set, is invoked after a proxy has been installed on
	// the *http.Transport. Callers can use it to attach proxy-specific
	// concerns (e.g. OnProxyConnectResponse callbacks, response classifiers)
	// without bleeding those concerns into destregistry itself. Receives the
	// underlying transport plus the last hop, the one Go's transport talks
	// to; returns the RoundTripper to use thereafter.
	WrapTransport func(*http.Transport, *url.URL) http.RoundTripper

	// Pool sizes the transport's idle connection pool. The zero value leaves
	// Go's defaults in place (2 idle per host, 100 total), which is only
	// appropriate for clients outside the delivery path. Use SizeFanOutPool
	// or SizeSingleHostPool to derive it.
	Pool PoolSizing

	// OnConnection, if set, is invoked once per request with whether the
	// underlying connection was reused. This is the signal that the pool
	// ceiling is binding.
	OnConnection func(reused bool)
}

func (c HTTPClientConfig) needsTransport() bool {
	return len(c.Proxy) > 0 ||
		c.UserAgent != nil ||
		c.OnConnection != nil ||
		c.Pool.MaxIdleConns > 0 ||
		c.Pool.MaxIdleConnsPerHost > 0
}

// NewHTTPClient builds an *http.Client from config. Free function — no
// provider state is involved.
func NewHTTPClient(config HTTPClientConfig) (*http.Client, error) {
	client := &http.Client{}

	if config.Timeout != nil {
		client.Timeout = *config.Timeout
	}

	if !config.needsTransport() {
		return client, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if config.Pool.MaxIdleConns > 0 {
		transport.MaxIdleConns = config.Pool.MaxIdleConns
	}
	if config.Pool.MaxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = config.Pool.MaxIdleConnsPerHost
	}

	var rt http.RoundTripper = transport

	if n := len(config.Proxy); n > 0 {
		last := config.Proxy[n-1]
		transport.Proxy = http.ProxyURL(last)
		if n > 1 {
			// Go talks to the last hop; the dialer gets it there through
			// every hop before it.
			transport.DialContext = newChainDialer(config.Proxy[:n-1], transport.DialContext, func() *tls.Config { return transport.TLSClientConfig }).DialContext
		}
		if config.WrapTransport != nil {
			rt = config.WrapTransport(transport, last)
		}
	}

	if config.UserAgent != nil {
		rt = &userAgentTransport{
			userAgent: *config.UserAgent,
			transport: rt,
		}
	}

	if config.OnConnection != nil {
		rt = &connTraceTransport{onConnection: config.OnConnection, transport: rt}
	}

	client.Transport = rt
	return client, nil
}

// connTraceTransport reports whether each request got a pooled connection or
// had to open a new one. Outermost wrapper so the trace is installed before
// any other RoundTripper runs.
type connTraceTransport struct {
	onConnection func(reused bool)
	transport    http.RoundTripper
}

func (t *connTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			t.onConnection(info.Reused)
		},
	}
	// httptrace composes with any trace already on the context rather than
	// replacing it.
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	return t.transport.RoundTrip(req)
}

// userAgentTransport wraps an http.RoundTripper to inject a User-Agent header
type userAgentTransport struct {
	userAgent string
	transport http.RoundTripper
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", t.userAgent)
	return t.transport.RoundTrip(req)
}
