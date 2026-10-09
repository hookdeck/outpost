package netguard_test

import (
	"encoding/csv"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every registry entry with its first and last address and, where the
// neighbour isn't itself special, the addresses just outside it.
func TestIsGlobal_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		addr   string
		global bool
	}{
		// Plain public addresses.
		{"1.1.1.1", true},
		{"8.8.8.8", true},
		{"93.184.216.34", true},
		{"2001:4860:4860::8888", true},
		{"2606:4700:4700::1111", true},

		// 0.0.0.0/8 "This network", 0.0.0.0/32.
		{"0.0.0.0", false},
		{"0.0.0.1", false},
		{"0.255.255.255", false},
		{"1.0.0.0", true},
		// 10.0.0.0/8 Private-Use.
		{"9.255.255.255", true},
		{"10.0.0.0", false},
		{"10.255.255.255", false},
		{"11.0.0.0", true},
		// 100.64.0.0/10 Shared Address Space.
		{"100.63.255.255", true},
		{"100.64.0.0", false},
		{"100.127.255.255", false},
		{"100.128.0.0", true},
		// 127.0.0.0/8 Loopback.
		{"126.255.255.255", true},
		{"127.0.0.0", false},
		{"127.0.0.1", false},
		{"127.255.255.255", false},
		{"128.0.0.0", true},
		// 169.254.0.0/16 Link Local (cloud metadata lives here).
		{"169.253.255.255", true},
		{"169.254.0.0", false},
		{"169.254.169.254", false},
		{"169.254.255.255", false},
		{"169.255.0.0", true},
		// 172.16.0.0/12 Private-Use.
		{"172.15.255.255", true},
		{"172.16.0.0", false},
		{"172.31.255.255", false},
		{"172.32.0.0", true},
		// 192.0.0.0/24 IETF Protocol Assignments with its /29, /32 rows and
		// the two global anycast exceptions.
		{"191.255.255.255", true},
		{"192.0.0.0", false},
		{"192.0.0.7", false},
		{"192.0.0.8", false},
		{"192.0.0.9", true},
		{"192.0.0.10", true},
		{"192.0.0.11", false},
		{"192.0.0.170", false},
		{"192.0.0.171", false},
		{"192.0.0.255", false},
		{"192.0.1.0", true},
		// 192.0.2.0/24 TEST-NET-1.
		{"192.0.2.0", false},
		{"192.0.2.255", false},
		{"192.0.3.0", true},
		// 192.31.196.0/24 AS112-v4 and 192.52.193.0/24 AMT: globally
		// reachable per the registry.
		{"192.31.196.0", true},
		{"192.31.196.255", true},
		{"192.52.193.0", true},
		{"192.52.193.255", true},
		// 192.88.99.0/24 deprecated 6to4 relay anycast, 192.88.99.2/32 6a44.
		{"192.88.98.255", true},
		{"192.88.99.0", false},
		{"192.88.99.1", false},
		{"192.88.99.2", false},
		{"192.88.99.255", false},
		{"192.88.100.0", true},
		// 192.168.0.0/16 Private-Use.
		{"192.167.255.255", true},
		{"192.168.0.0", false},
		{"192.168.255.255", false},
		{"192.169.0.0", true},
		// 192.175.48.0/24 Direct Delegation AS112: globally reachable.
		{"192.175.48.0", true},
		{"192.175.48.255", true},
		// 198.18.0.0/15 Benchmarking.
		{"198.17.255.255", true},
		{"198.18.0.0", false},
		{"198.19.255.255", false},
		{"198.20.0.0", true},
		// 198.51.100.0/24 TEST-NET-2.
		{"198.51.99.255", true},
		{"198.51.100.0", false},
		{"198.51.100.255", false},
		{"198.51.101.0", true},
		// 203.0.113.0/24 TEST-NET-3.
		{"203.0.112.255", true},
		{"203.0.113.0", false},
		{"203.0.113.255", false},
		{"203.0.114.0", true},
		// 224.0.0.0/4 multicast (outside the registry), 240.0.0.0/4 Reserved,
		// 255.255.255.255/32 Limited Broadcast.
		{"223.255.255.255", true},
		{"224.0.0.0", false},
		{"239.255.255.255", false},
		{"240.0.0.0", false},
		{"255.255.255.254", false},
		{"255.255.255.255", false},

		// ::/128, ::1/128 and the rest of ::/8 (outside 2000::/3).
		{"::", false},
		{"::1", false},
		{"::2", false},
		// IPv4-compatible (deprecated) and SIIT IPv4-translated addresses
		// are not unwrapped: they sit outside 2000::/3.
		{"::a00:1", false},
		{"::7f00:1", false},
		{"::808:808", false},
		{"::ffff:0:7f00:1", false},
		{"::ffff:0:808:808", false},
		// ::ffff:0:0/96 IPv4-mapped: judged by the embedded address.
		{"::ffff:0.0.0.0", false},
		{"::ffff:10.0.0.1", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:169.254.169.254", false},
		{"::ffff:8.8.8.8", true},
		{"::ffff:255.255.255.255", false},
		// 64:ff9b::/96 NAT64: judged by the embedded address.
		{"64:ff9b::", false},
		{"64:ff9b::a00:1", false},
		{"64:ff9b::7f00:1", false},
		{"64:ff9b::c000:9", true},
		{"64:ff9b::808:808", true},
		{"64:ff9b::ffff:ffff", false},
		{"64:ff9b::1:0:0", false},
		// 64:ff9b:1::/48 local-use NAT64.
		{"64:ff9b:1::", false},
		{"64:ff9b:1::808:808", false},
		{"64:ff9b:1:ffff:ffff:ffff:ffff:ffff", false},
		// 100::/64 Discard-Only, 100:0:0:1::/64 Dummy IPv6 Prefix.
		{"100::", false},
		{"100::ffff:ffff:ffff:ffff", false},
		{"100:0:0:1::", false},
		{"100:0:0:1:ffff:ffff:ffff:ffff", false},
		// 2000::/3 boundaries.
		{"1fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"2000::", true},
		{"3fff:1000::", true},
		{"4000::", false},
		// 2001::/23 IETF Protocol Assignments, with 2001::/32 TEREDO ("N/A").
		{"2001::", false},
		{"2001:0:4136:e378:8000:63bf:3fff:fdd2", false},
		{"2001:0:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"2001:1::", false},
		{"2001:1::1", true},
		{"2001:1::2", true},
		{"2001:1::3", true},
		{"2001:1::4", false},
		{"2001:2::", false},
		{"2001:2:0:ffff:ffff:ffff:ffff:ffff", false},
		{"2001:2:1::", false},
		{"2001:3::", true},
		{"2001:3:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:4:111:ffff:ffff:ffff:ffff:ffff", false},
		{"2001:4:112::", true},
		{"2001:4:112:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:4:113::", false},
		{"2001:10::", false},
		{"2001:1f:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"2001:20::", true},
		{"2001:2f:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:30::", true},
		{"2001:3f:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:40::", false},
		{"2001:1ff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"2001:200::", true},
		// 2001:db8::/32 Documentation.
		{"2001:db7:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:db8::", false},
		{"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"2001:db9::", true},
		// 2002::/16 6to4: judged by the embedded address.
		{"2002::", false},
		{"2002:a00:1::", false},
		{"2002:7f00:1::1", false},
		{"2002:c0a8:101::1", false},
		{"2002:808:808::", true},
		{"2002:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		// 2620:4f:8000::/48 Direct Delegation AS112.
		{"2620:4f:8000::", true},
		{"2620:4f:8000:ffff:ffff:ffff:ffff:ffff", true},
		// 3fff::/20 Documentation.
		{"3ffe:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"3fff::", false},
		{"3fff:fff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		// 5f00::/16 SRv6 SIDs.
		{"5f00::", false},
		{"5f00:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		// fc00::/7 Unique-Local, fe80::/10 Link-Local, fec0::/10 site-local,
		// ff00::/8 multicast.
		{"fc00::", false},
		{"fd00::1", false},
		{"fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"fe80::", false},
		{"fe80::1", false},
		{"febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"fec0::1", false},
		{"ff02::1", false},
		{"ff0e::1", false},
		// A zone is never global, even on a global address.
		{"fe80::1%eth0", false},
		{"2001:4860:4860::8888%eth0", false},
	}
	for _, tc := range cases {
		addr := netip.MustParseAddr(tc.addr)
		assert.Equal(t, tc.global, netguard.IsGlobal(addr), tc.addr)
	}
	assert.False(t, netguard.IsGlobal(netip.Addr{}), "zero Addr")
}

// registryRow is one row of an IANA special-purpose registry CSV.
type registryRow struct {
	prefix    netip.Prefix
	reachable string // "True", "False", "N/A" or "" (terminated)
}

var footnote = regexp.MustCompile(`\s*\[\d+\]`)

// loadRegistry reads a registry CSV as published by IANA (copies in
// testdata, fetched 2026-10-09; registry last updated 2025-10-09).
func loadRegistry(t *testing.T, path string) []registryRow {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	require.NoError(t, err)
	require.Equal(t, "Address Block", records[0][0])
	require.Equal(t, "Globally Reachable", records[0][8])
	var rows []registryRow
	for _, rec := range records[1:] {
		reachable := strings.TrimSpace(footnote.ReplaceAllString(rec[8], ""))
		for _, block := range strings.Split(footnote.ReplaceAllString(rec[0], ""), ",") {
			rows = append(rows, registryRow{netip.MustParsePrefix(strings.TrimSpace(block)), reachable})
		}
	}
	require.NotEmpty(t, rows)
	return rows
}

var (
	ipv4MappedPrefix = netip.MustParsePrefix("::ffff:0:0/96")
	nat64Prefix      = netip.MustParsePrefix("64:ff9b::/96")
	sixToFourPrefix  = netip.MustParsePrefix("2002::/16")
	multicastV4      = netip.MustParsePrefix("224.0.0.0/4")
	globalUnicastV6  = netip.MustParsePrefix("2000::/3")
)

// embedsIPv4 reports whether a registry row is one IsGlobal unwraps.
func embedsIPv4(p netip.Prefix) bool {
	return p == ipv4MappedPrefix || p == nat64Prefix || p == sixToFourPrefix
}

// registryOracle answers IsGlobal straight from the registry rows (most
// specific row wins, only "True" is global) plus the documented departures:
// embedded IPv4 for mapped/NAT64/6to4, multicast for IPv4, and 2000::/3 as
// the only global IPv6 space.
func registryOracle(rows []registryRow, addr netip.Addr) bool {
	if addr.Is4In6() {
		return registryOracle(rows, addr.Unmap())
	}
	a := addr.As16()
	if nat64Prefix.Contains(addr) {
		return registryOracle(rows, netip.AddrFrom4([4]byte(a[12:16])))
	}
	if sixToFourPrefix.Contains(addr) {
		return registryOracle(rows, netip.AddrFrom4([4]byte(a[2:6])))
	}
	best := -1
	for i, r := range rows {
		if r.prefix.Contains(addr) && (best < 0 || r.prefix.Bits() > rows[best].prefix.Bits()) {
			best = i
		}
	}
	if best >= 0 {
		return rows[best].reachable == "True"
	}
	if addr.Is4() {
		return !multicastV4.Contains(addr)
	}
	return globalUnicastV6.Contains(addr)
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 1 << (7 - i%8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// TestIsGlobal_MatchesRegistry checks the embedded tables row for row
// against the published registries, then probes every row's edges (and the
// addresses just outside them) against an oracle built from the CSVs.
func TestIsGlobal_MatchesRegistry(t *testing.T) {
	t.Parallel()
	v4 := loadRegistry(t, "testdata/iana-ipv4-special-registry-1.csv")
	v6 := loadRegistry(t, "testdata/iana-ipv6-special-registry-1.csv")
	// The oracle needs both: NAT64, 6to4 and mapped rows defer to IPv4 rows.
	all := append(append([]registryRow(nil), v4...), v6...)
	for _, tc := range []struct {
		name string
		v6   bool
		rows []registryRow
	}{
		{"ipv4", false, v4},
		{"ipv6", true, v6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := tc.rows

			want := make(map[netip.Prefix]bool)
			for _, r := range rows {
				if !embedsIPv4(r.prefix) {
					want[r.prefix] = r.reachable == "True"
				}
			}
			got := netguard.RegistryTable(tc.v6)
			delete(got, multicastV4) // not a special-purpose registry row
			assert.Equal(t, want, got, "embedded table must match the registry")

			for _, r := range rows {
				first, last := r.prefix.Masked().Addr(), lastAddr(r.prefix)
				probes := []netip.Addr{first, first.Next(), last, last.Prev(), first.Prev(), last.Next()}
				for _, addr := range probes {
					if !addr.IsValid() {
						continue
					}
					assert.Equal(t, registryOracle(all, addr), netguard.IsGlobal(addr), "%s (row %s)", addr, r.prefix)
				}
			}
		})
	}
}
