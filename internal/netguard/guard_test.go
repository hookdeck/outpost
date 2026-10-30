package netguard_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/netguard/netguardtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver answers CheckURL lookups from a table keyed by rooted name
// and records every query.
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]string
	errs    map[string]error
	queries []string
}

func newResolver() *fakeResolver {
	return &fakeResolver{answers: map[string][]string{}, errs: map[string]error{}}
}

// set maps host (without the trailing dot) to its answer.
func (r *fakeResolver) set(host string, answer []string) *fakeResolver {
	r.answers[host+"."] = answer
	return r
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, host)
	if err, ok := r.errs[host]; ok {
		return nil, err
	}
	answer, ok := r.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	var out []netip.Addr
	for _, s := range answer {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

func (r *fakeResolver) Queries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.queries...)
}

func mustAllowlist(t *testing.T, entries ...string) *netguard.Allowlist {
	t.Helper()
	al, err := netguard.ParseAllowlist(entries)
	require.NoError(t, err)
	return al
}

func requireNotAllowed(t *testing.T, err error, reason string) *netguard.AddressNotAllowedError {
	t.Helper()
	require.Error(t, err)
	e, ok := netguard.AsAddressNotAllowed(err)
	require.True(t, ok, "want *AddressNotAllowedError, got %T: %v", err, err)
	assert.Equal(t, reason, e.Reason)
	assert.True(t, netguard.IsAddressNotAllowed(err))
	return e
}

