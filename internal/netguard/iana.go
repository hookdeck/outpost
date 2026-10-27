package netguard

import "net/netip"

// specialBlock is one row of an IANA special-purpose address registry.
// global mirrors the "Globally Reachable" column: only "True" is global.
// "False", "N/A" and the blank columns of terminated blocks all fail closed.
type specialBlock struct {
	prefix netip.Prefix
	global bool
}

// The tables below are transcribed from the IANA IPv4 and IPv6
// Special-Purpose Address Registries, both last updated 2025-10-09
// (https://www.iana.org/assignments/iana-ipv4-special-registry/ and
// .../iana-ipv6-special-registry/). The CSVs are kept in testdata/ and a test
// checks the tables against them row for row: to refresh, replace the CSVs
// with the current iana-ipv{4,6}-special-registry-1.csv and update the
// tables until the test passes.
//
// A lookup takes the most specific matching block, which is how the
// registry expresses exceptions: 192.0.0.9/32 is global inside the
// non-global 192.0.0.0/24.
var ipv4Special = []specialBlock{
	{netip.MustParsePrefix("0.0.0.0/8"), false},      // This network
	{netip.MustParsePrefix("0.0.0.0/32"), false},     // This host on this network
	{netip.MustParsePrefix("10.0.0.0/8"), false},     // Private-Use
	{netip.MustParsePrefix("100.64.0.0/10"), false},  // Shared Address Space
	{netip.MustParsePrefix("127.0.0.0/8"), false},    // Loopback
	{netip.MustParsePrefix("169.254.0.0/16"), false}, // Link Local
	{netip.MustParsePrefix("172.16.0.0/12"), false},  // Private-Use
	{netip.MustParsePrefix("192.0.0.0/24"), false},   // IETF Protocol Assignments
	{netip.MustParsePrefix("192.0.0.0/29"), false},   // IPv4 Service Continuity Prefix
	{netip.MustParsePrefix("192.0.0.8/32"), false},   // IPv4 dummy address
	{netip.MustParsePrefix("192.0.0.9/32"), true},    // Port Control Protocol Anycast
	{netip.MustParsePrefix("192.0.0.10/32"), true},   // Traversal Using Relays around NAT Anycast
	{netip.MustParsePrefix("192.0.0.170/32"), false}, // NAT64/DNS64 Discovery
	{netip.MustParsePrefix("192.0.0.171/32"), false}, // NAT64/DNS64 Discovery
	{netip.MustParsePrefix("192.0.2.0/24"), false},   // Documentation (TEST-NET-1)
	{netip.MustParsePrefix("192.31.196.0/24"), true}, // AS112-v4
	{netip.MustParsePrefix("192.52.193.0/24"), true}, // AMT
	// Terminated 2015-03 (RFC 7526); the registry leaves its columns blank.
	{netip.MustParsePrefix("192.88.99.0/24"), false},     // Deprecated (6to4 Relay Anycast)
	{netip.MustParsePrefix("192.88.99.2/32"), false},     // 6a44-relay anycast address
	{netip.MustParsePrefix("192.168.0.0/16"), false},     // Private-Use
	{netip.MustParsePrefix("192.175.48.0/24"), true},     // Direct Delegation AS112 Service
	{netip.MustParsePrefix("198.18.0.0/15"), false},      // Benchmarking
	{netip.MustParsePrefix("198.51.100.0/24"), false},    // Documentation (TEST-NET-2)
	{netip.MustParsePrefix("203.0.113.0/24"), false},     // Documentation (TEST-NET-3)
	{netip.MustParsePrefix("240.0.0.0/4"), false},        // Reserved
	{netip.MustParsePrefix("255.255.255.255/32"), false}, // Limited Broadcast
	// Not in the special-purpose registry: multicast (IANA IPv4 Multicast
	// Address Space Registry) is never a unicast destination.
	{netip.MustParsePrefix("224.0.0.0/4"), false}, // Multicast
}

