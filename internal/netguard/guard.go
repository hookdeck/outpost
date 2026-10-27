// Package netguard keeps outbound requests to user-supplied URLs away from
// internal networks. A URL is vetted when it is accepted (Guard.CheckURL)
// and again on every new connection (Guard.DialContext): every address a
// connection is about to be made to must be globally reachable per the IANA
// special-purpose registries or allowlisted, so a DNS answer that changes in
// between (rebinding) can't send it to a blocked address. Non-ASCII host names must be given
// in punycode.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	dialTimeout   = 30 * time.Second
	dialKeepAlive = 30 * time.Second
)

// Resolver resolves host names. *net.Resolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Guard decides which addresses outbound connections may reach: globally
// reachable ones (IsGlobal) and allowlisted ones. CheckURL fails a host if
// any resolved address is refused; DialContext skips refused addresses and
// fails only if none is left. Scheme and port don't matter.
//
// The zero value is usable: no allowlist and net.DefaultResolver. A Guard is
// safe for concurrent use; don't change its fields once it is in use.
type Guard struct {
	Allowlist *Allowlist
	// Resolver defaults to net.DefaultResolver. Names are always looked up
	// in rooted form ("example.com."), so CheckURL and DialContext resolve
	// the same name regardless of resolv.conf search domains. DialContext
	// uses it only if it is a *net.Resolver.
	Resolver Resolver
}

// CheckURL vets a URL before it is accepted. IP-literal hosts are checked as
// they are; names are resolved and every address in the answer must pass.
// Passing CheckURL guarantees nothing later, since DNS can change:
// DialContext checks again on every new connection.
//
// A non-ASCII host is refused rather than guessed at. Callers must map every
// error (resolver failure, empty answer, non-global address) to the same
// client-visible result, or the check becomes an oracle for internal DNS.
// Policy refusals are *AddressNotAllowedError; resolver errors pass through
// unchanged.
func (g *Guard) CheckURL(ctx context.Context, u *url.URL) error {
	if u == nil {
		return errors.New("netguard: nil URL")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("netguard: URL has no host")
	}
	addrs, err := g.resolve(ctx, host)
	if err != nil {
		return err
	}
	return g.checkAddrs(host, addrs)
}

// DialContext is an http.Transport DialContext: a net.Dialer with the
// default transport's timeouts whose Control hook refuses each address
// before the socket connects to it. The TLS server name and Host header
// stay the original host name, since the transport derives them from the
// request URL.
func (g *Guard) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, &net.OpError{Op: "dial", Net: network, Err: net.UnknownNetworkError(network)}
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: err}
	}
	if _, err := netip.ParseAddr(host); err != nil {
		fqdn, err := rootedHost(host)
		if err != nil {
			return nil, &net.OpError{Op: "dial", Net: network, Err: err}
		}
		address = net.JoinHostPort(fqdn, port)
	}
	d := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialKeepAlive,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return &AddressNotAllowedError{Host: host, Reason: ReasonNotGlobal}
			}
			return g.checkAddr(host, ap.Addr())
		},
	}
	if r, ok := g.Resolver.(*net.Resolver); ok {
		d.Resolver = r
	}
	return d.DialContext(ctx, network, address)
}

// rootedHost returns host in rooted form, refusing non-ASCII names.
func rootedHost(host string) (string, error) {
	for i := 0; i < len(host); i++ {
		if host[i] >= 0x80 {
			return "", fmt.Errorf("netguard: host %q is not ASCII (IDNA-encode it first)", host)
		}
	}
	if !strings.HasSuffix(host, ".") {
		host += "."
	}
	return host, nil
}

func (g *Guard) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, errors.New("netguard: empty host")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	fqdn, err := rootedHost(host)
	if err != nil {
		return nil, err
	}
	var r Resolver = net.DefaultResolver
	if g.Resolver != nil {
		r = g.Resolver
	}
	addrs, err := r.LookupNetIP(ctx, "ip", fqdn)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, &AddressNotAllowedError{Host: host, Reason: ReasonNoAddresses}
	}
	return addrs, nil
}

func (g *Guard) checkAddrs(host string, addrs []netip.Addr) error {
	for _, addr := range addrs {
		if err := g.checkAddr(host, addr); err != nil {
			return err
		}
	}
	return nil
}

func (g *Guard) checkAddr(host string, addr netip.Addr) error {
	addr = addr.Unmap()
	if g.Allowlist.Contains(addr) || IsGlobal(addr) {
		return nil
	}
	return &AddressNotAllowedError{Host: host, Addr: addr, Reason: ReasonNotGlobal}
}
