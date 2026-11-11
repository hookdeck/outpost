package e2e_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/mcpreceiver"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v3"
)

// TestMCP_EventDefinitions runs the MCP event definitions end to end: the catalog
// endpoint and subscription arguments.
func TestMCP_EventDefinitions(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpEventDefinitionsSuite{})
}

// mcpEventsYAML lists user.updated first to show the configured order is kept.
// user.updated declares no arguments.
const mcpEventsYAML = `
events:
  - name: user.updated
    description: A user was updated.
    inputSchema: {type: object, properties: {}}
    payloadSchema:
      type: object
      properties:
        id: {type: string}
  - name: user.created
    description: A user was created.
    inputSchema:
      type: object
      properties:
        status: {type: string, enum: [paid, pending]}
        amount: {type: number}
    payloadSchema:
      type: object
      properties:
        status: {type: string, enum: [paid, pending]}
        total: {type: number}
        amount: {type: number}
`

// mcpEventsWithDerivedYAML adds user.deleted, which omits inputSchema: its
// scalar fields are the arguments.
const mcpEventsWithDerivedYAML = mcpEventsYAML + `
  - name: user.deleted
    description: A user was deleted.
    payloadSchema:
      type: object
      properties:
        id: {type: string}
        active: {type: boolean, description: Whether the user was active.}
        tags: {type: array, items: {type: string}}
        profile: {type: object}
`

type mcpEventDefinitionsSuite struct {
	suite.Suite
	m *mcpSuite
}

func (s *mcpEventDefinitionsSuite) SetupSuite() {
	file := filepath.Join(s.T().TempDir(), "events.yaml")
	s.Require().NoError(os.WriteFile(file, []byte(mcpEventsWithDerivedYAML), 0o600))
	s.m = &mcpSuite{configure: func(cfg *config.Config) {
		withMCPDelivery(cfg)
		cfg.Topics = append(slices.Clone(testutil.TestTopics), "user.archived")
		s.Require().NoError(cfg.Experimental.MCP.EventsSchema.UnmarshalText([]byte(file)))
	}}
	s.m.SetT(s.T())
	s.m.SetupSuite()
}

func (s *mcpEventDefinitionsSuite) SetupTest() {
	s.m.SetT(s.T())
	s.m.SetupTest()
}

func (s *mcpEventDefinitionsSuite) TearDownSuite() { s.m.TearDownSuite() }

type catalogEvent struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Delivery      json.RawMessage `json:"delivery"`
	InputSchema   map[string]any  `json:"inputSchema"`
	PayloadSchema map[string]any  `json:"payloadSchema"`
}

type catalogPage struct {
	ResultType string         `json:"resultType"`
	Events     []catalogEvent `json:"events"`
	NextCursor string         `json:"nextCursor"`
}

func (s *mcpEventDefinitionsSuite) catalog(query string) (int, catalogPage) {
	s.T().Helper()
	var page catalogPage
	status := s.m.base.doJSON(http.MethodGet, s.m.base.apiURL("/mcp/events/list"+query), nil, &page)
	return status, page
}

