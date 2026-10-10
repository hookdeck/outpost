package netguard_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver answers from a table keyed by rooted name and records every
// query. A host mapped to several answers returns them in turn (the last one
// repeats), which is how the rebinding tests change DNS between calls.
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][][]string
	errs    map[string]error
	calls   map[string]int
	queries []string
	network []string
}

func newResolver() *fakeResolver {
	return &fakeResolver{answers: map[string][][]string{}, errs: map[string]error{}, calls: map[string]int{}}
}

// set maps host (without the trailing dot) to successive answers.
func (r *fakeResolver) set(host string, answers ...[]string) *fakeResolver {
	r.answers[host+"."] = answers
	return r
}

func (r *fakeResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, host)
	r.network = append(r.network, network)
	if err, ok := r.errs[host]; ok {
		return nil, err
	}
	seq, ok := r.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	i := r.calls[host]
	r.calls[host]++
	if i >= len(seq) {
		i = len(seq) - 1
	}
	var out []netip.Addr
	for _, s := range seq[i] {
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
	al, _, err := netguard.ParseAllowlist(entries)
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
	assert.Equal(t, reason == netguard.ReasonHTTPSRequired, netguard.IsHTTPSRequired(err))
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
		set("empty.example", []string{})
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
		// Plain http and other schemes, refused before any lookup.
		{"http://8.8.8.8/", netguard.ReasonHTTPSRequired, ""},
		{"http://public.example/", netguard.ReasonHTTPSRequired, ""},
		{"HTTP://public.example/", netguard.ReasonHTTPSRequired, ""},
		{"ftp://public.example/", netguard.ReasonHTTPSRequired, ""},
		{"wss://public.example/", netguard.ReasonHTTPSRequired, ""},
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

	// Names are looked up rooted; http, other schemes and IP literals
	// never reach the resolver.
	for _, q := range res.Queries() {
		assert.True(t, strings.HasSuffix(q, ".") && !strings.HasSuffix(q, ".."), q)
	}
	assert.NotContains(t, res.Queries(), "8.8.8.8.")
	assert.Equal(t, 2, countOf(res.Queries(), "public.example."), "https://public.example twice, http never")
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

func TestCheckURL_LoopbackAllowlist(t *testing.T) {
	t.Parallel()
	res := newResolver().
		set("localhost", []string{"127.0.0.1", "::1"}).
		set("tunnel.example", []string{"127.0.0.1"}).
		set("loop-and-public.example", []string{"127.0.0.1", "8.8.8.8"}).
		set("public.example", []string{"8.8.8.8"})
	g := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "localhost")}

	cases := []struct{ url, reason string }{
		{"https://127.0.0.1:9000/", ""},
		{"https://[::1]:9000/", ""},
		{"http://127.0.0.1:9000/", ""},
		{"http://127.1.2.3:9000/", ""},
		{"http://[::1]:9000/", ""},
		{"http://localhost:9000/", ""},
		{"http://tunnel.example/", ""},
		{"https://public.example/", ""},
		// http to a global (or partly global) host still needs TLS.
		{"http://public.example/", netguard.ReasonHTTPSRequired},
		{"http://8.8.8.8/", netguard.ReasonHTTPSRequired},
		{"http://loop-and-public.example/", netguard.ReasonHTTPSRequired},
		{"http://10.0.0.1/", netguard.ReasonNotGlobal},
		{"https://10.0.0.1/", netguard.ReasonNotGlobal},
		{"ftp://127.0.0.1/", netguard.ReasonHTTPSRequired},
	}
	for _, tc := range cases {
		err := g.CheckURL(context.Background(), mustURL(t, tc.url))
		if tc.reason == "" {
			assert.NoError(t, err, tc.url)
		} else {
			requireNotAllowed(t, err, tc.reason)
		}
	}
}

