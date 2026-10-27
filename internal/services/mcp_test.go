package services

import (
	"net/netip"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestBuilder(t *testing.T, mutate func(*config.Config)) *ServiceBuilder {
	t.Helper()
	cfg := &config.Config{}
	cfg.InitDefaults()
	if mutate != nil {
		mutate(cfg)
	}
	return &ServiceBuilder{cfg: cfg, logger: logging.NewTestLogger(zap.NewNop())}
}

func TestMCPDisabledBuildsNothing(t *testing.T) {
	b := newTestBuilder(t, nil)

	n, err := b.mcpNetwork()
	require.NoError(t, err)
	assert.Nil(t, n)
	provider, verifier, err := b.mcpProvider(nil)
	require.NoError(t, err)
	assert.Nil(t, provider)
	assert.Nil(t, verifier)
	assert.Nil(t, b.mcpNet)
}

func TestMCPEnabledWiring(t *testing.T) {
	b := newTestBuilder(t, func(c *config.Config) {
		c.MCP.Enabled = true
		c.MCP.RetrySchedule = []int{15, 60}
		c.MCP.AllowInsecureCallbacks = true
		c.Destinations.PrivateAddressAllowlist = []string{"localhost"}
	})

	provider, verifier, err := b.mcpProvider(nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Nil(t, verifier, "services without a verifier store build none")
	assert.NotNil(t, provider.Client)
	assert.NotNil(t, provider.Guard)

	n, err := b.mcpNetwork()
	require.NoError(t, err)
	assert.Same(t, n, b.mcpNet, "the network stack is built once")

	assert.True(t, provider.AllowInsecureHTTP)
	require.NotNil(t, provider.RetryBackoff)
	assert.Equal(t, 2, provider.RetryMaxRetries)
	assert.True(t, n.guard.Allowlist.Contains(netip.MustParseAddr("127.0.0.1")), "the destinations allowlist applies")
}

func TestAllowlistExempt(t *testing.T) {
	allow, err := netguard.ParseAllowlist([]string{"localhost", "192.0.2.0/24"})
	require.NoError(t, err)
	exempt := allowlistExempt(allow)
	assert.True(t, exempt("127.0.0.1"))
	assert.True(t, exempt("192.0.2.7"))
	assert.True(t, exempt("localhost"))
	assert.True(t, exempt("LOCALHOST."))
	assert.False(t, exempt("198.51.100.1"))
	assert.False(t, exempt("receiver.example.com"))

	only4, err := netguard.ParseAllowlist([]string{"127.0.0.0/8"})
	require.NoError(t, err)
	assert.False(t, allowlistExempt(only4)("localhost"), "localhost needs both loopbacks allowed")
}