func (s *mcpEventDefinitionsSuite) TestEventCatalog() {
	var want struct {
		Events []map[string]any `json:"events"`
	}
	s.Require().NoError(yaml.Unmarshal([]byte(mcpEventsWithDerivedYAML), &want))
	for _, ev := range want.Events {
		ev["delivery"] = []any{"webhook"}
		if _, ok := ev["inputSchema"]; !ok {
			ev["inputSchema"] = map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string"},
					"active": map[string]any{"type": "boolean", "description": "Whether the user was active."},
				},
				"additionalProperties": false,
			}
		}
	}

	status, page := s.catalog("")
	s.Require().Equal(http.StatusOK, status)
	s.Equal("complete", page.ResultType)
	s.Require().Len(page.Events, 3)
	got, err := json.Marshal(map[string]any{"events": page.Events})
	s.Require().NoError(err)
	wantJSON, err := json.Marshal(want)
	s.Require().NoError(err)
	s.JSONEq(string(wantJSON), string(got))
	s.Equal("user.updated", page.Events[0].Name, "configured order")
	s.Equal("user.created", page.Events[1].Name)
	s.Equal("user.deleted", page.Events[2].Name)

	status, page = s.catalog("?topics=user.updated")
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(page.Events, 1)
	s.Equal("user.updated", page.Events[0].Name)

	status, page = s.catalog("?topics=user.archived")
	s.Require().Equal(http.StatusOK, status)
	s.Empty(page.Events)

	status, page = s.catalog("?limit=1")
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(page.Events, 1)
	s.Equal("user.updated", page.Events[0].Name)
	s.Require().NotEmpty(page.NextCursor)
	status, page = s.catalog("?limit=1&cursor=" + page.NextCursor)
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(page.Events, 1)
	s.Equal("user.created", page.Events[0].Name)
	status, page = s.catalog("?limit=1&cursor=" + page.NextCursor)
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(page.Events, 1)
	s.Equal("user.deleted", page.Events[0].Name)
	s.Empty(page.NextCursor)

	tenant := s.m.base.createTenant().ID
	s.m.base.runErrorCases([]errorCase{
		{name: "no auth", method: http.MethodGet, path: "/mcp/events/list", status: http.StatusUnauthorized, message: "unauthorized"},
		{name: "tenant token", method: http.MethodGet, path: "/mcp/events/list", auth: s.m.base.tenantAuth(tenant), status: http.StatusForbidden, message: "forbidden"},
		{name: "limit too high", method: http.MethodGet, path: "/mcp/events/list?limit=1000", auth: s.m.base.adminAuth(), status: http.StatusBadRequest, message: "invalid limit: must be between 1 and 100"},
		{name: "bad cursor", method: http.MethodGet, path: "/mcp/events/list?cursor=%25%25", auth: s.m.base.adminAuth(), status: http.StatusBadRequest, message: "invalid cursor"},
	})
}

