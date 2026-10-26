package services

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/redis"
)

type mcpNetwork struct {
	allowlist *netguard.Allowlist
	guard     *netguard.Guard
	client    *http.Client
}

func (b *ServiceBuilder) mcpNetwork() (*mcpNetwork, error) {
	if !b.cfg.MCP.Enabled {
		return nil, nil
	}
	if b.mcpNet != nil {
		return b.mcpNet, nil
	}
	allowlist, err := netguard.ParseAllowlist(b.cfg.Destinations.PrivateAddressAllowlist)
	if err != nil {
		return nil, err
	}
	guard := &netguard.Guard{Allowlist: allowlist}
	pool := destregistry.SizeFanOutPool(b.cfg.DeliveryMaxConcurrency)
	emeter, err := emetrics.New()
	if err != nil {
		return nil, err
	}
	clientCfg := destregistry.HTTPClientConfig{
		Pool: pool,
		OnConnection: func(reused bool) {
			emeter.DeliveryConnection(context.Background(), reused, destmcp.Type)
		},
	}
	if ua := b.cfg.Destinations.ToConfig(b.cfg).UserAgent; ua != "" {
		clientCfg.UserAgent = &ua
	}
	client, err := destmcp.NewHTTPClient(guard, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("mcp callback client: %w", err)
	}
	b.mcpNet = &mcpNetwork{allowlist: allowlist, guard: guard, client: client}
	return b.mcpNet, nil
}

func (b *ServiceBuilder) mcpProvider(redisClient redis.Client) (*destmcp.Config, *mcpevents.Verifier, error) {
	n, err := b.mcpNetwork()
	if err != nil || n == nil {
		return nil, nil, err
	}
	retryBackoff, retryMaxRetries := b.cfg.MCPRetryBackoff()
	cfg := &destmcp.Config{
		Client:            n.client,
		Guard:             n.guard,
		Catalog:           b.cfg.MCPCatalog(),
		UserAgent:         b.cfg.Destinations.ToConfig(b.cfg).UserAgent,
		AllowInsecureHTTP: b.cfg.MCP.AllowInsecureCallbacks,
		RetryBackoff:      retryBackoff,
		RetryMaxRetries:   retryMaxRetries,
	}
	if redisClient == nil {
		return cfg, nil, nil
	}
	verifier, err := mcpevents.NewVerifier(mcpevents.VerifierConfig{
		Client: n.client,
		Store:  mcpevents.NewRedisVerificationStore(redisClient, b.cfg.DeploymentID),
		TTL:    time.Duration(b.cfg.MCP.VerificationTTLSeconds) * time.Second,
		Exempt: allowlistExempt(n.allowlist),
		Logger: b.logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create mcp verifier: %w", err)
	}
	cfg.Verifier = verifier
	return cfg, verifier, nil
}

func allowlistExempt(allowlist *netguard.Allowlist) func(host string) bool {
	loopback := allowlist.Contains(netip.MustParseAddr("127.0.0.1")) &&
		allowlist.Contains(netip.IPv6Loopback())
	return func(host string) bool {
		if addr, err := netip.ParseAddr(host); err == nil {
			return allowlist.Contains(addr)
		}
		return loopback && strings.EqualFold(strings.TrimSuffix(host, "."), "localhost")
	}
}
