// Package netguardtest provides an in-process DNS server for tests that
// resolve host names through the standard library.
package netguardtest

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
)

// DNS answers A queries from a table and AAAA queries with an empty answer.
// Names it doesn't know get NXDOMAIN.
type DNS struct {
	// Resolver sends every query to this server.
	Resolver *net.Resolver

	mu      sync.Mutex
	answers map[string][][]netip.Addr
	calls   map[string]int
	queries []string
}

// NewDNS starts a server that stops with the test.
func NewDNS(t testing.TB) *DNS {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d := &DNS{answers: map[string][][]netip.Addr{}, calls: map[string]int{}}
	d.Resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, conn.LocalAddr().String())
		},
	}
	t.Cleanup(func() { _ = conn.Close() })
	go d.serve(conn)
	return d
}

// Set maps host (no trailing dot) to IPv4 answers. With several answers, the
// nth A query for the host gets the nth one and the last repeats, which is
// how a test changes DNS between a URL check and a dial.
func (d *DNS) Set(host string, answers ...[]string) *DNS {
	seq := make([][]netip.Addr, len(answers))
	for i, set := range answers {
		for _, s := range set {
			seq[i] = append(seq[i], netip.MustParseAddr(s))
		}
	}
	d.mu.Lock()
	d.answers[strings.ToLower(host)+"."] = seq
	d.mu.Unlock()
	return d
}

// Queries returns the A-queried names in rooted form, in order.
func (d *DNS) Queries() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.queries...)
}

func (d *DNS) serve(conn net.PacketConn) {
	buf := make([]byte, 512)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if resp := d.reply(buf[:n]); resp != nil {
			_, _ = conn.WriteTo(resp, from)
		}
	}
}

func (d *DNS) reply(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	var name []string
	i := 12
	for i < len(q) && q[i] != 0 {
		end := i + 1 + int(q[i])
		if end > len(q) {
			return nil
		}
		name = append(name, string(q[i+1:end]))
		i = end
	}
	if i+5 > len(q) {
		return nil
	}
	rooted := strings.ToLower(strings.Join(name, ".")) + "."
	qtype := binary.BigEndian.Uint16(q[i+1:])
	question := q[12 : i+5]

	var ips []netip.Addr
	rcode := byte(3)
	d.mu.Lock()
	if seq, ok := d.answers[rooted]; ok {
		rcode = 0
		if qtype == 1 {
			d.queries = append(d.queries, rooted)
			n := d.calls[rooted]
			d.calls[rooted]++
			ips = seq[min(n, len(seq)-1)]
		}
	}
	d.mu.Unlock()

	resp := []byte{q[0], q[1], 0x81, 0x80 | rcode, 0, 1, byte(len(ips) >> 8), byte(len(ips)), 0, 0, 0, 0}
	resp = append(resp, question...)
	for _, ip := range ips {
		resp = append(resp, 0xC0, 0x0C, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
		a := ip.As4()
		resp = append(resp, a[:]...)
	}
	return resp
}
