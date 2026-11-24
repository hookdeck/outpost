package e2e_test

import (
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/suite"
)

// TestMCP_Limits checks the per-tenant destination limit against
// subscriptions.
func TestMCP_Limits(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpLimitsSuite{})
}

const mcpLimitsMaxDestinations = 2

type mcpLimitsSuite struct {
	suite.Suite
	m *mcpSuite
}

func (s *mcpLimitsSuite) SetupSuite() {
	s.m = &mcpSuite{configure: func(cfg *config.Config) {
		withMCPDelivery(cfg)
		cfg.MaxDestinationsPerTenant = mcpLimitsMaxDestinations
	}}
	s.m.SetT(s.T())
	s.m.SetupSuite()
}

func (s *mcpLimitsSuite) SetupTest() { s.m.SetupTest() }

func (s *mcpLimitsSuite) TearDownSuite() { s.m.TearDownSuite() }

func (s *mcpLimitsSuite) tenant(id string) models.Tenant {
	s.T().Helper()
	var got models.Tenant
	s.Require().Equal(http.StatusOK, s.m.base.doJSON(http.MethodGet, s.m.base.apiURL("/tenants/"+id), nil, &got))
	return got
}

// Subscriptions count toward the tenant's topics, not toward the limit or the
// tenant's destination count.
func (s *mcpLimitsSuite) TestSubscriptionsDoNotCount() {
	tenant := s.m.base.createTenant().ID
	for range 3 {
		s.m.mustSubscribe(tenant, s.m.newPath(tenant, mcpreceiver.PathConfig{}), mcpSubscribeOpts{})
	}
	got := s.tenant(tenant)
	s.Zero(got.DestinationsCount)
	s.Equal([]string{mcpTopic}, got.Topics)

	for range mcpLimitsMaxDestinations {
		_, url := s.m.r.Register(mcpreceiver.PathConfig{Unchecked: true})
		s.Require().Equal(http.StatusCreated, s.m.base.doJSON(http.MethodPost, s.m.destURL(tenant), map[string]any{
			"id": idgen.Destination(), "type": "webhook", "topics": []string{mcpTopic}, "config": map[string]any{"url": url},
		}, nil))
	}
	_, url := s.m.r.Register(mcpreceiver.PathConfig{Unchecked: true})
	s.Equal(http.StatusBadRequest, s.m.base.doJSON(http.MethodPost, s.m.destURL(tenant), map[string]any{
		"id": idgen.Destination(), "type": "webhook", "topics": []string{mcpTopic}, "config": map[string]any{"url": url},
	}, nil), "webhooks fill the limit")

	got = s.tenant(tenant)
	s.Equal(mcpLimitsMaxDestinations, got.DestinationsCount)
	s.Equal([]string{mcpTopic}, got.Topics)

	s.m.mustSubscribe(tenant, s.m.newPath(tenant, mcpreceiver.PathConfig{}), mcpSubscribeOpts{})
}