func TestCheckURL_PrivateAllowlist(t *testing.T) {
	t.Parallel()
	res := newResolver().set("internal.example", []string{"10.2.2.2"}).set("public.example", []string{"8.8.8.8"})
	al := mustAllowlist(t, "10.0.0.0/8")

	strict := &netguard.Guard{Resolver: res, Allowlist: al}
	assert.NoError(t, strict.CheckURL(context.Background(), mustURL(t, "https://10.1.1.1/")))
	assert.NoError(t, strict.CheckURL(context.Background(), mustURL(t, "https://internal.example/")))
	before := len(res.Queries())
	// No allowlisted loopback and no AllowInsecure: http can never pass, so
	// it's refused without a lookup.
	requireNotAllowed(t, strict.CheckURL(context.Background(), mustURL(t, "http://internal.example/")), netguard.ReasonHTTPSRequired)
	requireNotAllowed(t, strict.CheckURL(context.Background(), mustURL(t, "http://10.1.1.1/")), netguard.ReasonHTTPSRequired)
	assert.Len(t, res.Queries(), before)

	insecure := &netguard.Guard{Resolver: res, Allowlist: al, AllowInsecure: true}
	assert.NoError(t, insecure.CheckURL(context.Background(), mustURL(t, "http://10.1.1.1/")))
	assert.NoError(t, insecure.CheckURL(context.Background(), mustURL(t, "http://internal.example/")))
	// AllowInsecure covers allowlisted addresses only.
	requireNotAllowed(t, insecure.CheckURL(context.Background(), mustURL(t, "http://public.example/")), netguard.ReasonHTTPSRequired)
	requireNotAllowed(t, insecure.CheckURL(context.Background(), mustURL(t, "http://127.0.0.1/")), netguard.ReasonNotGlobal)

	// AllowInsecure without an allowlist opens nothing.
	bare := &netguard.Guard{Resolver: res, AllowInsecure: true}
	requireNotAllowed(t, bare.CheckURL(context.Background(), mustURL(t, "http://8.8.8.8/")), netguard.ReasonHTTPSRequired)
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// dialRecorder is a dial hook that records the addresses dialed.
type dialRecorder struct {
	mu    sync.Mutex
	addrs []string
}

func (d *dialRecorder) record(addr string) {
	d.mu.Lock()
	d.addrs = append(d.addrs, addr)
	d.mu.Unlock()
}

func (d *dialRecorder) dialed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.addrs...)
}

func TestDialContext_Rebinding(t *testing.T) {
	t.Parallel()
	// Public when the URL is checked, private by the time it is dialed.
	res := newResolver().set("rebind.example", []string{"93.184.216.34"}, []string{"10.0.0.1"})
	g := &netguard.Guard{Resolver: res}
	rec := &dialRecorder{}
	netguard.SetDial(g, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		return nil, errors.New("unexpected dial")
	})

	require.NoError(t, g.CheckURL(context.Background(), mustURL(t, "https://rebind.example/")))
	_, err := g.DialContext(netguard.WithScheme(context.Background(), "https"), "tcp", "rebind.example:443")
	e := requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.Equal(t, netip.MustParseAddr("10.0.0.1"), e.Addr)
	assert.Equal(t, "rebind.example", e.Host)
	assert.Empty(t, rec.dialed(), "nothing is dialed once any address fails")
	assert.Equal(t, []string{"rebind.example.", "rebind.example."}, res.Queries(), "resolved again at dial time")
}

func TestDialContext_AnyBadAddressFailsHost(t *testing.T) {
	t.Parallel()
	res := newResolver().set("mixed.example", []string{"127.0.0.1", "10.0.0.1"})
	g := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "127.0.0.0/8")}
	rec := &dialRecorder{}
	netguard.SetDial(g, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		return nil, errors.New("unexpected dial")
	})
	_, err := g.DialContext(netguard.WithScheme(context.Background(), "https"), "tcp", "mixed.example:443")
	requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.Empty(t, rec.dialed())
}

func TestDialContext_IPLiteralAndArguments(t *testing.T) {
	t.Parallel()
	res := newResolver()
	g := &netguard.Guard{Resolver: res}
	ctx := netguard.WithScheme(context.Background(), "https")

	_, err := g.DialContext(ctx, "tcp", "10.0.0.1:443")
	requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	_, err = g.DialContext(ctx, "tcp", "[fd00::1]:443")
	requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.Empty(t, res.Queries(), "IP literals are not resolved")

	_, err = g.DialContext(ctx, "udp", "8.8.8.8:53")
	assert.Error(t, err)
	_, err = g.DialContext(ctx, "tcp", "8.8.8.8")
	assert.Error(t, err)
	_, err = g.DialContext(ctx, "tcp", "8.8.8.8:https")
	assert.Error(t, err)
	_, err = g.DialContext(ctx, "tcp", "8.8.8.8:70000")
	assert.Error(t, err)
}