func TestCheckURL_NoAllowlist(t *testing.T) {
	t.Parallel()
	res := newResolver().
		set("public.example", []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"}).
		set("private.example", []string{"10.0.0.5"}).
		set("mixed.example", []string{"93.184.216.34", "10.0.0.5"}).
		set("mixed6.example", []string{"2606:2800:220:1::1", "fd00::1"}).
		set("metadata.example", []string{"169.254.169.254"}).
		set("mapped.example", []string{"::ffff:10.0.0.1"}).
		set("nat64.example", []string{"64:ff9b::7f00:1"}).
		set("sixtofour.example", []string{"2002:a00:1::1"}).
		set("loopback.example", []string{"127.0.0.1"}).
		set("empty.example", []string{}).
		set("localhost.example", []string{"127.0.0.1"})
	res.errs["servfail.example."] = &net.DNSError{Err: "server misbehaving", Name: "servfail.example.", IsTemporary: true}
	g := &netguard.Guard{Resolver: res}

	cases := []struct {
		url    string
		reason string // "" = allowed; "dns" = resolver error passes through
		addr   string
	}{
		{"https://8.8.8.8/", "", ""},
		{"https://[2001:4860:4860::8888]:8443/x", "", ""},
		{"https://[::ffff:8.8.8.8]/", "", ""},
		{"https://public.example/hook", "", ""},
		{"https://public.example./hook", "", ""},
		{"https://10.0.0.1/", netguard.ReasonNotGlobal, "10.0.0.1"},
		{"https://127.0.0.1:8080/", netguard.ReasonNotGlobal, "127.0.0.1"},
		{"https://0.0.0.0/", netguard.ReasonNotGlobal, "0.0.0.0"},
		{"https://[::1]/", netguard.ReasonNotGlobal, "::1"},
		{"https://[::ffff:127.0.0.1]/", netguard.ReasonNotGlobal, "127.0.0.1"},
		{"https://[fe80::1%25eth0]/", netguard.ReasonNotGlobal, "fe80::1%eth0"},
		{"https://[::]/", netguard.ReasonNotGlobal, "::"},
		{"https://255.255.255.255/", netguard.ReasonNotGlobal, "255.255.255.255"},
		{"https://224.0.0.1/", netguard.ReasonNotGlobal, "224.0.0.1"},
		{"https://[ff02::1]/", netguard.ReasonNotGlobal, "ff02::1"},
		{"https://[fec0::1]/", netguard.ReasonNotGlobal, "fec0::1"},
		{"https://100.64.0.1/", netguard.ReasonNotGlobal, "100.64.0.1"},
		{"https://192.0.0.9/", "", ""},
		{"https://[64:ff9b::a00:1]/", netguard.ReasonNotGlobal, "64:ff9b::a00:1"},
		{"https://private.example/", netguard.ReasonNotGlobal, "10.0.0.5"},
		{"https://mixed.example/", netguard.ReasonNotGlobal, "10.0.0.5"},
		{"https://mixed6.example/", netguard.ReasonNotGlobal, "fd00::1"},
		{"https://metadata.example/", netguard.ReasonNotGlobal, "169.254.169.254"},
		{"https://mapped.example/", netguard.ReasonNotGlobal, "10.0.0.1"},
		{"https://nat64.example/", netguard.ReasonNotGlobal, "64:ff9b::7f00:1"},
		{"https://sixtofour.example/", netguard.ReasonNotGlobal, "2002:a00:1::1"},
		{"https://loopback.example/", netguard.ReasonNotGlobal, "127.0.0.1"},
		{"https://empty.example/", netguard.ReasonNoAddresses, ""},
		{"https://nxdomain.example/", "dns", ""},
		{"https://servfail.example/", "dns", ""},
		{"http://127.0.0.1:8080/", netguard.ReasonNotGlobal, "127.0.0.1"},
		{"http://[::1]/", netguard.ReasonNotGlobal, "::1"},
		{"http://[::ffff:127.0.0.1]/", netguard.ReasonNotGlobal, "127.0.0.1"},
		{"http://0.0.0.0/", netguard.ReasonNotGlobal, "0.0.0.0"},
		{"https://localhost.example./", netguard.ReasonNotGlobal, "127.0.0.1"},
		// The scheme plays no part.
		{"http://8.8.8.8/", "", ""},
		{"http://[2001:4860:4860::8888]:8443/", "", ""},
		{"http://public.example/", "", ""},
		{"ftp://8.8.8.8/", "", ""},
		{"ftp://public.example/", "", ""},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.url)
		require.NoError(t, err, tc.url)
		err = g.CheckURL(context.Background(), u)
		switch tc.reason {
		case "":
			assert.NoError(t, err, tc.url)
		case "dns":
			var dnsErr *net.DNSError
			assert.True(t, errors.As(err, &dnsErr), "%s: %v", tc.url, err)
			assert.False(t, netguard.IsAddressNotAllowed(err), tc.url)
		default:
			e := requireNotAllowed(t, err, tc.reason)
			if tc.addr != "" {
				assert.Equal(t, netip.MustParseAddr(tc.addr), e.Addr, tc.url)
			} else {
				assert.False(t, e.Addr.IsValid(), tc.url)
			}
			assert.Equal(t, u.Hostname(), e.Host, tc.url)
			if _, literal := netip.ParseAddr(e.Host); literal != nil && e.Addr.IsValid() {
				assert.NotContains(t, e.Error(), e.Addr.String(), "Error() leaves the resolved address out")
			}
		}
	}

	// Names are looked up rooted; IP literals never reach the resolver.
	for _, q := range res.Queries() {
		assert.True(t, strings.HasSuffix(q, ".") && !strings.HasSuffix(q, ".."), q)
	}
	assert.NotContains(t, res.Queries(), "8.8.8.8.")
	assert.Equal(t, 4, countOf(res.Queries(), "public.example."), "once per public.example URL")
}

func countOf(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}

func TestCheckURL_ErrorShapes(t *testing.T) {
	t.Parallel()
	res := newResolver()
	g := &netguard.Guard{Resolver: res}
	assert.Error(t, g.CheckURL(context.Background(), nil))
	assert.Error(t, g.CheckURL(context.Background(), &url.URL{Scheme: "https", Path: "/x"}))

	// A non-ASCII host is refused, never handed to the resolver half-encoded.
	err := g.CheckURL(context.Background(), &url.URL{Scheme: "https", Host: "bücher.example"})
	require.Error(t, err)
	assert.False(t, netguard.IsAddressNotAllowed(err))
	assert.Empty(t, res.Queries())
}

