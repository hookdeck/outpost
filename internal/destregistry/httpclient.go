package destregistry

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type HTTPClientConfig struct {
	Timeout   *time.Duration
	UserAgent *string
	ProxyURL  *string
	// MaxIdleConnsPerHost caps the idle connections kept per destination host.
	// Go's default is 2, which is far too low for delivery: above two concurrent
	// deliveries to one host the surplus connections are closed rather than
	// pooled, so nearly every delivery pays a fresh TCP and TLS handshake and
	// burns an ephemeral port on the sending host. nil selects
	// defaultMaxIdleConnsPerHost.
	MaxIdleConnsPerHost *int
	// MaxIdleConns caps idle connections across all hosts. nil selects
	// defaultMaxIdleConns. Zero means unlimited.
	MaxIdleConns *int
	// MaxConnsPerHost caps total (active plus idle) connections per host, and
	// blocks dials beyond it. nil selects defaultMaxConnsPerHost. Zero means
	// unlimited, which is the default — a cap here converts destination
	// slowness into head-of-line blocking across every tenant sharing the host.
	MaxConnsPerHost *int
	// WrapTransport, if set, is invoked after a proxy URL has been installed
	// on the *http.Transport. Callers can use it to attach proxy-specific
	// concerns (e.g. OnProxyConnectResponse callbacks, response classifiers)
	// without bleeding those concerns into destregistry itself. Receives the
	// underlying transport plus the parsed proxy URL; returns the
	// RoundTripper to use thereafter.
	WrapTransport func(*http.Transport, *url.URL) http.RoundTripper
}

// Connection-pool defaults for the delivery client.
//
// Go's own defaults assume a client making occasional calls to many hosts.
// Delivery is the opposite: sustained concurrent requests to comparatively few
// hosts. Sizing the per-host idle pool to the concurrency a single destination
// is expected to carry keeps connections warm instead of reopening them.
const (
	defaultMaxIdleConnsPerHost = 512
	defaultMaxIdleConns        = 0 // unlimited; per-host is the meaningful bound
	defaultMaxConnsPerHost     = 0 // unlimited; see MaxConnsPerHost
)

func intOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

// NewHTTPClient builds an *http.Client from config. Free function — no
// provider state is involved.
func NewHTTPClient(config HTTPClientConfig) (*http.Client, error) {
	client := &http.Client{}

	if config.Timeout != nil {
		client.Timeout = *config.Timeout
	}

	// Always build our own transport. Returning a bare client here would leave
	// it on http.DefaultTransport, whose MaxIdleConnsPerHost of 2 is the whole
	// problem the pool settings below exist to avoid.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = intOr(config.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost)
	transport.MaxIdleConns = intOr(config.MaxIdleConns, defaultMaxIdleConns)
	transport.MaxConnsPerHost = intOr(config.MaxConnsPerHost, defaultMaxConnsPerHost)

	var rt http.RoundTripper = transport

	if config.ProxyURL != nil && *config.ProxyURL != "" {
		proxyURLParsed, err := url.Parse(*config.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURLParsed)
		if config.WrapTransport != nil {
			rt = config.WrapTransport(transport, proxyURLParsed)
		}
	}

	if config.UserAgent != nil {
		rt = &userAgentTransport{
			userAgent: *config.UserAgent,
			transport: rt,
		}
	}

	client.Transport = rt
	return client, nil
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
