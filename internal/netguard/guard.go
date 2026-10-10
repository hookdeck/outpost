// Package netguard keeps outbound requests to caller-chosen URLs (MCP
// callbacks) away from internal networks. A URL is vetted when it is
// accepted (Guard.CheckURL) and again on every new connection
// (Guard.DialContext): the host is resolved, every address in the answer must
// be globally reachable per the IANA special-purpose registries or
// allowlisted, and the connection goes to an address that was just checked,
// so a DNS answer that changes in between (rebinding) can't redirect it.
// NewHTTPClient wires the guard into an *http.Client that also never follows
// redirects and never uses an environment proxy.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// maxAddrDialTimeout caps the connect timeout of each address, so a
	// blackholed first A/AAAA record can't eat the whole budget.
	maxAddrDialTimeout = 3 * time.Second
	// defaultDialBudget bounds resolve plus connect when neither the dial
	// context nor the request carries a deadline.
	defaultDialBudget = 30 * time.Second
	// maxDialAttempts bounds how many addresses of one answer are tried.
	// Every address is still checked.
	maxDialAttempts = 8
	dialKeepAlive   = 30 * time.Second
)

// Resolver resolves host names. *net.Resolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Guard decides which addresses outbound connections may reach.
//
// Address policy: every resolved address must be globally reachable
// (IsGlobal) or allowlisted; one bad address fails the whole host.
//
// Scheme policy: https is subject to the address policy only. Plain http is
// allowed only when every address is allowlisted loopback, or, with
// AllowInsecure, allowlisted. Any other scheme is refused.
//
// The zero value is usable: no allowlist and net.DefaultResolver. A Guard is
// safe for concurrent use; don't change its fields once it is in use.
type Guard struct {
	Allowlist *Allowlist
	// Resolver defaults to net.DefaultResolver. Names are always looked up
	// in rooted form ("example.com.") so resolv.conf search domains never
	// turn a public-looking name into an internal one.
	Resolver Resolver
	// AllowInsecure permits plain http to any allowlisted address
	// (MCP_ALLOW_INSECURE_CALLBACKS; development only).
	AllowInsecure bool

	// dial replaces the final connect in tests; d carries the Control hook.
	dial func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error)
}

type dialInfoKey struct{}

// dialInfo is what DialContext learns about the request it dials for.
// http.Transport dials with a context detached from the request's
// cancellation and deadline but keeping its values, so both travel as one.
type dialInfo struct {
	scheme   string
	deadline time.Time
}

// WithScheme tells DialContext which URL scheme a connection is for, and
// records ctx's deadline to size per-address connect timeouts. The client
// from NewHTTPClient does this for every request; only direct users of
// DialContext need it. Without it DialContext applies the http policy.
func WithScheme(ctx context.Context, scheme string) context.Context {
	info := dialInfo{scheme: strings.ToLower(scheme)}
	if d, ok := ctx.Deadline(); ok {
		info.deadline = d
	}
	return context.WithValue(ctx, dialInfoKey{}, info)
}

// CheckURL vets a URL before it is accepted (subscribe time). IP-literal
// hosts are checked as they are; names are resolved and every address in the
// answer must pass. Passing CheckURL guarantees nothing later, since DNS can
// change: DialContext applies the same policy on every new connection.
//
// u should already be normalized (absolute, lowercase ASCII host); a
// non-ASCII host is refused rather than guessed at. Callers must map every
// error (resolver failure, empty answer, non-global address) to the same
// client-visible result, or the check becomes an oracle for internal DNS.
// Errors are *AddressNotAllowedError for policy refusals; resolver errors
// pass through unchanged.
func (g *Guard) CheckURL(ctx context.Context, u *url.URL) error {
	if u == nil {
		return errors.New("netguard: nil URL")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("netguard: URL has no host")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return &AddressNotAllowedError{Host: host, Reason: ReasonHTTPSRequired}
	}
	if err := g.precheckScheme(host, scheme); err != nil {
		return err
	}
	addrs, err := g.resolve(ctx, "ip", host)
	if err != nil {
		return err
	}
	return g.checkAddrs(host, addrs, scheme)
}