func TestDialContext_LookupNetworkFollowsDialNetwork(t *testing.T) {
	t.Parallel()
	res := newResolver().set("v4.example", []string{"10.0.0.1"})
	g := &netguard.Guard{Resolver: res}
	ctx := netguard.WithScheme(context.Background(), "https")
	for _, network := range []string{"tcp", "tcp4", "tcp6"} {
		_, _ = g.DialContext(ctx, network, "v4.example:443")
	}
	res.mu.Lock()
	defer res.mu.Unlock()
	assert.Equal(t, []string{"ip", "ip4", "ip6"}, res.network)
}

// Without WithScheme the dialer can't tell http from https, so it applies
// the stricter http policy.
func TestDialContext_NoSchemeIsHTTP(t *testing.T) {
	t.Parallel()
	g := &netguard.Guard{Resolver: newResolver()}
	_, err := g.DialContext(context.Background(), "tcp", "8.8.8.8:443")
	requireNotAllowed(t, err, netguard.ReasonHTTPSRequired)

	g = &netguard.Guard{Resolver: newResolver(), Allowlist: mustAllowlist(t, "localhost")}
	_, err = g.DialContext(context.Background(), "tcp", "8.8.8.8:443")
	requireNotAllowed(t, err, netguard.ReasonHTTPSRequired)
}

// listen starts a TCP listener on 127.0.0.1 that accepts and holds
// connections, counting them.
func listen(t *testing.T) (net.Listener, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	return ln, &accepted
}

func TestDialContext_PerAddressTimeout(t *testing.T) {
	t.Parallel()
	ln, accepted := listen(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	// 127.0.0.2 blackholes; 127.0.0.1 answers.
	res := newResolver().set("multi.example", []string{"127.0.0.2", "127.0.0.1"})
	g := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "127.0.0.0/8")}
	rec := &dialRecorder{}
	var blackholeBudget atomic.Int64
	netguard.SetDial(g, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		if strings.HasPrefix(address, "127.0.0.2:") {
			dl, _ := ctx.Deadline()
			blackholeBudget.Store(int64(time.Until(dl)))
			<-ctx.Done()
			return nil, &net.OpError{Op: "dial", Net: network, Err: ctx.Err()}
		}
		return d.DialContext(ctx, network, address)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	conn, err := g.DialContext(netguard.WithScheme(ctx, "https"), "tcp", "multi.example:"+port)
	require.NoError(t, err)
	defer conn.Close()

	assert.Equal(t, []string{"127.0.0.2:" + port, "127.0.0.1:" + port}, rec.dialed())
	assert.Equal(t, "127.0.0.1:"+port, conn.RemoteAddr().String())
	// The first address got about half the remaining budget, not all of it.
	// (Loose bounds: only the halving matters, not scheduler jitter.)
	budget := time.Duration(blackholeBudget.Load())
	assert.InDelta(t, float64(400*time.Millisecond), float64(budget), float64(150*time.Millisecond), "budget %s", budget)
	assert.Eventually(t, func() bool { return accepted.Load() == 1 }, time.Second, 5*time.Millisecond)
}

func TestDialContext_PerAddressTimeoutCap(t *testing.T) {
	t.Parallel()
	res := newResolver().set("multi.example", []string{"127.0.0.2", "127.0.0.3"})
	g := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "127.0.0.0/8")}
	var budgets []time.Duration
	var mu sync.Mutex
	netguard.SetDial(g, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		dl, ok := ctx.Deadline()
		require.True(t, ok)
		mu.Lock()
		budgets = append(budgets, time.Until(dl))
		mu.Unlock()
		return nil, fmt.Errorf("dial %s: connection refused", address)
	})

	// No deadline anywhere: each address gets the 3s cap.
	_, err := g.DialContext(netguard.WithScheme(context.Background(), "https"), "tcp", "multi.example:443")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.2", "the first failure is returned")
	require.Len(t, budgets, 2)
	for _, b := range budgets {
		assert.InDelta(t, float64(netguard.MaxAddrDialTimeout), float64(b), float64(500*time.Millisecond))
	}

	// A request deadline carried by WithScheme is honoured even though
	// http.Transport dials with a context that has none.
	budgets = nil
	reqCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	dialCtx := context.WithoutCancel(netguard.WithScheme(reqCtx, "https"))
	_, err = g.DialContext(dialCtx, "tcp", "multi.example:443")
	require.Error(t, err)
	require.Len(t, budgets, 2)
	assert.InDelta(t, float64(500*time.Millisecond), float64(budgets[0]), float64(200*time.Millisecond))
}

