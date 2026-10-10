package netguard

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"
)

const (
	tlsHandshakeTimeout    = 10 * time.Second
	responseHeaderTimeout  = 8 * time.Second
	maxResponseHeaderBytes = 64 << 10
	proxyDialTimeout       = 30 * time.Second
)

// ClientConfig configures NewHTTPClient.
type ClientConfig struct {
	// Guard vets every connection (or, with ProxyURL, every target URL).
	// Required.
	Guard *Guard
	// UserAgent, if set, replaces the User-Agent of every request.
	UserAgent string
	// ProxyURL, if set, sends every request through this forward proxy
	// (http, https, socks5 or socks5h). Each target URL still goes through
	// Guard.CheckURL before the request leaves, but the proxy resolves and
	// connects on its own, so pinning the vetted address is the proxy's job.
	// Dialing the proxy itself is not guarded. Without it no proxy is used:
	// HTTP_PROXY/HTTPS_PROXY are never consulted.
	ProxyURL *url.URL
	// MaxIdleConns and MaxIdleConnsPerHost size the idle pool. Zero keeps
	// Go's defaults (100 total, 2 per host).
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	// RootCAs replaces the system roots. Tests only.
	RootCAs *x509.CertPool
	// OnConnection, if set, is called once per request with whether the
	// connection came from the idle pool.
	OnConnection func(reused bool)
}

// NewHTTPClient builds an *http.Client for caller-chosen URLs: connections
// go through Guard.DialContext (or a pre-flight Guard.CheckURL with
// ProxyURL), redirects are returned to the caller instead of followed,
// environment proxies are ignored, and response headers are capped at 64 KiB
// with an 8s wait for them (TLS handshake 10s). There is no overall client
// timeout: callers bound each request with its context, whose deadline also
// sizes the per-address connect timeouts. Responses are not transparently
// decompressed.
func NewHTTPClient(cfg ClientConfig) (*http.Client, error) {
	if cfg.Guard == nil {
		return nil, errors.New("netguard: ClientConfig.Guard is required")
	}
	transport := &http.Transport{
		// Proxy stays nil unless ProxyURL is set: an HTTPS_PROXY in the
		// environment must never carry callback traffic around the guard.
		Proxy:                  nil,
		DialContext:            cfg.Guard.DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           100,
		MaxIdleConnsPerHost:    cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    tlsHandshakeTimeout,
		ExpectContinueTimeout:  1 * time.Second,
		ResponseHeaderTimeout:  responseHeaderTimeout,
		MaxResponseHeaderBytes: maxResponseHeaderBytes,
		// Receivers' responses are small and mostly discarded; asking for
		// gzip only adds decompression work on attacker-chosen bytes.
		DisableCompression: true,
		TLSClientConfig:    &tls.Config{RootCAs: cfg.RootCAs},
	}
	if cfg.MaxIdleConns > 0 {
		transport.MaxIdleConns = cfg.MaxIdleConns
	}
	rt := &guardTransport{
		base:         transport,
		guard:        cfg.Guard,
		userAgent:    cfg.UserAgent,
		onConnection: cfg.OnConnection,
	}
	if cfg.ProxyURL != nil {
		switch cfg.ProxyURL.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			// Never echo the URL: it may carry credentials.
			return nil, errors.New("netguard: proxy URL scheme must be http, https, socks5 or socks5h")
		}
		if cfg.ProxyURL.Hostname() == "" {
			return nil, errors.New("netguard: proxy URL has no host")
		}
		transport.Proxy = http.ProxyURL(cfg.ProxyURL)
		transport.DialContext = (&net.Dialer{Timeout: proxyDialTimeout, KeepAlive: dialKeepAlive}).DialContext
		rt.preflight = true
	}
	return &http.Client{
		Transport: rt,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// guardTransport passes the request's scheme and deadline to the dialer,
// runs the pre-flight check in proxy mode, and applies the User-Agent and
// connection observer.
type guardTransport struct {
	base         *http.Transport
	guard        *Guard
	preflight    bool
	userAgent    string
	onConnection func(reused bool)
}

func (t *guardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, errors.New("netguard: nil Request.URL")
	}
	ctx := WithScheme(req.Context(), req.URL.Scheme)
	if t.preflight {
		if err := t.guard.CheckURL(ctx, req.URL); err != nil {
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, err
		}
	}
	if t.onConnection != nil {
		onConnection := t.onConnection
		// A fresh trace per request: WithClientTrace composes by mutating
		// the trace it is given.
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { onConnection(info.Reused) },
		})
	}
	// RoundTrippers must not modify the caller's request.
	out := req.WithContext(ctx)
	if t.userAgent != "" {
		out.Header = req.Header.Clone()
		if out.Header == nil {
			out.Header = make(http.Header)
		}
		out.Header.Set("User-Agent", t.userAgent)
	}
	return t.base.RoundTrip(out)
}

// CloseIdleConnections lets http.Client.CloseIdleConnections reach the
// underlying transport.
func (t *guardTransport) CloseIdleConnections() {
	t.base.CloseIdleConnections()
}
