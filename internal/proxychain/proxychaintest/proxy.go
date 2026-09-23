// Package proxychaintest provides an in-process HTTP CONNECT forward proxy
// for tests.
package proxychaintest

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// Proxy is an httptest forward proxy: CONNECT is hijacked and piped to the
// requested authority, plain requests are forwarded absolute-URI style. It
// records every CONNECT authority and Proxy-Authorization it saw.
type Proxy struct {
	*httptest.Server
	mu       sync.Mutex
	connects []string
	auths    []string
	// Reject, if set, is consulted for each CONNECT; a non-zero status short
	// circuits with that status and headers.
	Reject func(target string) (int, http.Header)
	// Hold, if set, is closed by the test to release a CONNECT that is
	// deliberately left unanswered.
	Hold chan struct{}
}

// New starts a Proxy, closed on test cleanup.
func New(t *testing.T, useTLS bool) *Proxy {
	t.Helper()
	p := &Proxy{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			out, err := http.DefaultTransport.RoundTrip(&http.Request{
				Method: r.Method, URL: r.URL, Header: r.Header, Body: r.Body, Host: r.Host,
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer out.Body.Close()
			for k, v := range out.Header {
				w.Header()[k] = v
			}
			w.WriteHeader(out.StatusCode)
			_, _ = io.Copy(w, out.Body)
			return
		}
		p.mu.Lock()
		p.connects = append(p.connects, r.Host)
		p.auths = append(p.auths, r.Header.Get("Proxy-Authorization"))
		p.mu.Unlock()

		if p.Hold != nil {
			<-p.Hold
		}
		if p.Reject != nil {
			if status, h := p.Reject(r.Host); status != 0 {
				for k, v := range h {
					w.Header()[k] = v
				}
				w.WriteHeader(status)
				return
			}
		}
		upstream, err := net.DialTimeout("tcp", r.Host, 5*time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("proxychaintest: response writer is not a Hijacker")
			return
		}
		w.WriteHeader(http.StatusOK)
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("proxychaintest: hijack: %v", err)
			return
		}
		defer conn.Close()
		_ = buf.Flush()
		done := make(chan struct{}, 2)
		// Read through buf: the server may already hold the client's first
		// post-CONNECT bytes there, and reading the raw conn would drop them.
		go func() { _, _ = io.Copy(upstream, buf); done <- struct{}{} }()
		go func() { _, _ = io.Copy(conn, upstream); done <- struct{}{} }()
		<-done
	})
	if useTLS {
		p.Server = httptest.NewTLSServer(handler)
	} else {
		p.Server = httptest.NewServer(handler)
	}
	t.Cleanup(p.Close)
	return p
}

// Connects returns every CONNECT authority seen so far.
func (p *Proxy) Connects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.connects...)
}

// Auths returns the Proxy-Authorization header of every CONNECT seen so far.
func (p *Proxy) Auths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.auths...)
}

// WithCreds rewrites a server URL to carry userinfo.
func WithCreds(rawURL, user, pass string) string {
	u, _ := url.Parse(rawURL)
	u.User = url.UserPassword(user, pass)
	return u.String()
}

// HostOf returns the host:port of a URL.
func HostOf(rawURL string) string {
	u, _ := url.Parse(rawURL)
	return u.Host
}