func TestCheckURL_Allowlist(t *testing.T) {
	t.Parallel()
	res := newResolver().
		set("localhost", []string{"127.0.0.1", "::1"}).
		set("internal.example", []string{"10.2.2.2"}).
		set("mapped.example", []string{"::ffff:10.2.2.2"}).
		set("other.example", []string{"10.3.3.3"})

	exact := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "127.0.0.1/32")}
	assert.NoError(t, exact.CheckURL(context.Background(), mustURL(t, "http://127.0.0.1:9000/")))
	requireNotAllowed(t, exact.CheckURL(context.Background(), mustURL(t, "http://127.0.0.2:9000/")), netguard.ReasonNotGlobal)

	private := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "10.2.0.0/16")}
	assert.NoError(t, private.CheckURL(context.Background(), mustURL(t, "https://10.2.1.1/")))
	assert.NoError(t, private.CheckURL(context.Background(), mustURL(t, "https://internal.example/")))
	assert.NoError(t, private.CheckURL(context.Background(), mustURL(t, "https://mapped.example/")))
	requireNotAllowed(t, private.CheckURL(context.Background(), mustURL(t, "https://other.example/")), netguard.ReasonNotGlobal)
	requireNotAllowed(t, private.CheckURL(context.Background(), mustURL(t, "https://10.3.1.1/")), netguard.ReasonNotGlobal)

	loopback := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "localhost")}
	assert.NoError(t, loopback.CheckURL(context.Background(), mustURL(t, "http://[::1]:9000/")))
	assert.NoError(t, loopback.CheckURL(context.Background(), mustURL(t, "http://127.1.2.3/")))
	assert.NoError(t, loopback.CheckURL(context.Background(), mustURL(t, "http://localhost:9000/")))
	assert.NoError(t, loopback.CheckURL(context.Background(), mustURL(t, "http://localhost.:9000/")))
	requireNotAllowed(t, loopback.CheckURL(context.Background(), mustURL(t, "https://10.0.0.1/")), netguard.ReasonNotGlobal)
}

// Go's URL parser keeps these forms as host names and netip refuses them,
// so they reach the resolver, and whatever the system resolver makes of
// them, its answer is checked.
func TestCheckURL_ShortIPv4Forms(t *testing.T) {
	t.Parallel()
	hosts := []string{"0x7f.1", "127.1", "2130706433", "0177.0.0.1", "0x7f000001"}
	res := newResolver()
	for _, h := range hosts {
		_, err := netip.ParseAddr(h)
		require.Error(t, err, h)
		res.set(h, []string{"127.0.0.1"})
	}
	g := &netguard.Guard{Resolver: res}
	for _, h := range hosts {
		requireNotAllowed(t, g.CheckURL(context.Background(), mustURL(t, "http://"+h+"/")), netguard.ReasonNotGlobal)
	}

	// An unresolvable name fails closed.
	err := (&netguard.Guard{Resolver: newResolver()}).CheckURL(context.Background(), mustURL(t, "http://127.1/"))
	require.Error(t, err)
	assert.False(t, netguard.IsAddressNotAllowed(err))
}

