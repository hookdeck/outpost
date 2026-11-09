package e2e_test

import (
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/cmd/e2e/mcpreceiver"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
)

// TestMCP_InvalidEventDefinitions checks that Outpost starts with unusable
// event definitions: MCP Events answers 503, the reason goes only to the log,
// and the rest of the service, including deliveries to existing mcp destinations, runs.
func TestMCP_InvalidEventDefinitions(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpInvalidEventsSuite{})
}

type mcpInvalidEventsSuite struct {
	suite.Suite
	m *mcpSuite
}

func (s *mcpInvalidEventsSuite) SetupSuite() {
	s.m = &mcpSuite{configure: func(cfg *config.Config) {
		withMCPDelivery(cfg)
		cfg.Topics = testutil.TestTopics
		s.Require().NoError(cfg.Experimental.MCP.EventsSchema.UnmarshalText([]byte(`{"events":`)))
	}}
	s.m.SetT(s.T())
	s.m.SetupSuite()
}

func (s *mcpInvalidEventsSuite) SetupTest() {
	s.m.SetT(s.T())
	s.m.SetupTest()
}

func (s *mcpInvalidEventsSuite) TearDownSuite() { s.m.TearDownSuite() }

func (s *mcpInvalidEventsSuite) TestServiceRunsAndMCPEventsIsUnavailable() {
	base := s.m.base
	s.Equal(http.StatusOK, base.doJSON(http.MethodGet, base.apiURL("/healthz"), nil, nil))

	var resp struct {
		Message string `json:"message"`
	}
	status := base.doJSON(http.MethodGet, base.apiURL("/mcp/events/list"), nil, &resp)
	s.Equal(http.StatusServiceUnavailable, status)
	s.Equal("MCP Events unavailable", resp.Message)

	tenant := base.createTenant().ID
	s.NotEmpty(tenant)

	p := s.m.newPath(mcpreceiver.PathConfig{})
	for _, action := range []string{"subscribe", "unsubscribe"} {
		resp.Message = ""
		status := base.doJSON(http.MethodPost, base.apiURL("/tenants/"+tenant+"/mcp/"+action),
			map[string]any{"principal": p.principal, "params": map[string]any{"name": mcpTopic, "delivery": map[string]any{"mode": "webhook", "url": p.url, "secret": p.secret}}}, &resp)
		s.Equal(http.StatusServiceUnavailable, status, action)
		s.Equal("MCP Events unavailable", resp.Message, action)
	}
	s.Empty(s.m.r.Challenges(p.path), "nothing is verified")
}

func (s *mcpInvalidEventsSuite) TestExistingDestinationKeepsDelivering() {
	tenant := s.m.base.createTenant().ID
	p := s.m.newPath(mcpreceiver.PathConfig{Unchecked: true})
	writeDestinationToStore(s.T(), s.m.base.config, models.Destination{
		ID:          p.destID,
		TenantID:    tenant,
		Type:        "mcp",
		Topics:      models.Topics{mcpTopic},
		Config:      models.Config{"url": p.url},
		Credentials: models.Credentials{"secret": p.secret},
	})

	pub := s.m.publishMCP(tenant, map[string]any{"n": 1})
	s.Equal([]string{p.destID}, pub.DestinationIDs)
	events := s.m.r.WaitFor(p.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	s.Equal(pub.ID, events[0].Envelope().EventID)
}
