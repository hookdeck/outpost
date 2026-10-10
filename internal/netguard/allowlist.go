package netguard

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// Allowlist holds the address ranges a Guard lets connections reach even
// though they are not globally reachable (MCP_CALLBACK_ALLOWLIST: local
// development, tunnels, trusted internal receivers). It holds IP ranges only.
// A hostname entry would trust whatever DNS answers for that name, which is
// exactly what the guard is there to distrust. A nil *Allowlist is empty.
type Allowlist struct {
	prefixes []netip.Prefix
}

var loopbackPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// ParseAllowlist parses allowlist entries: an IP address, a CIDR range, or
// "localhost" (127.0.0.0/8 and ::1/128). Blank entries are skipped. A /0
// range, an address with a zone and anything unparsable is an error; all
// bad entries are reported together. IPv4-mapped IPv6 entries are converted
// to IPv4, since the guard compares unmapped addresses.
//
// warnings lists the entries that open non-global space (private, loopback,
// link-local, ...) and ranges written with host bits set, for the caller to
// log at startup.
func ParseAllowlist(entries []string) (*Allowlist, []string, error) {
	var (
		prefixes []netip.Prefix
		warnings []string
		errs     []error
		seen     = make(map[netip.Prefix]bool)
	)
	add := func(entry string, p netip.Prefix) {
		if seen[p] {
			return
		}
		seen[p] = true
		prefixes = append(prefixes, p)
		if name, ok := nonGlobalOverlap(p); ok {
			warnings = append(warnings, fmt.Sprintf("allowlist entry %q allows connections to non-global addresses (%s %s)", entry, p, name))
		}
	}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, "localhost") {
			for _, p := range loopbackPrefixes {
				add(entry, p)
			}
			continue
		}
		p, err := parseAllowlistEntry(entry)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if masked := p.Masked(); masked != p {
			warnings = append(warnings, fmt.Sprintf("allowlist entry %q has host bits set; using %s", entry, masked))
			p = masked
		}
		add(entry, p)
	}
	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return &Allowlist{prefixes: prefixes}, warnings, nil
}

func parseAllowlistEntry(entry string) (netip.Prefix, error) {
	var p netip.Prefix
	if strings.Contains(entry, "/") {
		var err error
		if p, err = netip.ParsePrefix(entry); err != nil {
			return netip.Prefix{}, fmt.Errorf("allowlist entry %q: not an IP address or CIDR range", entry)
		}
	} else {
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("allowlist entry %q: not an IP address or CIDR range (host names are not supported)", entry)
		}
		if addr.Zone() != "" {
			return netip.Prefix{}, fmt.Errorf("allowlist entry %q: zoned addresses are not supported", entry)
		}
		p = netip.PrefixFrom(addr, addr.BitLen())
	}
	if p.Addr().Is4In6() && p.Bits() >= 96 {
		p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
	}
	if p.Bits() == 0 {
		return netip.Prefix{}, fmt.Errorf("allowlist entry %q: a /0 range would disable the guard", entry)
	}
	return p, nil
}

// Contains reports whether addr falls in an allowlisted range. IPv4-mapped
// addresses are unmapped first; an address with a zone never matches.
func (a *Allowlist) Contains(addr netip.Addr) bool {
	if a == nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range a.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Prefixes returns a copy of the allowlisted ranges.
func (a *Allowlist) Prefixes() []netip.Prefix {
	if a == nil {
		return nil
	}
	return append([]netip.Prefix(nil), a.prefixes...)
}

// permitsHTTP reports whether any address could pass the plain-http policy:
// an allowlisted loopback range, or any allowlisted range when insecure
// callbacks are enabled. When it is false http is refused before DNS.
func (a *Allowlist) permitsHTTP(allowInsecure bool) bool {
	if a == nil || len(a.prefixes) == 0 {
		return false
	}
	if allowInsecure {
		return true
	}
	for _, p := range a.prefixes {
		for _, lo := range loopbackPrefixes {
			if p.Overlaps(lo) {
				return true
			}
		}
	}
	return false
}