func TestDialContext_AttemptCap(t *testing.T) {
	t.Parallel()
	var answer []string
	for i := 1; i <= 20; i++ {
		answer = append(answer, fmt.Sprintf("127.0.0.%d", i))
	}
	res := newResolver().set("many.example", answer)
	g := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "127.0.0.0/8")}
	rec := &dialRecorder{}
	netguard.SetDial(g, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		rec.record(address)
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connection refused")}
	})
	_, err := g.DialContext(netguard.WithScheme(context.Background(), "https"), "tcp", "many.example:443")
	require.Error(t, err)
	assert.Len(t, rec.dialed(), netguard.MaxDialAttempts)
}

// The Control hook re-checks the address the socket really connects to,
// whatever the code above it vetted.
func TestDialContext_ControlRechecksConnectedAddress(t *testing.T) {
	t.Parallel()
	ln, accepted := listen(t)
	res := newResolver().set("public.example", []string{"8.8.8.8"})
	g := &netguard.Guard{Resolver: res}
	netguard.SetDial(g, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		// Swap the vetted 8.8.8.8 for a loopback listener.
		return d.DialContext(ctx, network, ln.Addr().String())
	})
	_, err := g.DialContext(netguard.WithScheme(context.Background(), "https"), "tcp", "public.example:443")
	e := requireNotAllowed(t, err, netguard.ReasonNotGlobal)
	assert.Equal(t, netip.MustParseAddr("127.0.0.1"), e.Addr)
	assert.Equal(t, "public.example", e.Host)
	// Control runs before connect(2): the listener never sees a connection.
	time.Sleep(50 * time.Millisecond)
	assert.Zero(t, accepted.Load())

	// It applies the scheme policy too: a plain-http connection vetted for
	// an allowlisted loopback address must not end up at a public one.
	res = newResolver().set("tunnel.example", []string{"127.0.0.1"})
	g = &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "localhost")}
	netguard.SetDial(g, func(ctx context.Context, d *net.Dialer, network, address string) (net.Conn, error) {
		// Refused by Control before connect(2): no packet leaves.
		return d.DialContext(ctx, network, "8.8.8.8:80")
	})
	_, err = g.DialContext(netguard.WithScheme(context.Background(), "http"), "tcp", "tunnel.example:80")
	e = requireNotAllowed(t, err, netguard.ReasonHTTPSRequired)
	assert.Equal(t, netip.MustParseAddr("8.8.8.8"), e.Addr)
}

func TestDialContext_ConnectsToVettedAddress(t *testing.T) {
	t.Parallel()
	ln, accepted := listen(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	res := newResolver().set("hooks.example", []string{"::ffff:127.0.0.1"})
	g := &netguard.Guard{Resolver: res, Allowlist: mustAllowlist(t, "localhost")}
	conn, err := g.DialContext(netguard.WithScheme(context.Background(), "http"), "tcp", "hooks.example:"+port)
	require.NoError(t, err)
	defer conn.Close()
	// Mapped answers are dialed as IPv4.
	assert.Equal(t, "127.0.0.1:"+port, conn.RemoteAddr().String())
	assert.Eventually(t, func() bool { return accepted.Load() == 1 }, time.Second, 5*time.Millisecond)
}

func TestDialContext_ResolverErrorPassesThrough(t *testing.T) {
	t.Parallel()
	g := &netguard.Guard{Resolver: newResolver()}
	_, err := g.DialContext(netguard.WithScheme(context.Background(), "https"), "tcp", "nxdomain.example:443")
	var dnsErr *net.DNSError
	require.True(t, errors.As(err, &dnsErr), "%v", err)
	assert.True(t, dnsErr.IsNotFound)

	g = &netguard.Guard{Resolver: newResolver().set("empty.example", []string{})}
	_, err = g.DialContext(netguard.WithScheme(context.Background(), "https"), "tcp", "empty.example:443")
	requireNotAllowed(t, err, netguard.ReasonNoAddresses)
}
