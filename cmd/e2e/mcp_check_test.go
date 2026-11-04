package e2e_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpcheck"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpserver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
)

// TestMCP_Check runs the conformance checks against the reference server in
// front of Outpost.
func TestMCP_Check(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpCheckSuite{})
}

type mcpCheckSuite struct {
	suite.Suite
	m      *mcpSuite
	server *httptest.Server
	tenant string
}

func (s *mcpCheckSuite) SetupSuite() {
	file := filepath.Join(s.T().TempDir(), "events.yaml")
	s.Require().NoError(os.WriteFile(file, []byte(mcpEventsYAML), 0o600))
	s.m = &mcpSuite{configure: func(cfg *config.Config) {
		withMCPDelivery(cfg)
		cfg.Topics = testutil.TestTopics
		s.Require().NoError(cfg.Experimental.MCP.EventsSchema.UnmarshalText([]byte(file)))
	}}
	s.m.SetT(s.T())
	s.m.SetupSuite()
	s.tenant = s.m.base.createTenant().ID
	s.server = httptest.NewServer(mcpserver.New(mcpserver.Config{
		OutpostURL:    s.m.base.apiURL(""),
		APIKey:        s.m.base.config.APIKey,
		DefaultTenant: s.tenant,
	}))
}

func (s *mcpCheckSuite) SetupTest() {
	s.m.SetT(s.T())
	s.m.SetupTest()
}

func (s *mcpCheckSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Close()
	}
	if s.m != nil {
		s.m.TearDownSuite()
	}
}

func (s *mcpCheckSuite) TestCheck() {
	report := mcpcheck.Run(context.Background(), mcpcheck.Options{
		ServerURL: s.server.URL,
		Principal: "check-" + idgen.String(),
		Receiver:  s.m.r,
		Publish: func(_ context.Context, event string, data map[string]any) (string, error) {
			return s.m.base.publish(s.tenant, event, data, withRetry()).ID, nil
		},
		Wait:  30 * time.Second,
		Quiet: 3 * time.Second,
		Logf:  s.T().Logf,
	})
	s.T().Log("\n" + report.String())
	for _, r := range report {
		switch r.Status {
		case mcpcheck.Fail, mcpcheck.Skip:
			s.Failf("check not passing", "%s %s: %s", r.Status, r.ID, r.Detail)
		case mcpcheck.Warn:
			s.Equal("subscribe.https-required", r.ID, "%s: %s", r.ID, r.Detail)
			s.Equal("server accepted an http callback (development exception); SEP requires https", r.Detail)
		}
	}
}
