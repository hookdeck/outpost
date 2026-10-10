package netguard

import (
	"context"
	"net"
	"net/http"
	"net/netip"
)

// SetDial replaces the final connect of g's DialContext; d carries the
// Control hook that re-checks the connected address.
func SetDial(g *Guard, dial func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error)) {
	g.dial = dial
}

// TransportOf returns the *http.Transport under a NewHTTPClient client.
func TransportOf(c *http.Client) *http.Transport {
	return c.Transport.(*guardTransport).base
}

// HostLimiterEntries is the number of hosts the limiter is tracking.
func HostLimiterEntries(l *HostLimiter) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.inflight)
}

// HostLimiterWaiters is the number of Acquire calls waiting for hostport.
func HostLimiterWaiters(l *HostLimiter, hostport string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if h := l.inflight[hostport]; h != nil {
		return h.waiters.Len()
	}
	return 0
}

// RegistryTable returns the embedded registry rows as prefix → global.
func RegistryTable(v6 bool) map[netip.Prefix]bool {
	blocks := ipv4Special
	if v6 {
		blocks = ipv6Special
	}
	out := make(map[netip.Prefix]bool, len(blocks))
	for _, b := range blocks {
		out[b.prefix] = b.global
	}
	return out
}

const (
	MaxAddrDialTimeout     = maxAddrDialTimeout
	MaxDialAttempts        = maxDialAttempts
	MaxResponseHeaderBytes = maxResponseHeaderBytes
)