func (s *mcpEventDefinitionsSuite) TestArgumentsFilterDeliveries() {
	tenant := s.m.base.createTenant().ID
	p := s.m.withArguments(s.m.newPath(tenant, mcpreceiver.PathConfig{}), `{"status":"paid"}`)
	s.m.mustSubscribe(tenant, p, mcpSubscribeOpts{})

	matching := s.m.publishMCP(tenant, map[string]any{"status": "paid", "total": 10})
	events := s.m.r.WaitFor(p.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	s.Equal(matching.ID, events[0].Envelope().EventID)

	other := s.m.publishMCP(tenant, map[string]any{"status": "pending"})
	unrelated := s.m.publishMCP(tenant, map[string]any{"total": 10})
	s.Empty(unrelated.DestinationIDs, "the argument must match")
	time.Sleep(mcpRetryWait)
	got := s.m.r.Events(p.path)
	attempts := s.m.attempts(tenant, p.destID)
	ok := len(got) == 1 && len(attempts) == 1 && len(other.DestinationIDs) == 0 && len(matching.DestinationIDs) == 1
	s.m.check(ok, "events %d, attempts %d, destinations matching %v non-matching %v",
		len(got), len(attempts), matching.DestinationIDs, other.DestinationIDs)

	stored := readDestinationFromStore(s.T(), s.m.base.config, tenant, p.destID)
	s.Require().NotNil(stored)
	s.Equal(`{"status":"paid"}`, stored.Config["arguments"])
	s.Equal(models.Filter{"data": map[string]any{"status": "paid"}}, stored.Filter, "arguments derive a filter")
}

// An event without inputSchema takes every scalar field as an argument.
func (s *mcpEventDefinitionsSuite) TestDerivedArgumentsFilterDeliveries() {
	const topic, args = "user.deleted", `{"active":true,"id":"u1"}`
	tenant := s.m.base.createTenant().ID
	p := s.m.newPath(tenant, mcpreceiver.PathConfig{})
	_, normalized, err := mcpevents.NormalizeCallbackURL(p.url)
	s.Require().NoError(err)
	id := mcpevents.DeriveSubscriptionID(tenant, normalized, topic, json.RawMessage(args))
	s.m.r.Set(p.path, mcpreceiver.PathConfig{Secrets: []string{p.secret}, SubscriptionID: id})

	var raw json.RawMessage
	status := s.m.base.doJSON(http.MethodPost, s.m.base.apiURL("/tenants/"+tenant+"/mcp/events/subscribe"), map[string]any{
		"name":      topic,
		"arguments": json.RawMessage(args),
		"delivery":  map[string]any{"mode": "webhook", "url": p.url, "secret": p.secret},
	}, &raw)
	s.Require().Equal(http.StatusOK, status, "%s", raw)

	publish := func(data map[string]any) publishResponse {
		return s.m.base.publish(tenant, topic, data, withRetry())
	}
	hit := publish(map[string]any{"id": "u1", "active": true, "tags": []string{"a"}})
	events := s.m.r.WaitFor(p.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	s.Equal(hit.ID, events[0].Envelope().EventID)
	otherID := publish(map[string]any{"id": "u2", "active": true})
	inactive := publish(map[string]any{"id": "u1", "active": false})
	s.m.check(len(hit.DestinationIDs) == 1 && len(otherID.DestinationIDs) == 0 && len(inactive.DestinationIDs) == 0,
		"matched: id u1 active %v, id u2 %v, inactive %v", hit.DestinationIDs, otherID.DestinationIDs, inactive.DestinationIDs)
}

func (s *mcpEventDefinitionsSuite) TestNumericArgumentMatchesByEquality() {
	tenant := s.m.base.createTenant().ID
	p := s.m.withArguments(s.m.newPath(tenant, mcpreceiver.PathConfig{}), `{"amount":10}`)
	s.m.mustSubscribe(tenant, p, mcpSubscribeOpts{})

	hit := s.m.publishMCP(tenant, map[string]any{"amount": 10})
	miss := s.m.publishMCP(tenant, map[string]any{"amount": 11})
	s.m.check(len(hit.DestinationIDs) == 1 && len(miss.DestinationIDs) == 0, "amount 10 matched %v, amount 11 matched %v", hit.DestinationIDs, miss.DestinationIDs)
}

func (s *mcpEventDefinitionsSuite) TestSubscribeErrors_MCPEventDefinitions() {
	tenant := s.m.base.createTenant().ID
	cases := []struct {
		name    string
		topic   string
		args    string
		code    int
		message string
	}{
		{"event not in the definitions", "user.archived", ``, -32023, "NotFound"},
		{"argument value outside enum", "user.created", `{"status":"bogus"}`, -32602, "InvalidParams"},
		{"argument not in inputSchema", "user.created", `{"total":10}`, -32602, "InvalidParams"},
		{"list value", "user.created", `{"status":["paid"]}`, -32602, "InvalidParams"},
		{"operator object", "user.created", `{"amount":{"$gt":1}}`, -32602, "InvalidParams"},
		{"arguments on a no-argument event", "user.updated", `{"id":"x"}`, -32602, "InvalidParams"},
		{"derived argument of the wrong type", "user.deleted", `{"active":"yes"}`, -32602, "InvalidParams"},
		{"non-scalar field is not a derived argument", "user.deleted", `{"tags":["a"]}`, -32602, "InvalidParams"},
		{"arguments not an object", "user.created", `["paid"]`, -32602, "InvalidParams"},
	}
	for _, c := range cases {
		p := s.m.newPath(tenant, mcpreceiver.PathConfig{})
		params := map[string]any{
			"name":     c.topic,
			"delivery": map[string]any{"mode": "webhook", "url": p.url, "secret": p.secret},
		}
		if c.args != "" {
			params["arguments"] = json.RawMessage(c.args)
		}
		var raw json.RawMessage
		status := s.m.base.doJSON(http.MethodPost, s.m.base.apiURL("/tenants/"+tenant+"/mcp/events/subscribe"),
			params, &raw)
		failure := parseMCPError(raw)
		s.m.check(status == http.StatusUnprocessableEntity && failure.MCPError.Code == c.code && failure.MCPError.Message == c.message,
			"%s: status %d body %s", c.name, status, raw)
		s.Empty(s.m.r.Challenges(p.path), "%s: no challenge for a refused subscription", c.name)
	}
}
