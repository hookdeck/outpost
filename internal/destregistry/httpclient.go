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
}

// NewHTTPClient builds an *http.Client from config. Free function — no
// provider state is involved.
func NewHTTPClient(config HTTPClientConfig) (*http.Client, error) {
	client := &http.Client{}

	if config.Timeout != nil {
		client.Timeout = *config.Timeout
	}

	if config.ProxyURL == nil && config.UserAgent == nil {
		return client, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	var rt http.RoundTripper = transport

	if config.ProxyURL != nil && *config.ProxyURL != "" {
		proxyURLParsed, err := url.Parse(*config.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURLParsed)
		transport.OnProxyConnectResponse = onProxyConnectResponse
		rt = newProxyTransport(rt, proxyURLParsed)
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
