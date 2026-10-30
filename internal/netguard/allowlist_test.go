package netguard_test

import (
	"net/netip"
	"testing"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAllowlist(t *testing.T) {
	t.Parallel()
	al, err := netguard.ParseAllowlist([]string{
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
	al, err := netguard.ParseAllowlist([]string{"LocalHost", "127.0.0.0/8", "localhost"})
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}, al.Prefixes(), "duplicates collapse")
}

func TestParseAllowlist_Empty(t *testing.T) {
	t.Parallel()
	al, err := netguard.ParseAllowlist(nil)
	require.NoError(t, err)
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
		"10.0.0.1/8",
	} {
		al, err := netguard.ParseAllowlist([]string{"127.0.0.1", entry})
		assert.Error(t, err, entry)
		assert.Nil(t, al, entry)
	}

	// Every bad entry is reported at once.
	_, err := netguard.ParseAllowlist([]string{"bad-one", "10.0.0.0/8", "0.0.0.0/0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"bad-one"`)
	assert.Contains(t, err.Error(), `"0.0.0.0/0"`)
	assert.NotContains(t, err.Error(), `"10.0.0.0/8"`)
}

func TestParseAllowlist_HostBits(t *testing.T) {
	t.Parallel()
	for entry, masked := range map[string]string{
		"10.1.2.3/8":          "10.0.0.0/8",
		"fd00::1/8":           "fd00::/8",
		"::ffff:10.1.2.3/104": "10.0.0.0/8",
		"192.168.1.1/24":      "192.168.1.0/24",
	} {
		al, err := netguard.ParseAllowlist([]string{entry})
		require.Error(t, err, entry)
		assert.Nil(t, al, entry)
		assert.Contains(t, err.Error(), "use "+masked, entry)
	}
}
