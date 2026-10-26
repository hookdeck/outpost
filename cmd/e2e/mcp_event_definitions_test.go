package e2e_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/mcpreceiver"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
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

const mcpEventsYAML = `
user.created:
  description: A user was created.
  payload_schema:
    type: object
    properties:
      status: {type: string, enum: [paid, pending]}
      total: {type: number, x-mcp-filter: false}
      amount: {type: number}
user.updated:
  description: A user was updated.
  payload_schema:
    type: object
    properties:
      id: {type: string}
`

type mcpEventDefinitionsSuite struct {
	suite.Suite
	m *mcpSuite
}

func (s *mcpEventDefinitionsSuite) SetupSuite() {
	file := filepath.Join(s.T().TempDir(), "events.yaml")
	s.Require().NoError(os.WriteFile(file, []byte(mcpEventsYAML), 0o600))
	s.m = &mcpSuite{configure: func(cfg *config.Config) {
		withMCPDelivery(cfg)
		cfg.Topics = testutil.TestTopics
		cfg.MCP.EventsFile = file
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
	status := s.m.base.doJSON(http.MethodGet, s.m.base.apiURL("/mcp/events"+query), nil, &page)
	return status, page
}

func properties(schema map[string]any) map[string]any {
	props, _ := schema["properties"].(map[string]any)
	return props
}

func (s *mcpEventDefinitionsSuite) TestEventCatalog() {
	status, page := s.catalog("")
	s.Require().Equal(http.StatusOK, status)
	s.Equal("complete", page.ResultType)
	s.Require().Len(page.Events, 2)
	created, updated := page.Events[0], page.Events[1]
	s.Equal("user.created", created.Name)
	s.Equal("user.updated", updated.Name)
	s.Equal("A user was created.", created.Description)
	s.NotEmpty(created.Delivery)

	in := properties(created.InputSchema)
	_, hasStatus := in["status"]
	_, hasAmount := in["amount"]
	_, hasTotal := in["total"]
	payload := properties(created.PayloadSchema)
	_, payloadHasTotal := payload["total"]
	_, payloadHasStatus := payload["status"]
	_, updatedHasID := properties(updated.InputSchema)["id"]
	ok := hasStatus && hasAmount && !hasTotal && payloadHasTotal && payloadHasStatus && updatedHasID && created.InputSchema["type"] == "object"
	s.m.check(ok, "inputSchema props %v, payloadSchema props %v", keys(in), keys(payload))

	status, page = s.catalog("?topics=user.updated")
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(page.Events, 1)
	s.Equal("user.updated", page.Events[0].Name)

	status, page = s.catalog("?topics=user.deleted")
	s.Require().Equal(http.StatusOK, status)
	s.Empty(page.Events)

	status, page = s.catalog("?limit=1")
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(page.Events, 1)
	s.Equal("user.created", page.Events[0].Name)
	s.Require().NotEmpty(page.NextCursor)
	status, page = s.catalog("?limit=1&cursor=" + page.NextCursor)
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(page.Events, 1)
	s.Equal("user.updated", page.Events[0].Name)
	s.Empty(page.NextCursor)

	tenant := s.m.base.createTenant().ID
	s.m.base.runErrorCases([]errorCase{
		{name: "no auth", method: http.MethodGet, path: "/mcp/events", status: http.StatusUnauthorized, message: "unauthorized"},
		{name: "tenant token", method: http.MethodGet, path: "/mcp/events", auth: s.m.base.tenantAuth(tenant), status: http.StatusForbidden, message: "forbidden"},
		{name: "limit too high", method: http.MethodGet, path: "/mcp/events?limit=1000", auth: s.m.base.adminAuth(), status: http.StatusBadRequest, message: "invalid limit: must be between 1 and 100"},
		{name: "bad cursor", method: http.MethodGet, path: "/mcp/events?cursor=%25%25", auth: s.m.base.adminAuth(), status: http.StatusBadRequest, message: "invalid cursor"},
	})
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (s *mcpEventDefinitionsSuite) TestArgumentsFilterDeliveries() {
	tenant := s.m.base.createTenant().ID
	p := s.m.newPath(mcpreceiver.PathConfig{})
	body := p.body(mcpDestOpts{})
	body["config"].(map[string]any)["arguments"] = `{"status":"paid"}`
	status, raw := s.m.create(tenant, body)
	s.Require().Equal(http.StatusCreated, status, "%s", raw)

	var created struct {
		Filter map[string]any `json:"filter"`
	}
	s.Require().NoError(json.Unmarshal(raw, &created))
	s.NotEmpty(created.Filter, "arguments derive a filter")

	matching := s.m.publishMCP(tenant, map[string]any{"status": "paid", "total": 10})
	events := s.m.r.WaitFor(p.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	s.Equal(matching.ID, events[0].Envelope().EventID)

	other := s.m.publishMCP(tenant, map[string]any{"status": "pending"})
	time.Sleep(mcpRetryWait)
	got := s.m.r.Events(p.path)
	attempts := s.m.attempts(tenant, p.destID)
	ok := len(got) == 1 && len(attempts) == 1 && len(other.DestinationIDs) == 0 && len(matching.DestinationIDs) == 1
	s.m.check(ok, "events %d, attempts %d, destinations matching %v non-matching %v",
		len(got), len(attempts), matching.DestinationIDs, other.DestinationIDs)

	var stored struct {
		Config map[string]any `json:"config"`
		Filter map[string]any `json:"filter"`
	}
	s.Equal(http.StatusOK, s.m.base.doJSON(http.MethodGet, s.m.destURL(tenant, "/"+p.destID), nil, &stored))
	s.Equal(`{"status":"paid"}`, stored.Config["arguments"])
	s.Equal(created.Filter, stored.Filter)
}

func (s *mcpEventDefinitionsSuite) TestErrorResponses_MCPEventDefinitions() {
	tenant := s.m.base.createTenant().ID
	p := s.m.newPath(mcpreceiver.PathConfig{})
	dest := "/tenants/" + tenant + "/destinations"
	admin := s.m.base.adminAuth()

	with := func(mutate func(body map[string]any)) map[string]any {
		body := p.body(mcpDestOpts{destID: idgen.Destination()})
		mutate(body)
		return body
	}
	cases := []errorCase{
		{name: "topic not in catalog", method: http.MethodPost, path: dest, auth: admin,
			body:   with(func(b map[string]any) { b["topics"] = []string{"user.deleted"} }),
			status: http.StatusUnprocessableEntity, message: validationError, detail: "topics failed not_found validation"},
		{name: "argument value outside enum", method: http.MethodPost, path: dest, auth: admin,
			body: with(func(b map[string]any) {
				b["config"].(map[string]any)["arguments"] = `{"status":"bogus"}`
			}),
			status: http.StatusUnprocessableEntity, message: validationError, detail: "config.arguments.status failed invalid validation"},
		{name: "argument not in inputSchema", method: http.MethodPost, path: dest, auth: admin,
			body: with(func(b map[string]any) {
				b["config"].(map[string]any)["arguments"] = `{"total":10}`
			}),
			status: http.StatusUnprocessableEntity, message: validationError, detail: "config.arguments"},
		{name: "arguments not an object", method: http.MethodPost, path: dest, auth: admin,
			body: with(func(b map[string]any) {
				b["config"].(map[string]any)["arguments"] = `["paid"]`
			}),
			status: http.StatusUnprocessableEntity, message: validationError, detail: "config.arguments"},
		{name: "filter with a catalog", method: http.MethodPost, path: dest, auth: admin,
			body: with(func(b map[string]any) {
				b["filter"] = map[string]any{"data": map[string]any{"status": "paid"}}
			}),
			status: http.StatusUnprocessableEntity, message: validationError, detail: "filter failed invalid validation"},
	}

	armed := p.body(mcpDestOpts{})
	armed["config"].(map[string]any)["arguments"] = `{"status":"paid"}`
	status, raw := s.m.create(tenant, armed)
	s.Require().Equal(http.StatusCreated, status, "%s", raw)
	cases = append(cases,
		errorCase{name: "arguments are immutable", method: http.MethodPatch, path: dest + "/" + p.destID, auth: admin,
			body:   map[string]any{"config": map[string]any{"arguments": `{"status":"pending"}`}},
			status: http.StatusUnprocessableEntity, message: validationError, detail: "config.arguments failed immutable validation"},
		errorCase{name: "filter is immutable", method: http.MethodPatch, path: dest + "/" + p.destID, auth: admin,
			body:   map[string]any{"filter": map[string]any{"data": map[string]any{"status": "pending"}}},
			status: http.StatusUnprocessableEntity, message: validationError, detail: "filter failed immutable validation"},
	)

	s.m.base.runErrorCases(cases)
}