// ipv6Special leaves out three registry rows that IsGlobal judges by the
// IPv4 address they carry instead: ::ffff:0:0/96 (IPv4-mapped; Go dials it as
// plain IPv4), 64:ff9b::/96 (NAT64, "True" in the registry, but a NAT64
// gateway forwards to whatever IPv4 address is embedded) and 2002::/16 (6to4,
// "N/A"; a 6to4 router tunnels to the embedded IPv4 address).
var ipv6Special = []specialBlock{
	{netip.MustParsePrefix("::1/128"), false},        // Loopback Address
	{netip.MustParsePrefix("::/128"), false},         // Unspecified Address
	{netip.MustParsePrefix("64:ff9b:1::/48"), false}, // IPv4-IPv6 Translat.
	{netip.MustParsePrefix("100::/64"), false},       // Discard-Only Address Block
	{netip.MustParsePrefix("100:0:0:1::/64"), false}, // Dummy IPv6 Prefix
	{netip.MustParsePrefix("2001::/23"), false},      // IETF Protocol Assignments
	{netip.MustParsePrefix("2001::/32"), false},      // TEREDO ("N/A")
	{netip.MustParsePrefix("2001:1::1/128"), true},   // Port Control Protocol Anycast
	{netip.MustParsePrefix("2001:1::2/128"), true},   // Traversal Using Relays around NAT Anycast
	{netip.MustParsePrefix("2001:1::3/128"), true},   // DNS-SD Service Registration Protocol Anycast
	{netip.MustParsePrefix("2001:2::/48"), false},    // Benchmarking
	{netip.MustParsePrefix("2001:3::/32"), true},     // AMT
	{netip.MustParsePrefix("2001:4:112::/48"), true}, // AS112-v6
	// Terminated 2014-03 (RFC 4843); the registry leaves its columns blank.
	{netip.MustParsePrefix("2001:10::/28"), false},     // Deprecated (previously ORCHID)
	{netip.MustParsePrefix("2001:20::/28"), true},      // ORCHIDv2
	{netip.MustParsePrefix("2001:30::/28"), true},      // Drone Remote ID Protocol Entity Tags (DETs) Prefix
	{netip.MustParsePrefix("2001:db8::/32"), false},    // Documentation
	{netip.MustParsePrefix("2620:4f:8000::/48"), true}, // Direct Delegation AS112 Service
	{netip.MustParsePrefix("3fff::/20"), false},        // Documentation
	{netip.MustParsePrefix("5f00::/16"), false},        // Segment Routing (SRv6) SIDs
	{netip.MustParsePrefix("fc00::/7"), false},         // Unique-Local
	{netip.MustParsePrefix("fe80::/10"), false},        // Link-Local Unicast
}

var (
	ipv4Mapped = netip.MustParsePrefix("::ffff:0:0/96")
	nat64      = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour  = netip.MustParsePrefix("2002::/16")
	// globalUnicast is the only IPv6 space the IANA IPv6 Address Space
	// Registry allocates for global unicast; everything outside it (fec0::/10
	// site-local, ff00::/8 multicast, the reserved blocks) is never global.
	globalUnicast = netip.MustParsePrefix("2000::/3")
)

// IsGlobal reports whether addr is globally reachable per the IANA
// special-purpose address registries. Unlike netip.Addr.IsGlobalUnicast it
// rejects shared address space, documentation, benchmarking and reserved
// ranges, and it judges IPv4-mapped (::ffff:0:0/96), NAT64 (64:ff9b::/96)
// and 6to4 (2002::/16) addresses by the IPv4 address they embed. Invalid
// addresses and addresses with a zone are never global.
func IsGlobal(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	if addr.Is4() {
		b, ok := mostSpecific(ipv4Special, addr)
		return !ok || b.global
	}
	if v4, ok := embeddedIPv4(addr); ok {
		return IsGlobal(v4)
	}
	if b, ok := mostSpecific(ipv6Special, addr); ok {
		return b.global
	}
	return globalUnicast.Contains(addr)
}

// embeddedIPv4 extracts the IPv4 address an IPv4-mapped, NAT64 or 6to4
// address stands for.
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	a := addr.As16()
	switch {
	case ipv4Mapped.Contains(addr), nat64.Contains(addr):
		return netip.AddrFrom4([4]byte(a[12:16])), true
	case sixToFour.Contains(addr):
		return netip.AddrFrom4([4]byte(a[2:6])), true
	}
	return netip.Addr{}, false
}

func mostSpecific(blocks []specialBlock, addr netip.Addr) (specialBlock, bool) {
	best := -1
	for i := range blocks {
		if blocks[i].prefix.Contains(addr) && (best < 0 || blocks[i].prefix.Bits() > blocks[best].prefix.Bits()) {
			best = i
		}
	}
	if best < 0 {
		return specialBlock{}, false
	}
	return blocks[best], true
}