func TestCheckURL_TrailingDot(t *testing.T) {
	t.Parallel()
	res := newResolver().set("public.example", []string{"8.8.8.8"}).set("localhost", []string{"127.0.0.1"})
	g := &netguard.Guard{Resolver: res}
	assert.NoError(t, g.CheckURL(context.Background(), mustURL(t, "https://public.example./")))
	assert.Equal(t, []string{"public.example."}, res.Queries())
	requireNotAllowed(t, g.CheckURL(context.Background(), mustURL(t, "https://localhost./")), netguard.ReasonNotGlobal)
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// listener accepts and closes connections on 127.0.0.1 and returns its port.
func listener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

func TestDialContext_RefusesPrivateAddresses(t *testing.T) {
	t.Parallel()
	dns := netguardtest.NewDNS(t).Set("internal.example", []string{"10.0.0.1"})
	g := &netguard.Guard{Resolver: dns.Resolver}
	ctx := context.Background()

	for _, address := range []string{"10.0.0.1:443", "[fd00::1]:443", "[::ffff:10.0.0.1]:443", "127.0.0.1:1"} {
		_, err := g.DialContext(ctx, "tcp", address)
		requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	}

	_, err := g.DialContext(ctx, "tcp", "internal.example:443")
	e := requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.Equal(t, netip.MustParseAddr("10.0.0.1"), e.Addr)
	assert.Equal(t, "internal.example", e.Host)
	assert.Equal(t, []string{"internal.example."}, dns.Queries(), "names are looked up in rooted form")
}

func TestDialContext_Rebinding(t *testing.T) {
	t.Parallel()
	// Public when the URL is checked, private by the time it is dialed.
	dns := netguardtest.NewDNS(t).Set("rebind.example", []string{"93.184.216.34"}, []string{"10.0.0.1"})
	g := &netguard.Guard{Resolver: dns.Resolver}

	require.NoError(t, g.CheckURL(context.Background(), mustURL(t, "https://rebind.example/")))
	_, err := g.DialContext(context.Background(), "tcp", "rebind.example:443")
	e := requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.Equal(t, netip.MustParseAddr("10.0.0.1"), e.Addr)
	assert.Equal(t, []string{"rebind.example.", "rebind.example."}, dns.Queries(), "resolved again at dial time")
}

func TestDialContext_MixedRecordsNeverConnectToPrivate(t *testing.T) {
	t.Parallel()
	port := listener(t)
	dns := netguardtest.NewDNS(t).Set("mixed.example", []string{"10.0.0.1", "127.0.0.1"})

	// Without the allowlisted address the private one is all there is.
	g := &netguard.Guard{Resolver: dns.Resolver}
	_, err := g.DialContext(context.Background(), "tcp", "mixed.example:"+port)
	requireNotAllowed(t, err, netguard.ReasonNotGlobal)

	g = &netguard.Guard{Resolver: dns.Resolver, Allowlist: mustAllowlist(t, "127.0.0.1/32")}
	conn, err := g.DialContext(context.Background(), "tcp", "mixed.example:"+port)
	require.NoError(t, err)
	defer conn.Close()
	assert.Equal(t, "127.0.0.1", conn.RemoteAddr().(*net.TCPAddr).IP.String())
}

func TestDialContext_AllowlistedLoopback(t *testing.T) {
	t.Parallel()
	port := listener(t)
	dns := netguardtest.NewDNS(t).Set("hooks.example", []string{"127.0.0.1"})
	g := &netguard.Guard{Resolver: dns.Resolver, Allowlist: mustAllowlist(t, "localhost")}

	for _, address := range []string{"127.0.0.1:" + port, "hooks.example:" + port} {
		conn, err := g.DialContext(context.Background(), "tcp", address)
		require.NoError(t, err, address)
		conn.Close()
	}
}

func TestDialContext_Arguments(t *testing.T) {
	t.Parallel()
	g := &netguard.Guard{}
	ctx := context.Background()
	_, err := g.DialContext(ctx, "udp", "8.8.8.8:53")
	assert.Error(t, err)
	_, err = g.DialContext(ctx, "tcp", "8.8.8.8")
	assert.Error(t, err)
	_, err = g.DialContext(ctx, "tcp", "bücher.example:443")
	assert.Error(t, err)
}

func TestDialContext_ResolverErrorPassesThrough(t *testing.T) {
	t.Parallel()
	g := &netguard.Guard{Resolver: netguardtest.NewDNS(t).Resolver}
	_, err := g.DialContext(context.Background(), "tcp", "nxdomain.example:443")
	var dnsErr *net.DNSError
	require.True(t, errors.As(err, &dnsErr), "%v", err)
	assert.True(t, dnsErr.IsNotFound)
}
