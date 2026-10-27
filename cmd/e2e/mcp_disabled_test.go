package e2e_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
)

// TestMCP_Disabled checks the behavior with MCP off: type "mcp" is rejected
// like any unknown type and is not listed, and a stored mcp destination fails
// each event once without retries.
func TestMCP_Disabled(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpDisabledSuite{})
}

type mcpDisabledSuite struct {
	suite.Suite
	base *basicSuite
}

func (s *mcpDisabledSuite) SetupSuite() {
	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		configure: func(cfg *config.Config) {
			withMCPDisabled(cfg)
			cfg.Alert.ConsecutiveFailureCount = config.NewOptionalString("1")
			cfg.Topics = testutil.TestTopics
		},
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
}

func (s *mcpDisabledSuite) SetupTest() { s.base.SetT(s.T()) }

func (s *mcpDisabledSuite) TearDownSuite() { s.base.TearDownSuite() }

func (s *mcpDisabledSuite) TestTypeMCPRejectedLikeUnknown() {
	tenant := s.base.createTenant().ID
	post := func(typ string) (int, string) {
		var raw json.RawMessage
		status := s.base.doJSON(http.MethodPost, s.base.apiURL("/tenants/"+tenant+"/destinations"), map[string]any{
			"type": typ, "topics": []string{mcpTopic},
			"config":      map[string]any{"url": "https://203.0.113.10/"},
			"credentials": map[string]any{"secret": newWhsec()},
		}, &raw)
		return status, strings.ReplaceAll(string(raw), typ, "<type>")
	}
	mcpStatus, mcpBody := post("mcp")
	unknownStatus, unknownBody := post("no_such_type")
	s.Equal(unknownStatus, mcpStatus)
	s.Equal(unknownBody, mcpBody)
	s.GreaterOrEqual(mcpStatus, 400)

	var types json.RawMessage
	s.Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiURL("/destination-types"), nil, &types))
	s.NotContains(string(types), `"mcp"`)
	s.Equal(http.StatusNotFound, s.base.doJSON(http.MethodGet, s.base.apiURL("/destination-types/mcp"), nil, nil))
}

func (s *mcpDisabledSuite) attempts(tenantID, destID string) []mcpAttempt {
	s.T().Helper()
	var resp struct {
		Models []mcpAttempt `json:"models"`
	}
	status := s.base.doJSON(http.MethodGet, s.base.apiURL("/attempts?tenant_id="+tenantID+"&destination_id="+destID+"&limit=100"), nil, &resp)
	s.Require().Equal(http.StatusOK, status)
	return resp.Models
}

// A destination created while MCP was on gets one failed attempt per event
// and no retries once MCP is off.
func (s *mcpDisabledSuite) TestExistingDestinationFailsWithoutRetry() {
	tenant := s.base.createTenant().ID
	destID := idgen.Destination()
	writeDestinationToStore(s.T(), s.base.config, models.Destination{
		ID:          destID,
		TenantID:    tenant,
		Type:        "mcp",
		Topics:      models.Topics{mcpTopic},
		Config:      models.Config{"url": "https://203.0.113.10/"},
		Credentials: models.Credentials{"secret": newWhsec()},
	})
	s.base.publish(tenant, mcpTopic, map[string]any{"n": 1}, withRetry())

	deadline := time.Now().Add(attemptPollTimeout)
	var got []mcpAttempt
	for len(got) < 1 && time.Now().Before(deadline) {
		got = s.attempts(tenant, destID)
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().Len(got, 1)
	s.Equal("failed", got[0].Status)

	time.Sleep(2 * time.Second)
	s.Len(s.attempts(tenant, destID), 1)
	s.Nil(s.base.getDestination(tenant, destID).DisabledAt)
}
