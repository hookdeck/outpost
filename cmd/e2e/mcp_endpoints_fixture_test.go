package e2e_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpserver"
)

func init() { registerMCPFixture(endpointsFixture{}) }

// endpointsFixture is an MCP server that forwards events/subscribe and
// events/unsubscribe to Outpost's /mcp/subscribe and /mcp/unsubscribe.
type endpointsFixture struct{}

func (endpointsFixture) Name() string { return "endpoints" }

func (endpointsFixture) ConfigureOutpost(cfg *config.Config) {
	cfg.Experimental.MCP.SubscriptionTTLDefaultSeconds = int(mcpTTL.Default / time.Second)
	cfg.Experimental.MCP.SubscriptionTTLMinSeconds = int(mcpTTL.Min / time.Second)
	cfg.Experimental.MCP.SubscriptionTTLMaxSeconds = int(mcpTTL.Max / time.Second)
}

func (endpointsFixture) Start(t *testing.T, env mcpFixtureEnv) mcpFixtureServer {
	principals := make(map[string]mcpserver.Principal, len(env.Principals))
	for token, p := range env.Principals {
		principals[token] = mcpserver.Principal{Tenant: p.Tenant, AllowedTopics: p.AllowedTopics}
	}
	return &endpointsServer{Server: httptest.NewServer(mcpserver.New(mcpserver.Config{
		OutpostURL: env.OutpostURL,
		APIKey:     env.APIKey,
		Principals: principals,
	}))}
}

type endpointsServer struct {
	*httptest.Server
}

func (s *endpointsServer) URL() string { return s.Server.URL }
