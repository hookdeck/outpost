package destregistry

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ParseProxyURL parses the proxy config value: one or more forward proxy
// URLs separated by whitespace, nearest hop first. An unencoded space is never valid inside a URL, so a
// single-proxy value parses as a one-element chain unchanged. Empty or
// whitespace-only input yields nil (no proxy).
//
// Every hop must be an absolute http or https URL with a host. Errors name
// the offending hop by index and never echo the value, which may carry
// credentials.
func ParseProxyURL(s string) ([]*url.URL, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, nil
	}
	hops := make([]*url.URL, 0, len(fields))
	for i, raw := range fields {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("proxy hop %d: invalid URL", i)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("proxy hop %d: scheme must be http or https", i)
		}
		if u.Host == "" || u.Hostname() == "" {
			return nil, fmt.Errorf("proxy hop %d: missing host", i)
		}
		hops = append(hops, u)
	}
	return hops, nil
}

// RedactedProxyURL renders a proxy hop as scheme://host[:port] for logs and
// error messages, dropping userinfo, path and query.
func RedactedProxyURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ProxyConnectError is returned by the chain dialer when an intermediate hop
// answers a CONNECT with anything but 200. Hop and Next are redacted
// scheme://host:port strings; Header carries the hop's response headers so
// the caller can classify (for Envoy, x-envoy-response-flags).
type ProxyConnectError struct {
	Hop    string
	Next   string
	Status int
	Header http.Header
}

func (e *ProxyConnectError) Error() string {
	return fmt.Sprintf("proxy %s returned %d for CONNECT %s", e.Hop, e.Status, e.Next)
}

// DialFunc matches http.Transport.DialContext.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// chainDialer tunnels through hops 0..n-2 of a proxy chain so the transport,
// whose Proxy is the last hop, ends up talking to that hop over an
// already-established CONNECT tunnel. Go's transport cannot tell the
// returned conn from a direct dial: it still sends its own CONNECT (or
// absolute-URI request) to the last hop, with that hop's credentials.
type chainDialer struct {
	hops []*url.URL
	dial DialFunc
	// tls is read per dial so TLSClientConfig set on the transport after
	// construction (WrapTransport, tests) still applies to https hops.
	tls func() *tls.Config
}

func newChainDialer(hops []*url.URL, dial DialFunc, tlsConfig func() *tls.Config) *chainDialer {
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	return &chainDialer{hops: hops, dial: dial, tls: tlsConfig}
}

// DialContext receives the last hop's host:port from the transport and
// returns a conn tunneled to it through every intermediate hop.
func (d *chainDialer) DialContext(ctx context.Context, network, lastHopAddr string) (net.Conn, error) {
	first := d.hops[0]
	conn, err := d.dial(ctx, network, proxyHostPort(first))
	if err != nil {
		return nil, err
	}
	if first.Scheme == "https" {
		if conn, err = d.wrapTLS(ctx, conn, first.Hostname()); err != nil {
			return nil, err
		}
	}

	for i, hop := range d.hops {
		var next *url.URL
		nextAddr := lastHopAddr
		if i+1 < len(d.hops) {
			next = d.hops[i+1]
			nextAddr = proxyHostPort(next)
		}
		if err := connectThrough(ctx, conn, hop, nextAddr); err != nil {
			conn.Close()
			return nil, err
		}
		if next != nil && next.Scheme == "https" {
			if conn, err = d.wrapTLS(ctx, conn, next.Hostname()); err != nil {
				return nil, err
			}
		}
	}
	return conn, nil
}

// wrapTLS starts TLS on conn to a proxy hop. On failure the conn is closed.
func (d *chainDialer) wrapTLS(ctx context.Context, conn net.Conn, serverName string) (net.Conn, error) {
	var cfg *tls.Config
	if d.tls != nil {
		cfg = d.tls()
	}
	if cfg == nil {
		cfg = &tls.Config{}
	} else {
		cfg = cfg.Clone()
	}
	if cfg.ServerName == "" {
		cfg.ServerName = serverName
	}
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tc, nil
}

// connectThrough sends one CONNECT for target over conn, which is already
// connected to hop, and requires a 200. Mirrors the transport's own CONNECT:
// nothing after the status line and headers is consumed.
func connectThrough(ctx context.Context, conn net.Conn, hop *url.URL, target string) error {
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: target},
		Host:   target,
		Header: http.Header{},
	}
	if u := hop.User; u != nil {
		pass, _ := u.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pass)))
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}
	if err := req.Write(conn); err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return &ProxyConnectError{
			Hop:    RedactedProxyURL(hop),
			Next:   target,
			Status: resp.StatusCode,
			Header: resp.Header,
		}
	}
	return nil
}

// proxyHostPort returns host:port for a hop, defaulting the port by scheme
// the way the transport does for its own proxy.
func proxyHostPort(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == "https" {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}
