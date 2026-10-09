package netguard_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAllowlist(t *testing.T) {
	t.Parallel()
	al, _, err := netguard.ParseAllowlist([]string{
		" 10.0.0.0/8 ", "", "   ", "192.168.1.5", "fd00::/8", "localhost", "::ffff:172.16.0.0/108",
	})
	require.NoError(t, err)

	assert.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.5/32"),
		netip.MustParsePrefix("fd00::/8"),
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
		// IPv4-mapped entries are stored as IPv4.
		netip.MustParsePrefix("172.16.0.0/12"),
	}, al.Prefixes())

	for addr, want := range map[string]bool{
		"10.0.0.0":         true,
		"10.255.255.255":   true,
		"11.0.0.0":         false,
		"192.168.1.5":      true,
		"192.168.1.6":      false,
		"fd12::1":          true,
		"fc00::1":          false,
		"127.0.0.1":        true,
		"127.9.9.9":        true,
		"::1":              true,
		"::2":              false,
		"172.20.0.1":       true,
		"::ffff:10.1.2.3":  true, // mapped addresses are unmapped first
		"::ffff:11.1.2.3":  false,
		"fd00::1%eth0":     false, // zones never match
		"8.8.8.8":          false,
		"2001:4860::8888":  false,
		"::ffff:127.0.0.1": true,
	} {
		assert.Equal(t, want, al.Contains(netip.MustParseAddr(addr)), addr)
	}
	assert.False(t, al.Contains(netip.Addr{}))
}

func TestParseAllowlist_Localhost(t *testing.T) {
	t.Parallel()
	al, warnings, err := netguard.ParseAllowlist([]string{"LocalHost", "127.0.0.0/8", "localhost"})
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}, al.Prefixes(), "duplicates collapse")
	assert.Len(t, warnings, 2, "one loopback warning per range")
}

func TestParseAllowlist_Empty(t *testing.T) {
	t.Parallel()
	al, warnings, err := netguard.ParseAllowlist(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Empty(t, al.Prefixes())
	assert.False(t, al.Contains(netip.MustParseAddr("127.0.0.1")))

	var nilList *netguard.Allowlist
	assert.False(t, nilList.Contains(netip.MustParseAddr("127.0.0.1")))
	assert.Nil(t, nilList.Prefixes())
}

func TestParseAllowlist_Rejects(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{
		"0.0.0.0/0",
		"::/0",
		"::ffff:0.0.0.0/96", // all of IPv4, once unmapped
		"example.com",
		"*.example.com",
		"*",
		"10.0.0.0/33",
		"10.0.0.0/abc",
		"10.0.0",
		"fe80::1%eth0",
		"fe80::1%eth0/64",
		"[::1]",
		"127.0.0.1:8080",
		"localhost:8080",
		"0x7f.0.0.1",
		"2130706433",
	} {
		al, warnings, err := netguard.ParseAllowlist([]string{"127.0.0.1", entry})
		assert.Error(t, err, entry)
		assert.Nil(t, al, entry)
		assert.Nil(t, warnings, entry)
	}

	// Every bad entry is reported at once.
	_, _, err := netguard.ParseAllowlist([]string{"bad-one", "10.0.0.0/8", "0.0.0.0/0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"bad-one"`)
	assert.Contains(t, err.Error(), `"0.0.0.0/0"`)
	assert.NotContains(t, err.Error(), `"10.0.0.0/8"`)
}

func TestParseAllowlist_Warnings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		entry string
		warn  string // substring of the single expected warning; "" = none
	}{
		{"10.0.0.0/8", "Private-Use"},
		{"172.16.5.0/24", "Private-Use"},
		{"192.168.0.0/16", "Private-Use"},
		{"127.0.0.1", "Loopback"},
		{"169.254.169.254", "Link Local"},
		{"100.64.0.0/10", "Shared Address Space"},
		{"::1", "Loopback Address"},
		{"fd00::/8", "Unique-Local"},
		{"fe80::/10", "Link-Local Unicast"},
		{"2001:db8::/32", "Documentation"},
		{"64:ff9b::/96", "IPv4-IPv6 Translat."},
		{"64:ff9b::a00:1", "embeds Private-Use"},
		{"2002::/16", "6to4"},
		{"ff00::/8", "outside global unicast"},
		{"::/1", ""}, // asserted below: some warning
		{"0.0.0.0/1", "This network"},
		{"8.0.0.0/7", ""},
		{"8.8.8.8", ""},
		{"192.0.0.9", ""}, // global exception inside 192.0.0.0/24
		{"192.31.196.0/24", ""},
		{"2001:4860::/32", ""},
		{"2606:4700::/32", ""},
	}
	for _, tc := range cases {
		_, warnings, err := netguard.ParseAllowlist([]string{tc.entry})
		require.NoError(t, err, tc.entry)
		switch {
		case tc.entry == "::/1":
			assert.Len(t, warnings, 1, tc.entry)
		case tc.warn == "":
			assert.Empty(t, warnings, tc.entry)
		default:
			require.Len(t, warnings, 1, tc.entry)
			assert.Contains(t, warnings[0], tc.warn, tc.entry)
			assert.Contains(t, warnings[0], tc.entry, tc.entry)
		}
	}
}

func TestParseAllowlist_HostBits(t *testing.T) {
	t.Parallel()
	al, warnings, err := netguard.ParseAllowlist([]string{"10.1.2.3/8"})
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, al.Prefixes())
	require.Len(t, warnings, 2)
	assert.True(t, strings.Contains(warnings[0], "host bits") || strings.Contains(warnings[1], "host bits"))
}