// DialContext is an http.Transport DialContext. It resolves the host on
// every call, applies the policy to every address (the scheme comes from
// WithScheme), then connects to the vetted addresses in order with a
// per-address timeout of min(remaining/addresses left, 3s). The TLS server
// name and Host header stay the original host name, since the transport
// derives them from the request URL. As a last line of defence the dialer's
// Control hook re-checks the address the socket actually connects to.
func (g *Guard) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var lookupNetwork string
	switch network {
	case "tcp":
		lookupNetwork = "ip"
	case "tcp4":
		lookupNetwork = "ip4"
	case "tcp6":
		lookupNetwork = "ip6"
	default:
		return nil, &net.OpError{Op: "dial", Net: network, Err: net.UnknownNetworkError(network)}
	}
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: err}
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: fmt.Errorf("invalid port %q", portStr)}
	}

	info, _ := ctx.Value(dialInfoKey{}).(dialInfo)
	deadline := info.deadline
	if d, ok := ctx.Deadline(); ok && (deadline.IsZero() || d.Before(deadline)) {
		deadline = d
	}
	if deadline.IsZero() {
		deadline = time.Now().Add(defaultDialBudget)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	if err := g.precheckScheme(host, info.scheme); err != nil {
		return nil, err
	}
	addrs, err := g.resolve(ctx, lookupNetwork, host)
	if err != nil {
		return nil, err
	}
	if err := g.checkAddrs(host, addrs, info.scheme); err != nil {
		return nil, err
	}

	if len(addrs) > maxDialAttempts {
		addrs = addrs[:maxDialAttempts]
	}
	var firstErr error
	for i, addr := range addrs {
		timeout := time.Until(deadline) / time.Duration(len(addrs)-i)
		if timeout > maxAddrDialTimeout {
			timeout = maxAddrDialTimeout
		}
		conn, err := g.dialAddr(ctx, timeout, network, host, info.scheme, netip.AddrPortFrom(addr.Unmap(), uint16(port)))
		if err == nil {
			return conn, nil
		}
		if notAllowed, ok := AsAddressNotAllowed(err); ok {
			return nil, notAllowed
		}
		if firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, firstErr
}

func (g *Guard) dialAddr(ctx context.Context, timeout time.Duration, network, host, scheme string, target netip.AddrPort) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	d := &net.Dialer{
		KeepAlive: dialKeepAlive,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return &AddressNotAllowedError{Host: host, Reason: ReasonNotGlobal}
			}
			return g.checkAddr(host, ap.Addr(), scheme)
		},
	}
	if g.dial != nil {
		return g.dial(ctx, d, network, target.String())
	}
	return d.DialContext(ctx, network, target.String())
}

// precheckScheme refuses non-https before DNS when no address could pass
// the http policy anyway (the production default: no allowlist).
func (g *Guard) precheckScheme(host, scheme string) error {
	if scheme == "https" || g.Allowlist.permitsHTTP(g.AllowInsecure) {
		return nil
	}
	return &AddressNotAllowedError{Host: host, Reason: ReasonHTTPSRequired}
}

func (g *Guard) resolve(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, errors.New("netguard: empty host")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	for i := 0; i < len(host); i++ {
		if host[i] >= 0x80 {
			return nil, fmt.Errorf("netguard: host %q is not ASCII (IDNA-encode it first)", host)
		}
	}
	fqdn := host
	if !strings.HasSuffix(fqdn, ".") {
		fqdn += "."
	}
	var r Resolver = net.DefaultResolver
	if g.Resolver != nil {
		r = g.Resolver
	}
	addrs, err := r.LookupNetIP(ctx, network, fqdn)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, &AddressNotAllowedError{Host: host, Reason: ReasonNoAddresses}
	}
	return addrs, nil
}

func (g *Guard) checkAddrs(host string, addrs []netip.Addr, scheme string) error {
	for _, addr := range addrs {
		if err := g.checkAddr(host, addr, scheme); err != nil {
			return err
		}
	}
	return nil
}

func (g *Guard) checkAddr(host string, addr netip.Addr, scheme string) error {
	addr = addr.Unmap()
	allowlisted := g.Allowlist.Contains(addr)
	if scheme == "https" {
		if allowlisted || IsGlobal(addr) {
			return nil
		}
		return &AddressNotAllowedError{Host: host, Addr: addr, Reason: ReasonNotGlobal}
	}
	if allowlisted && (g.AllowInsecure || addr.IsLoopback()) {
		return nil
	}
	if !allowlisted && !IsGlobal(addr) {
		return &AddressNotAllowedError{Host: host, Addr: addr, Reason: ReasonNotGlobal}
	}
	return &AddressNotAllowedError{Host: host, Addr: addr, Reason: ReasonHTTPSRequired}
}
