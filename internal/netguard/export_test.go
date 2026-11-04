package netguard

import "net/netip"

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

// Prefixes returns a copy of the allowlisted ranges.
func (a *Allowlist) Prefixes() []netip.Prefix {
	if a == nil {
		return nil
	}
	return append([]netip.Prefix(nil), a.prefixes...)
}
