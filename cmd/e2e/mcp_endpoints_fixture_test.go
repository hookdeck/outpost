package e2e_test

import (
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpserver"
)

func init() { registerMCPFixture(endpointsFixture{}) }

// endpointsFixture is an MCP server that forwards events/subscribe and
// events/unsubscribe to Outpost's /mcp/events/subscribe and /mcp/events/unsubscribe.
type endpointsFixture struct{}

func (endpointsFixture) Name() string { return "endpoints" }

func (endpointsFixture) ConfigureOutpost(cfg *config.Config) {}

func (endpointsFixture) Start(t *testing.T, env mcpFixtureEnv) mcpFixtureServer {
	users := make(map[string]mcpserver.User, len(env.Users))
	for token, u := range env.Users {
		users[token] = mcpserver.User{Tenant: u.Tenant, AllowedTopics: u.AllowedTopics}
	}
	return &endpointsServer{Server: httptest.NewServer(mcpserver.New(mcpserver.Config{
		OutpostURL: env.OutpostURL,
		APIKey:     env.APIKey,
		Users:      users,
	}))}
}

type endpointsServer struct {
	*httptest.Server
}

func (s *endpointsServer) URL() string { return s.Server.URL }
