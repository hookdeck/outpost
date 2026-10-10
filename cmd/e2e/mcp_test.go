package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/suite"
)

// TestE2E_MCP runs MCP Events end to end: events/list, subscribe with
// callback verification, signed deliveries, filters, retries and the MCP
// delivery rules, refreshes, secret rotation, unsubscribe, expiry,
// revocation, auto-disable with parked retries, the v1/v2 split and the
// mcp_error mapping. The callback is a plain-http receiver on 127.0.0.1,
// allowlisted.
func TestE2E_MCP(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpSuite{})
}

// TestE2E_MCP_WithDeploymentID runs the MCP suite with a deployment ID, so
// every MCP key (indexes, parked retries, verification cache, delivery
// status, locks, applied topic schemas) is prefixed.
func TestE2E_MCP_WithDeploymentID(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpSuite{deploymentID: "dp_mcp_e2e"})
}

// Suite settings.
const (
	// mcpPrincipalLimit is MAX_MCP_SUBSCRIPTIONS_PER_PRINCIPAL.
	mcpPrincipalLimit = 3
	// mcpAutoDisableCount consecutive failures auto-disable a destination.
	mcpAutoDisableCount = 5
	// globalRetryMaxLimit differs from MCP_RETRY_SCHEDULE's 3 retries, so
	// the tests show which one mcp destinations follow.
	globalRetryMaxLimit = 5
)

type mcpSuite struct {
	suite.Suite
	deploymentID string
	base         *basicSuite
	receiver     *mcpReceiver
}

func (s *mcpSuite) SetupSuite() {
	s.receiver = newMCPReceiver()
	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		deploymentID:   s.deploymentID,
		configure: func(cfg *config.Config) {
			withMCPTopics(cfg, orderCreatedSchema)
			withMCPTestSettings(cfg)
			// TestExpiry works with an expired subscription the sweep
			// hasn't deleted yet: a 30s interval gives a 60s grace.
			// TestE2E_MCP_ExpirySweep covers the sweep.
			cfg.MCP.ExpirySweepInterval = config.Duration(30 * time.Second)
			cfg.MCP.ServerURL = "https://mcp.example.com/mcp"
			cfg.MaxMCPSubscriptionsPerPrincipal = mcpPrincipalLimit
			cfg.RetryMaxLimit = globalRetryMaxLimit
			// Few enough failures for the auto-disable test to fit in a
			// few seconds; more than a fully retried event (4 attempts).
			cfg.Alert.ConsecutiveFailureCount = config.NewOptionalString(strconv.Itoa(mcpAutoDisableCount))
		},
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
}

func (s *mcpSuite) SetupTest() {
	s.base.SetT(s.T())
	s.base.opeventsServer.Reset()
}

func (s *mcpSuite) TearDownSuite() {
	s.base.TearDownSuite()
	s.receiver.Close()
}

// =============================================================================
// Helpers
// =============================================================================

func newMCPPrincipal() string { return "user_" + idgen.String() }

// newSubscription returns a subscription of a fresh principal to a fresh
// callback path.
func (s *mcpSuite) newSubscription(name string, args map[string]any) (mcpSubscription, string) {
	path, callbackURL := s.receiver.newPath()
	return mcpSubscription{
		Principal: newMCPPrincipal(),
		Name:      name,
		Arguments: args,
		URL:       callbackURL,
		Secret:    newStandardWebhooksSecret(),
	}, path
}

func (s *mcpSuite) subscriptionsURL(tenantID string) string {
	return s.base.apiV2URL("/tenants/" + tenantID + "/mcp/subscriptions")
}

// put sends a subscribe request and returns the status and raw body.
func (s *mcpSuite) put(tenantID string, body any) (int, json.RawMessage) {
	s.T().Helper()
	var raw json.RawMessage
	status := s.base.doJSON(http.MethodPut, s.subscriptionsURL(tenantID), body, &raw)
	return status, raw
}

// subscribe subscribes and requires success.
func (s *mcpSuite) subscribe(tenantID string, sub mcpSubscription) mcpSubscribeResult {
	s.T().Helper()
	status, raw := s.put(tenantID, sub.body())
	s.Require().Equal(http.StatusOK, status, "subscribe: %s", raw)
	var result mcpSubscribeResult
	s.Require().NoError(json.Unmarshal(raw, &result))
	s.Require().Equal(sub.id(s.T()), result.ID)
	return result
}

// requireMCPError requires a 422 mcp_error of kind with data.
func (s *mcpSuite) requireMCPError(status int, raw json.RawMessage, kind string, code int, message string, data map[string]any) {
	s.T().Helper()
	s.Require().Equal(http.StatusUnprocessableEntity, status, "body: %s", raw)
	var top map[string]json.RawMessage
	s.Require().NoError(json.Unmarshal(raw, &top))
	s.Require().Len(top, 1, "the body is only {\"mcp_error\":{...}}: %s", raw)
	var body mcpErrorBody
	s.Require().NoError(json.Unmarshal(raw, &body))
	s.Equal(kind, body.MCPError.Kind, "%s", raw)
	s.Equal(code, body.MCPError.Code, "%s", raw)
	s.Equal(message, body.MCPError.Message, "%s", raw)
	s.Equal(data, body.MCPError.Data, "%s", raw)
}

// unsubscribe sends an unsubscribe request and returns the raw 200 body.
func (s *mcpSuite) unsubscribe(tenantID string, body any) string {
	s.T().Helper()
	var raw json.RawMessage
	status := s.base.doJSON(http.MethodPost, s.subscriptionsURL(tenantID)+"/unsubscribe", body, &raw)
	s.Require().Equal(http.StatusOK, status, "unsubscribe: %s", raw)
	return string(raw)
}

// publishV2 publishes through API v2, which lists MCP subscriptions in
// destination_ids.
func (s *mcpSuite) publishV2(tenantID, topic string, data any, opts ...publishOpt) publishResponse {
	s.T().Helper()
	o := publishOpts{}
	for _, fn := range opts {
		fn(&o)
	}
	body := map[string]any{"tenant_id": tenantID, "topic": topic, "eligible_for_retry": o.eligibleForRetry, "data": data}
	if o.eventID != "" {
		body["id"] = o.eventID
	}
	var resp publishResponse
	status := s.base.doJSON(http.MethodPost, s.base.apiV2URL("/publish"), body, &resp)
	s.Require().Equal(http.StatusAccepted, status, "failed to publish event")
	return resp
}

// getDestinationV2 returns a destination through API v2, or the status.
func (s *mcpSuite) getDestinationV2(tenantID, destID string) (int, map[string]any) {
	s.T().Helper()
	status, body := s.base.doRawGet(s.base.apiV2URL("/tenants/" + tenantID + "/destinations/" + destID))
	var dest map[string]any
	if status == http.StatusOK {
		s.Require().NoError(json.Unmarshal(body, &dest))
	}
	return status, dest
}

// attempts lists the attempts of a destination, by attempt number.
func (s *mcpSuite) attempts(version, tenantID, destID string) []mcpAttempt {
	s.T().Helper()
	path := "/attempts?tenant_id=" + tenantID
	if destID != "" {
		path += "&destination_id=" + destID
	}
	u := s.base.apiV2URL(path)
	if version == "v1" {
		u = s.base.apiURL(path)
	}
	var resp struct {
		Models []mcpAttempt `json:"models"`
	}
	status := s.base.doJSON(http.MethodGet, u, nil, &resp)
	s.Require().Equal(http.StatusOK, status)
	sort.SliceStable(resp.Models, func(i, j int) bool { return resp.Models[i].AttemptNumber < resp.Models[j].AttemptNumber })
	return resp.Models
}

// waitForAttempts polls until the destination has n attempts (API v2).
func (s *mcpSuite) waitForAttempts(tenantID, destID string, n int) []mcpAttempt {
	s.T().Helper()
	deadline := time.Now().Add(attemptPollTimeout)
	var got []mcpAttempt
	for time.Now().Before(deadline) {
		if got = s.attempts("v2", tenantID, destID); len(got) >= n {
			return got
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for %d attempts of %s (got %d)", n, destID, len(got))
	return nil
}

// eventIDs returns the event IDs of event requests.
func eventIDs(reqs []mcpRequest) []string {
	ids := make([]string, 0, len(reqs))
	for _, r := range reqs {
		ids = append(ids, r.envelope().EventID)
	}
	sort.Strings(ids)
	return ids
}

func sorted(ids ...string) []string {
	out := slices.Clone(ids)
	sort.Strings(out)
	return out
}

func orderData(total float64, currency string) map[string]any {
	return map[string]any{"orderId": "ord_" + idgen.String(), "total": total, "currency": currency}
}

var subscriptionIDPattern = regexp.MustCompile(`^sub_[0-9a-f]{32}$`)

// =============================================================================
// events/list
// =============================================================================

// The spec's example topic, as events/list serves it. The inferred
// inputSchema is the spec example's, bounded (string and list sizes, a
// non-empty operator object), and numbers also take a list ($in), as the
// spec's text says.
const orderCreatedEventJSON = `{
	"name": "order.created",
	"description": "Fires when a new order is placed.",
	"delivery": ["webhook"],
	"inputSchema": {
		"type": "object",
		"properties": {
			"total": {
				"description": "Order total in major units.",
				"anyOf": [
					{"type": "number"},
					{"type": "array", "items": {"type": "number"}, "minItems": 1, "maxItems": 100},
					{
						"type": "object",
						"properties": {
							"$gt": {"type": "number"}, "$gte": {"type": "number"},
							"$lt": {"type": "number"}, "$lte": {"type": "number"}
						},
						"additionalProperties": false,
						"minProperties": 1
					}
				]
			},
			"currency": {
				"description": "ISO 4217 currency code.",
				"anyOf": [
					{"type": "string", "maxLength": 256},
					{"type": "array", "items": {"type": "string", "maxLength": 256}, "minItems": 1, "maxItems": 100}
				]
			}
		},
		"additionalProperties": false
	},
	"payloadSchema": {
		"type": "object",
		"properties": {
			"orderId": {"type": "string"},
			"total": {"type": "number", "description": "Order total in major units."},
			"currency": {"type": "string", "description": "ISO 4217 currency code."},
			"createdAt": {"type": "string", "format": "date-time"}
		},
		"required": ["orderId", "total", "currency"]
	}
}`

type mcpEventsPage struct {
	Events     []json.RawMessage `json:"events"`
	NextCursor *string           `json:"nextCursor"`
}

func (s *mcpSuite) listEvents(tenantID, query string) (int, json.RawMessage, mcpEventsPage) {
	s.T().Helper()
	status, body := s.base.doRawGet(s.base.apiV2URL("/tenants/" + tenantID + "/mcp/events" + query))
	var page mcpEventsPage
	if status == http.StatusOK {
		s.Require().NoError(json.Unmarshal(body, &page))
	}
	return status, body, page
}

func eventNames(page mcpEventsPage) []string {
	names := []string{}
	for _, e := range page.Events {
		var entry struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(e, &entry)
		names = append(names, entry.Name)
	}
	return names
}

func (s *mcpSuite) TestEventsList() {
	tenant := s.base.createTenant()

	status, raw, page := s.listEvents(tenant.ID, "")
	s.Require().Equal(http.StatusOK, status)
	s.Equal([]string{"order.created", "order.shipped"}, eventNames(page), "MCP-enabled topics only, in TOPICS order")
	s.JSONEq(orderCreatedEventJSON, string(page.Events[0]))
	var top map[string]json.RawMessage
	s.Require().NoError(json.Unmarshal(raw, &top))
	s.NotContains(top, "nextCursor", "the last page has no nextCursor")

	s.Run("topics allowlist", func() {
		for query, want := range map[string][]string{
			"?topics=order.shipped":                      {"order.shipped"},
			"?topics=order.shipped,order.created":        {"order.created", "order.shipped"},
			"?topics=order.shipped&topics=order.created": {"order.created", "order.shipped"},
			"?topics=user.created":                       {},
			"?topics=":                                   {},
		} {
			status, _, page := s.listEvents(tenant.ID, query)
			s.Require().Equal(http.StatusOK, status, query)
			s.Equal(want, eventNames(page), query)
		}
	})

	s.Run("pagination", func() {
		status, _, first := s.listEvents(tenant.ID, "?limit=1")
		s.Require().Equal(http.StatusOK, status)
		s.Equal([]string{"order.created"}, eventNames(first))
		s.Require().NotNil(first.NextCursor)

		status, raw, second := s.listEvents(tenant.ID, "?limit=1&cursor="+url.QueryEscape(*first.NextCursor))
		s.Require().Equal(http.StatusOK, status)
		s.Equal([]string{"order.shipped"}, eventNames(second))
		s.NotContains(string(raw), "nextCursor")
	})

	s.Run("invalid params", func() {
		status, raw, _ := s.listEvents(tenant.ID, "?limit=0")
		s.requireMCPError(status, raw, "invalid_params", -32602, "InvalidParams", map[string]any{"field": "limit", "reason": "invalid"})
		status, raw, _ = s.listEvents(tenant.ID, "?cursor=bogus")
		s.requireMCPError(status, raw, "invalid_params", -32602, "InvalidParams", map[string]any{"field": "cursor", "reason": "invalid"})
	})

	s.Run("API v2 only", func() {
		status, _ := s.base.doRawGet(s.base.apiURL("/tenants/" + tenant.ID + "/mcp/events"))
		s.Equal(http.StatusNotFound, status)
	})
}

// =============================================================================
// Subscribe
// =============================================================================

func (s *mcpSuite) TestSubscribe_VerifiesCallbackAndReturnsResult() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", map[string]any{"total": map[string]any{"$gte": 100}, "currency": "USD"})

	start := time.Now()
	status, raw := s.put(tenant.ID, sub.body())
	s.Require().Equal(http.StatusOK, status, "%s", raw)

	// The result, ready to forward: no deliveryStatus on a new subscription.
	var fields map[string]json.RawMessage
	s.Require().NoError(json.Unmarshal(raw, &fields))
	s.ElementsMatch([]string{"id", "refreshBefore", "cursor", "truncated"}, keys(fields))
	s.Equal("null", string(fields["cursor"]))
	s.Equal("false", string(fields["truncated"]))
	var result mcpSubscribeResult
	s.Require().NoError(json.Unmarshal(raw, &result))
	s.Regexp(subscriptionIDPattern, result.ID)
	s.Equal(sub.id(s.T()), result.ID, "the guide's derivation")
	s.Require().NotNil(result.RefreshBefore)
	refreshBefore, err := time.Parse(time.RFC3339, *result.RefreshBefore)
	s.Require().NoError(err)
	s.Equal(0, refreshBefore.Nanosecond(), "whole seconds")
	s.WithinRange(refreshBefore, start.Add(time.Hour).Add(-time.Second), time.Now().Add(time.Hour), "MCP_TTL_DEFAULT")

	// The challenge came first: signed like a delivery, with its own
	// webhook-id.
	reqs := s.receiver.matching(path, nil)
	s.Require().Len(reqs, 1, "only the challenge")
	challenge := reqs[0]
	s.Require().True(isMCPVerification(challenge))
	s.NotEmpty(challenge.envelope().Challenge)
	s.True(strings.HasPrefix(challenge.Header.Get("webhook-id"), "msg_verification_"), challenge.Header.Get("webhook-id"))
	s.Equal(result.ID, challenge.Header.Get("X-MCP-Subscription-Id"))
	s.NoError(verifyMCPSignature(challenge, sub.Secret))

	// The mcp destination behind it (v2).
	status, dest := s.getDestinationV2(tenant.ID, result.ID)
	s.Require().Equal(http.StatusOK, status)
	s.Equal("mcp", dest["type"])
	s.Equal([]any{"order.created"}, dest["topics"])
	s.NotNil(dest["expires_at"])
	destConfig := dest["config"].(map[string]any)
	s.Equal(sub.URL, destConfig["url"])
	s.Equal(result.ID, destConfig["subscription_id"])
	s.Equal(sub.Principal, destConfig["principal"])
	s.Equal("order.created", destConfig["event"])
	s.Equal(`{"currency":"USD","total":{"$gte":100}}`, destConfig["arguments"], "canonical JSON")
	s.Equal(map[string]any{"data": map[string]any{"total": map[string]any{"$gte": float64(100)}, "currency": "USD"}}, dest["filter"])

	// The operator listing.
	var list []map[string]any
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.subscriptionsURL(tenant.ID)+"?principal="+url.QueryEscape(sub.Principal), nil, &list))
	s.Require().Len(list, 1)
	s.Equal(result.ID, list[0]["id"])
	s.Equal("order.created", list[0]["event"])
	s.Equal(map[string]any{"currency": "USD", "total": map[string]any{"$gte": float64(100)}}, list[0]["arguments"])
	s.Equal(sub.URL, list[0]["url"])
	s.Contains(list[0], "disabled_at")
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// =============================================================================
// Delivery
// =============================================================================

// An MCP delivery is the MCP envelope, signed with Standard Webhooks, with
// exactly the MCP headers, whatever DESTINATIONS_WEBHOOK_MODE says.
func (s *mcpSuite) TestPublish_SignedEnvelope() {
	s.Require().Contains([]string{"", "default"}, s.base.config.Destinations.Webhook.Mode, "deliveries ignore the webhook mode")
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	result := s.subscribe(tenant.ID, sub)

	eventID := "evt_" + idgen.String()
	eventTime := time.Date(2026, 10, 9, 16, 58, 12, 0, time.UTC)
	data := json.RawMessage(`{"orderId":"ord_42","total":180,"currency":"USD"}`)
	before := time.Now().Add(-time.Second)
	s.base.publish(tenant.ID, "order.created", data, withEventID(eventID), withTime(eventTime))

	req := s.receiver.waitFor(s.T(), path, 1, isMCPEvent)[0]
	want := `{"eventId":"` + eventID + `","name":"order.created","timestamp":"2026-10-09T16:58:12Z","data":{"orderId":"ord_42","total":180,"currency":"USD"},"cursor":null}`
	s.Equal(want, string(req.Body), "data passes through byte for byte")

	s.Equal("application/json", req.Header.Get("Content-Type"))
	s.Equal(eventID, req.Header.Get("webhook-id"), "webhook-id is the event ID")
	s.Equal(result.ID, req.Header.Get("X-MCP-Subscription-Id"))
	s.NotEmpty(req.Header.Get("User-Agent"))
	ts, err := strconv.ParseInt(req.Header.Get("webhook-timestamp"), 10, 64)
	s.Require().NoError(err)
	s.WithinRange(time.Unix(ts, 0), before.Truncate(time.Second), time.Now().Add(time.Second), "a fresh timestamp per attempt")
	s.Len(strings.Fields(req.Header.Get("webhook-signature")), 1)
	s.NoError(verifyMCPSignature(req, sub.Secret))

	allowed := map[string]bool{
		"Content-Type": true, "Webhook-Id": true, "Webhook-Timestamp": true, "Webhook-Signature": true,
		"X-Mcp-Subscription-Id": true, "User-Agent": true, "Content-Length": true, "Accept-Encoding": true,
	}
	for name := range req.Header {
		s.True(allowed[name], "unexpected header %s: no custom or x-outpost-* headers", name)
	}

	// The attempt is logged like any other.
	attempts := s.waitForAttempts(tenant.ID, result.ID, 1)
	s.Equal("success", attempts[0].Status)
	s.Equal("mcp", attempts[0].DestinationType)
	s.Equal(eventID, attempts[0].EventID)
}

// arguments become the destination filter: $gte, $in (a list) and equality.
// Filters run at publish, so a non-matching event is never even listed in
// destination_ids.
func (s *mcpSuite) TestFilters() {
	tenant := s.base.createTenant()
	big, bigPath := s.newSubscription("order.created", map[string]any{"total": map[string]any{"$gte": 100}, "currency": []any{"USD", "EUR"}})
	usd, usdPath := s.newSubscription("order.created", map[string]any{"currency": "USD"})
	bigID := s.subscribe(tenant.ID, big).ID
	usdID := s.subscribe(tenant.ID, usd).ID

	small := s.publishV2(tenant.ID, "order.created", orderData(50, "USD"))
	s.Equal([]string{usdID}, small.DestinationIDs, "50 < 100")
	gbp := s.publishV2(tenant.ID, "order.created", orderData(150, "GBP"))
	s.Empty(gbp.DestinationIDs, "GBP is in neither")
	eur := s.publishV2(tenant.ID, "order.created", orderData(150, "EUR"))
	s.Equal([]string{bigID}, eur.DestinationIDs)
	both := s.publishV2(tenant.ID, "order.created", orderData(150, "USD"))
	s.ElementsMatch([]string{bigID, usdID}, both.DestinationIDs)

	s.Equal(sorted(eur.ID, both.ID), eventIDs(s.receiver.waitFor(s.T(), bigPath, 2, isMCPEvent)))
	s.Equal(sorted(small.ID, both.ID), eventIDs(s.receiver.waitFor(s.T(), usdPath, 2, isMCPEvent)))

	// Another topic never reaches them.
	shipped := s.publishV2(tenant.ID, "order.shipped", map[string]any{"orderId": "o_1"})
	s.Empty(shipped.DestinationIDs)

	s.Run("filter override and metadata", func() {
		cad, cadPath := s.newSubscription("order.created", map[string]any{"currency": "USD"})
		cad.Extra = map[string]any{
			"filter":   map[string]any{"data": map[string]any{"currency": "CAD"}},
			"metadata": map[string]any{"agent": "chatgpt"},
		}
		cadID := s.subscribe(tenant.ID, cad).ID
		status, dest := s.getDestinationV2(tenant.ID, cadID)
		s.Require().Equal(http.StatusOK, status)
		s.Equal(map[string]any{"data": map[string]any{"currency": "CAD"}}, dest["filter"], "the override replaces the generated filter")
		s.Equal(map[string]any{"agent": "chatgpt"}, dest["metadata"])

		usdEvent := s.publishV2(tenant.ID, "order.created", orderData(10, "USD"))
		s.NotContains(usdEvent.DestinationIDs, cadID)
		cadEvent := s.publishV2(tenant.ID, "order.created", orderData(10, "CAD"))
		s.Equal([]string{cadID}, cadEvent.DestinationIDs)
		s.Equal([]string{cadEvent.ID}, eventIDs(s.receiver.waitFor(s.T(), cadPath, 1, isMCPEvent)))

		// A refresh without metadata keeps it.
		cad.Extra = map[string]any{"filter": map[string]any{"data": map[string]any{"currency": "CAD"}}}
		s.subscribe(tenant.ID, cad)
		_, dest = s.getDestinationV2(tenant.ID, cadID)
		s.Equal(map[string]any{"agent": "chatgpt"}, dest["metadata"])
	})
}

// ttlMs is clamped to [MCP_TTL_MIN, MCP_TTL_MAX]; null asks for no expiry,
// which MCP_ALLOW_NO_EXPIRY=false turns into MCP_TTL_MAX.
func (s *mcpSuite) TestTTLGrants() {
	tenant := s.base.createTenant()
	for _, tc := range []struct {
		name  string
		ttlMs any
		want  time.Duration
	}{
		{"absent", nil, time.Hour},
		{"below the minimum", 1, time.Second},
		{"above the maximum", 1e12, 24 * time.Hour},
		{"null", json.RawMessage("null"), 24 * time.Hour},
		{"fractional ms are dropped", 90500.7, 90500 * time.Millisecond},
	} {
		s.Run(tc.name, func() {
			sub, _ := s.newSubscription("order.created", nil)
			sub.TTLMs = tc.ttlMs
			start := time.Now()
			result := s.subscribe(tenant.ID, sub)
			s.Require().NotNil(result.RefreshBefore)
			refreshBefore, err := time.Parse(time.RFC3339, *result.RefreshBefore)
			s.Require().NoError(err)
			s.WithinRange(refreshBefore, start.Add(tc.want).Add(-time.Second), time.Now().Add(tc.want))
		})
	}

	sub, _ := s.newSubscription("order.created", nil)
	sub.TTLMs = "1h"
	status, raw := s.put(tenant.ID, sub.body())
	s.requireMCPError(status, raw, "invalid_params", -32602, "InvalidParams", map[string]any{"field": "ttlMs", "reason": "invalid_type"})
}

// MCP_RETRY_SCHEDULE (1,1,1 here) retries failed deliveries: 500, 500, 200
// is three attempts, and none after the success.
func (s *mcpSuite) TestRetries_UntilSuccess() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	s.receiver.script(path, mcpScript{statuses: []int{500, 500}})
	id := s.subscribe(tenant.ID, sub).ID

	event := s.base.publish(tenant.ID, "order.created", orderData(10, "USD"), withRetry())
	reqs := s.receiver.waitFor(s.T(), path, 3, isMCPEvent)
	for _, r := range reqs {
		s.Equal(event.ID, r.Header.Get("webhook-id"), "eventId and webhook-id are unchanged across retries")
		s.NoError(verifyMCPSignature(r, sub.Secret))
	}

	attempts := s.waitForAttempts(tenant.ID, id, 3)
	s.Require().Len(attempts, 3)
	for i, want := range []struct{ status, code string }{{"failed", "500"}, {"failed", "500"}, {"success", ""}} {
		s.Equal(i+1, attempts[i].AttemptNumber)
		s.Equal(want.status, attempts[i].Status)
		if want.code != "" {
			s.Equal(want.code, attempts[i].Code)
		}
	}

	time.Sleep(1500 * time.Millisecond)
	s.Len(s.receiver.matching(path, isMCPEvent), 3, "no retry after a success")
}

// An event that keeps failing gets 1 + len(MCP_RETRY_SCHEDULE) attempts, not
// the global retry limit's, and then alert.attempt.exhausted_retries.
func (s *mcpSuite) TestRetries_ExhaustMCPSchedule() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	s.receiver.script(path, mcpScript{fallback: http.StatusInternalServerError})
	id := s.subscribe(tenant.ID, sub).ID

	s.base.publish(tenant.ID, "order.created", orderData(10, "USD"), withRetry())
	attempts := s.waitForAttempts(tenant.ID, id, 4)
	for i, a := range attempts {
		s.Equal(i+1, a.AttemptNumber)
		s.Equal("failed", a.Status)
	}

	var exhausted struct {
		Attempt struct {
			AttemptNumber int `json:"attempt_number"`
		} `json:"attempt"`
		Destination struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"destination"`
	}
	events := s.base.waitForOpEvents("alert.attempt.exhausted_retries", 1)
	s.Require().NoError(json.Unmarshal(events[0].Event.Data, &exhausted))
	s.Equal(id, exhausted.Destination.ID)
	s.Equal("mcp", exhausted.Destination.Type)
	s.Equal(4, exhausted.Attempt.AttemptNumber)

	time.Sleep(2 * time.Second)
	s.Len(s.receiver.matching(path, isMCPEvent), 4, "the global limit (%d retries) doesn't apply", globalRetryMaxLimit)
	status, dest := s.getDestinationV2(tenant.ID, id)
	s.Require().Equal(http.StatusOK, status)
	s.Nil(dest["disabled_at"], "4 failures stay under the auto-disable count")
}

// 410 and 413 fail the attempt and are never retried.
func (s *mcpSuite) TestNonRetryableStatuses() {
	tenant := s.base.createTenant()
	type target struct {
		status int
		path   string
		id     string
	}
	var targets []*target
	for _, status := range []int{http.StatusGone, http.StatusRequestEntityTooLarge} {
		sub, path := s.newSubscription("order.created", nil)
		s.receiver.script(path, mcpScript{fallback: status})
		targets = append(targets, &target{status: status, path: path, id: s.subscribe(tenant.ID, sub).ID})
	}

	s.base.publish(tenant.ID, "order.created", orderData(10, "USD"), withRetry())
	for _, tg := range targets {
		s.receiver.waitFor(s.T(), tg.path, 1, isMCPEvent)
		attempts := s.waitForAttempts(tenant.ID, tg.id, 1)
		s.Equal("failed", attempts[0].Status)
		s.Equal(strconv.Itoa(tg.status), attempts[0].Code)
	}

	// Past the whole retry schedule.
	time.Sleep(2500 * time.Millisecond)
	for _, tg := range targets {
		s.Len(s.receiver.matching(tg.path, isMCPEvent), 1, "%d is not retried", tg.status)
		s.Len(s.attempts("v2", tenant.ID, tg.id), 1, "%d is not retried", tg.status)
	}

	// A manual retry still runs, like for any destination.
	gone := targets[0]
	s.receiver.script(gone.path, mcpScript{})
	event := s.attempts("v2", tenant.ID, gone.id)[0].EventID
	status := s.base.doJSON(http.MethodPost, s.base.apiV2URL("/retry"), map[string]any{"event_id": event, "destination_id": gone.id}, nil)
	s.Require().Equal(http.StatusAccepted, status)
	s.receiver.waitFor(s.T(), gone.path, 2, isMCPEvent)
	attempts := s.waitForAttempts(tenant.ID, gone.id, 2)
	s.Equal("success", attempts[1].Status)
	s.True(attempts[1].Manual)
}

// An envelope over 256 KiB is never sent: one failed, non-retryable attempt
// with code payload_too_large.
func (s *mcpSuite) TestPayloadTooLarge() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.shipped", nil)
	id := s.subscribe(tenant.ID, sub).ID

	s.base.publish(tenant.ID, "order.shipped", map[string]any{
		"orderId": "o_big",
		"note":    strings.Repeat("x", 260<<10),
	}, withRetry())

	attempts := s.waitForAttempts(tenant.ID, id, 1)
	s.Equal("failed", attempts[0].Status)
	s.Equal("payload_too_large", attempts[0].Code)

	time.Sleep(1500 * time.Millisecond)
	s.Empty(s.receiver.matching(path, isMCPEvent), "nothing is sent")
	s.Len(s.attempts("v2", tenant.ID, id), 1, "not retried")
}

// =============================================================================
// Refresh
// =============================================================================

// A refresh is the same subscribe call: same ID, a later expiry, no new
// challenge (cached), and deliveryStatus from the last attempts.
func (s *mcpSuite) TestRefresh_IdempotentWithDeliveryStatus() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	first := s.subscribe(tenant.ID, sub)
	s.Nil(first.DeliveryStatus)

	s.base.publish(tenant.ID, "order.created", orderData(10, "USD"))
	s.receiver.waitFor(s.T(), path, 1, isMCPEvent)
	s.waitForAttempts(tenant.ID, first.ID, 1)

	// The status record is written right after the attempt is logged.
	var refreshed mcpSubscribeResult
	deadline := time.Now().Add(attemptPollTimeout)
	for {
		refreshed = s.subscribe(tenant.ID, sub)
		s.Require().NotNil(refreshed.DeliveryStatus, "refreshes report deliveryStatus")
		if refreshed.DeliveryStatus.LastDeliveryAt != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Equal(first.ID, refreshed.ID)
	s.True(refreshed.DeliveryStatus.Active)
	s.Require().NotNil(refreshed.DeliveryStatus.LastDeliveryAt)
	lastDelivery, err := time.Parse(time.RFC3339, *refreshed.DeliveryStatus.LastDeliveryAt)
	s.Require().NoError(err)
	s.WithinDuration(time.Now(), lastDelivery, time.Minute)
	s.Nil(refreshed.DeliveryStatus.LastError)
	s.GreaterOrEqual(*refreshed.RefreshBefore, *first.RefreshBefore)

	s.Len(s.receiver.matching(path, isMCPVerification), 1, "verification is cached per (principal, url)")
	var list []map[string]any
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.subscriptionsURL(tenant.ID), nil, &list))
	s.Len(list, 1, "one subscription")
}

// A refresh with a new secret keeps the old one as previous_secret: until
// MCP_SECRET_ROTATION_GRACE, deliveries carry both signatures.
func (s *mcpSuite) TestRefresh_SecretRotationDualSigns() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	oldSecret := sub.Secret
	id := s.subscribe(tenant.ID, sub).ID

	sub.Secret = newStandardWebhooksSecret()
	s.Equal(id, s.subscribe(tenant.ID, sub).ID, "the secret isn't part of the ID")

	s.base.publish(tenant.ID, "order.created", orderData(10, "USD"))
	req := s.receiver.waitFor(s.T(), path, 1, isMCPEvent)[0]
	s.Len(strings.Fields(req.Header.Get("webhook-signature")), 2, "two v1 signatures")
	s.NoError(verifyMCPSignature(req, sub.Secret), "the new secret verifies")
	s.NoError(verifyMCPSignature(req, oldSecret), "the old secret verifies during the grace period")

	status, dest := s.getDestinationV2(tenant.ID, id)
	s.Require().Equal(http.StatusOK, status)
	credentials := dest["credentials"].(map[string]any)
	s.Contains(credentials, "previous_secret")
	s.Contains(credentials, "previous_secret_invalid_at")
}

// =============================================================================
// Unsubscribe, expiry, revocation
// =============================================================================

func (s *mcpSuite) TestUnsubscribe() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	proof, proofPath := s.newSubscription("order.created", nil)
	id := s.subscribe(tenant.ID, sub).ID
	proofID := s.subscribe(tenant.ID, proof).ID

	first := s.base.publish(tenant.ID, "order.created", orderData(10, "USD"))
	s.receiver.waitFor(s.T(), path, 1, isMCPEvent)

	s.Equal("{}", s.unsubscribe(tenant.ID, sub.unsubscribeBody()))
	status, _ := s.getDestinationV2(tenant.ID, id)
	s.Equal(http.StatusNotFound, status)

	second := s.publishV2(tenant.ID, "order.created", orderData(10, "USD"))
	s.Equal([]string{proofID}, second.DestinationIDs)
	s.Equal(sorted(first.ID, second.ID), eventIDs(s.receiver.waitFor(s.T(), proofPath, 2, isMCPEvent)))
	s.Equal([]string{first.ID}, eventIDs(s.receiver.matching(path, isMCPEvent)), "no delivery after unsubscribe")

	s.Run("always {}", func() {
		s.Equal("{}", s.unsubscribe(tenant.ID, sub.unsubscribeBody()), "already gone")
		s.Equal("{}", s.unsubscribe("missing_"+idgen.String(), sub.unsubscribeBody()), "unknown tenant")
		unknown := sub
		unknown.Name = "no.such.topic"
		s.Equal("{}", s.unsubscribe(tenant.ID, unknown.unsubscribeBody()), "unknown topic")
	})
}

// Unsubscribing drops the subscription's pending retries.
func (s *mcpSuite) TestUnsubscribe_DropsPendingRetries() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	s.receiver.script(path, mcpScript{fallback: http.StatusInternalServerError})
	id := s.subscribe(tenant.ID, sub).ID

	s.base.publish(tenant.ID, "order.created", orderData(10, "USD"), withRetry())
	s.receiver.waitFor(s.T(), path, 1, isMCPEvent)
	s.Equal("{}", s.unsubscribe(tenant.ID, sub.unsubscribeBody()))
	unsubscribedAt := time.Now()

	time.Sleep(2500 * time.Millisecond)
	for _, r := range s.receiver.matching(path, isMCPEvent) {
		s.True(r.Received.Before(unsubscribedAt), "no attempt after unsubscribe")
	}
	s.LessOrEqual(len(s.attempts("v2", tenant.ID, id)), 2, "the scheduled retries were dropped")
}

// A subscription stops receiving events at its expires_at, retries
// included. Subscribing again replaces it with a new one and reports
// mcp.subscription.expired.
func (s *mcpSuite) TestExpiry() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	sub.TTLMs = 2000
	// Every attempt fails, so the first event keeps being retried.
	s.receiver.script(path, mcpScript{fallback: http.StatusInternalServerError})
	proof, proofPath := s.newSubscription("order.created", nil)
	start := time.Now()
	result := s.subscribe(tenant.ID, sub)
	proofID := s.subscribe(tenant.ID, proof).ID

	s.Require().NotNil(result.RefreshBefore)
	refreshBefore, err := time.Parse(time.RFC3339, *result.RefreshBefore)
	s.Require().NoError(err)
	s.WithinRange(refreshBefore, start.Add(time.Second).Truncate(time.Second), time.Now().Add(2*time.Second), "ttlMs is granted")

	first := s.publishV2(tenant.ID, "order.created", orderData(10, "USD"), withRetry())
	s.Contains(first.DestinationIDs, result.ID)
	s.receiver.waitFor(s.T(), path, 1, isMCPEvent)

	status, dest := s.getDestinationV2(tenant.ID, result.ID)
	s.Require().Equal(http.StatusOK, status)
	expiresAt, err := time.Parse(time.RFC3339Nano, dest["expires_at"].(string))
	s.Require().NoError(err)
	time.Sleep(time.Until(expiresAt) + 200*time.Millisecond)

	second := s.publishV2(tenant.ID, "order.created", orderData(10, "USD"))
	s.Equal([]string{proofID}, second.DestinationIDs, "an expired subscription matches nothing")
	s.receiver.waitFor(s.T(), proofPath, 2, isMCPEvent)

	// Past the retry schedule: no attempt after expires_at, retries
	// included.
	time.Sleep(2500 * time.Millisecond)
	reqs := s.receiver.matching(path, isMCPEvent)
	s.NotEmpty(reqs)
	for _, r := range reqs {
		s.Equal(first.ID, r.envelope().EventID)
		s.True(r.Received.Before(expiresAt.Add(100*time.Millisecond)), "attempt at %s after expires_at %s", r.Received, expiresAt)
	}

	// A manual retry is refused.
	var retryResp map[string]any
	status = s.base.doJSON(http.MethodPost, s.base.apiV2URL("/retry"), map[string]any{"event_id": first.ID, "destination_id": result.ID}, &retryResp)
	s.Equal(http.StatusBadRequest, status)
	s.Equal(map[string]any{"error": "destination_expired"}, retryResp["data"])

	again := s.subscribe(tenant.ID, sub)
	s.Equal(result.ID, again.ID)
	s.Nil(again.DeliveryStatus, "a new subscription, not a refresh of the expired one")

	events := s.base.waitForOpEvents("mcp.subscription.expired", 1)
	var data struct {
		TenantID       string    `json:"tenant_id"`
		SubscriptionID string    `json:"subscription_id"`
		Principal      string    `json:"principal"`
		Topic          string    `json:"topic"`
		URL            string    `json:"url"`
		ExpiresAt      time.Time `json:"expires_at"`
	}
	s.Require().NoError(json.Unmarshal(events[0].Event.Data, &data))
	s.Equal(tenant.ID, events[0].Event.TenantID)
	s.Equal(tenant.ID, data.TenantID)
	s.Equal(result.ID, data.SubscriptionID)
	s.Equal(sub.Principal, data.Principal)
	s.Equal("order.created", data.Topic)
	s.Equal(sub.URL, data.URL)
	s.True(expiresAt.Equal(data.ExpiresAt), "%s != %s", expiresAt, data.ExpiresAt)
}

// Revoking one subscription (here with the tenant's JWT, as the portal's
// disconnect does) deletes it and sends it a signed terminated envelope,
// without waiting for the callback.
func (s *mcpSuite) TestRevokeOne() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	const callbackDelay = 2 * time.Second
	s.receiver.script(path, mcpScript{delay: callbackDelay})
	result := s.subscribe(tenant.ID, sub)
	revokeURL := s.subscriptionsURL(tenant.ID) + "/" + result.ID

	start := time.Now()
	var resp map[string]any
	status := s.base.doJSONWithAuth(http.MethodDelete, revokeURL, s.base.tenantAuth(tenant.ID), nil, &resp)
	s.Require().Equal(http.StatusOK, status)
	s.Equal(map[string]any{"success": true}, resp)
	s.Less(time.Since(start), callbackDelay, "the terminated envelope is sent in the background")

	terminated := s.receiver.waitFor(s.T(), path, 1, isMCPTerminated)[0]
	s.JSONEq(`{"type":"terminated","error":{"code":-32012,"message":"Forbidden","data":{"reason":"access_revoked"}}}`, string(terminated.Body))
	s.True(strings.HasPrefix(terminated.Header.Get("webhook-id"), "msg_terminated_"), terminated.Header.Get("webhook-id"))
	s.Equal(result.ID, terminated.Header.Get("X-MCP-Subscription-Id"))
	s.NoError(verifyMCPSignature(terminated, sub.Secret))

	status, _ = s.getDestinationV2(tenant.ID, result.ID)
	s.Equal(http.StatusNotFound, status)
	s.Equal(http.StatusNotFound, s.base.doJSON(http.MethodDelete, revokeURL, nil, nil), "already revoked")
	s.Empty(s.publishV2(tenant.ID, "order.created", orderData(10, "USD")).DestinationIDs)
	s.Len(s.receiver.matching(path, isMCPTerminated), 1, "terminated once")
}

// Revoking a principal ends all its subscriptions, each with a terminated
// envelope, and leaves other principals alone.
func (s *mcpSuite) TestRevokeByPrincipal() {
	tenant := s.base.createTenant()
	created, createdPath := s.newSubscription("order.created", nil)
	shipped, shippedPath := s.newSubscription("order.shipped", nil)
	shipped.Principal = created.Principal
	other, otherPath := s.newSubscription("order.created", nil)
	s.subscribe(tenant.ID, created)
	s.subscribe(tenant.ID, shipped)
	otherID := s.subscribe(tenant.ID, other).ID

	var resp map[string]any
	status := s.base.doJSON(http.MethodDelete, s.subscriptionsURL(tenant.ID)+"?principal="+url.QueryEscape(created.Principal), nil, &resp)
	s.Require().Equal(http.StatusOK, status)
	s.Equal(map[string]any{"success": true, "deleted": float64(2)}, resp)

	for _, p := range []string{createdPath, shippedPath} {
		terminated := s.receiver.waitFor(s.T(), p, 1, isMCPTerminated)[0]
		s.Equal("access_revoked", terminated.envelope().Error.Data["reason"])
	}
	var list []map[string]any
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.subscriptionsURL(tenant.ID), nil, &list))
	s.Require().Len(list, 1)
	s.Equal(otherID, list[0]["id"])
	s.Empty(s.receiver.matching(otherPath, isMCPTerminated))

	// The revocation only fences calls that started before it. Fences and
	// tombstones are compared in milliseconds, a tie counting as revoked,
	// so start in a later millisecond.
	time.Sleep(2 * time.Millisecond)
	s.subscribe(tenant.ID, created)

	// The principal is required.
	status, raw := s.base.doRawWithAuthBody(http.MethodDelete, s.subscriptionsURL(tenant.ID)+"?principal=")
	s.requireMCPError(status, raw, "invalid_params", -32602, "InvalidParams", map[string]any{"field": "principal", "reason": "required"})
}

// =============================================================================
// Auto-disable, parked retries, re-enable
// =============================================================================

// Consecutive failures auto-disable a subscription (5 here). Its automatic
// retries then park instead of being dropped; a refresh re-enables it,
// reports it was inactive, and resumes the parked retries.
func (s *mcpSuite) TestAutoDisable_RefreshReenablesAndResumesParkedRetries() {
	tenant := s.base.createTenant()
	sub, path := s.newSubscription("order.created", nil)
	s.receiver.script(path, mcpScript{fallback: http.StatusInternalServerError})
	id := s.subscribe(tenant.ID, sub).ID

	var published []string
	for range 3 {
		published = append(published, s.base.publish(tenant.ID, "order.created", orderData(10, "USD"), withRetry()).ID)
	}

	deadline := time.Now().Add(mockServerPollTimeout)
	for {
		status, dest := s.getDestinationV2(tenant.ID, id)
		s.Require().Equal(http.StatusOK, status)
		if dest["disabled_at"] != nil {
			break
		}
		s.Require().True(time.Now().Before(deadline), "timed out waiting for the auto-disable")
		time.Sleep(100 * time.Millisecond)
	}

	// Every retry has fired 3.5s after the last publish: each was either
	// delivered (before the disable) or parked.
	redisClient, err := redis.New(context.Background(), s.base.config.Redis.ToConfig())
	s.Require().NoError(err)
	parkedKey := fmt.Sprintf("tenant:{%s}:parked_retries:%s", tenant.ID, id)
	if s.deploymentID != "" {
		parkedKey = s.deploymentID + ":" + parkedKey
	}
	time.Sleep(3500 * time.Millisecond)
	members, err := redisClient.SMembers(context.Background(), parkedKey).Result()
	s.Require().NoError(err)
	s.Require().NotEmpty(members, "automatic retries of a disabled subscription park")
	parked := map[string]bool{}
	for _, m := range members {
		var task deliverymq.RetryTask
		s.Require().NoError(task.FromString(m))
		s.Contains(published, task.EventID)
		parked[task.EventID] = true
	}

	s.receiver.script(path, mcpScript{})
	refreshed := s.subscribe(tenant.ID, sub)
	s.Require().NotNil(refreshed.DeliveryStatus)
	s.False(refreshed.DeliveryStatus.Active, "it was disabled")
	s.Require().NotNil(refreshed.DeliveryStatus.LastError)
	s.Equal("http_5xx", *refreshed.DeliveryStatus.LastError)
	s.Nil(refreshed.DeliveryStatus.LastDeliveryAt)

	status, dest := s.getDestinationV2(tenant.ID, id)
	s.Require().Equal(http.StatusOK, status)
	s.Nil(dest["disabled_at"], "re-enabled")

	// The parked retries are delivered, now successfully.
	deadline = time.Now().Add(mockServerPollTimeout)
	for {
		attempts := s.attempts("v2", tenant.ID, id)
		delivered := map[string]bool{}
		for _, a := range attempts {
			if a.Status == "success" {
				s.Greater(a.AttemptNumber, 1, "a resumed retry, not a new event")
				s.False(a.Manual)
				delivered[a.EventID] = true
			}
		}
		if len(delivered) >= len(parked) {
			for eventID := range parked {
				s.True(delivered[eventID], "parked retry of %s resumed", eventID)
			}
			break
		}
		s.Require().True(time.Now().Before(deadline), "timed out waiting for resumed retries (%d of %d)", len(delivered), len(parked))
		time.Sleep(100 * time.Millisecond)
	}
	n, err := redisClient.Exists(context.Background(), parkedKey).Result()
	s.Require().NoError(err)
	s.Zero(n, "nothing left parked")

	// A new event is delivered too.
	event := s.base.publish(tenant.ID, "order.created", orderData(10, "USD"))
	s.receiver.waitFor(s.T(), path, 1, func(r mcpRequest) bool { return r.envelope().EventID == event.ID })
}

// =============================================================================
// API v1 and v2
// =============================================================================

// v1 hides MCP subscriptions everywhere; v2 shows them.
func (s *mcpSuite) TestVisibility_V1V2() {
	tenant := s.base.createTenant()
	webhook := s.base.createWebhookDestination(tenant.ID, "order.created", withSecret(testSecret))
	sub, path := s.newSubscription("order.created", nil)
	id := s.subscribe(tenant.ID, sub).ID

	s.Run("publish response", func() {
		v1 := s.base.publish(tenant.ID, "order.created", orderData(10, "USD"))
		s.Equal([]string{webhook.ID}, v1.DestinationIDs, "v1 leaves MCP subscriptions out")
		v2 := s.publishV2(tenant.ID, "order.created", orderData(10, "USD"))
		s.ElementsMatch([]string{webhook.ID, id}, v2.DestinationIDs)
		s.receiver.waitFor(s.T(), path, 2, isMCPEvent)
		s.base.waitForNewMockServerEvents(webhook.mockID, 2)
	})

	s.Run("destinations", func() {
		var v1 []map[string]any
		s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiURL("/tenants/"+tenant.ID+"/destinations"), nil, &v1))
		s.Require().Len(v1, 1)
		s.Equal(webhook.ID, v1[0]["id"])
		var v2 []map[string]any
		s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiV2URL("/tenants/"+tenant.ID+"/destinations"), nil, &v2))
		s.Len(v2, 2)

		status, _ := s.base.doRawGet(s.base.apiURL("/tenants/" + tenant.ID + "/destinations/" + id))
		s.Equal(http.StatusNotFound, status, "v1 retrieve")
		status, _ = s.getDestinationV2(tenant.ID, id)
		s.Equal(http.StatusOK, status)
	})

	s.Run("attempts", func() {
		v2 := s.waitForAttempts(tenant.ID, "", 4)
		types := map[string]int{}
		for _, a := range v2 {
			types[a.DestinationType]++
		}
		s.Equal(map[string]int{"webhook": 2, "mcp": 2}, types)
		v1 := s.attempts("v1", tenant.ID, "")
		s.Len(v1, 2)
		for _, a := range v1 {
			s.Equal("webhook", a.DestinationType)
		}
		status, _ := s.base.doRawGet(s.base.apiURL("/tenants/" + tenant.ID + "/destinations/" + id + "/attempts"))
		s.Equal(http.StatusNotFound, status)
		mcpAttempts := s.attempts("v2", tenant.ID, id)
		s.Require().NotEmpty(mcpAttempts)
		status, _ = s.base.doRawGet(s.base.apiURL("/attempts/" + mcpAttempts[0].ID))
		s.Equal(http.StatusNotFound, status, "v1 attempt retrieve")
	})

	s.Run("destination types", func() {
		status, v1 := s.base.doRawGet(s.base.apiURL("/destination-types"))
		s.Require().Equal(http.StatusOK, status)
		s.NotContains(string(v1), `"create_mode"`)
		s.NotContains(string(v1), `"type":"mcp"`)
		status, _ = s.base.doRawGet(s.base.apiURL("/destination-types/mcp"))
		s.Equal(http.StatusNotFound, status)

		var v2 []map[string]any
		s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiV2URL("/destination-types"), nil, &v2))
		byType := map[string]map[string]any{}
		for _, t := range v2 {
			byType[t["type"].(string)] = t
		}
		s.Require().Contains(byType, "mcp")
		s.Equal("form", byType["webhook"]["create_mode"])
		s.Equal("external", byType["mcp"]["create_mode"])
		instructions := byType["mcp"]["instructions"].(string)
		s.NotContains(instructions, "{{", "the instructions template is rendered")
		s.Contains(instructions, "`https://mcp.example.com/mcp`")
		s.Contains(instructions, "- `order.created`\n- `order.shipped`")
	})

	s.Run("generic writes", func() {
		mcpBody := map[string]any{"type": "mcp", "topics": []string{"order.created"}, "config": map[string]any{"url": sub.URL}}
		for _, u := range []string{s.base.apiURL("/tenants/" + tenant.ID + "/destinations"), s.base.apiV2URL("/tenants/" + tenant.ID + "/destinations")} {
			s.Equal(http.StatusBadRequest, s.base.doJSON(http.MethodPost, u, mcpBody, nil), u)
		}
		destURL := s.base.apiV2URL("/tenants/" + tenant.ID + "/destinations/" + id)
		s.Equal(http.StatusBadRequest, s.base.doJSON(http.MethodPatch, destURL, map[string]any{"topics": []string{"*"}}, nil))
		s.Equal(http.StatusBadRequest, s.base.doJSON(http.MethodPut, destURL+"/disable", nil, nil))
		s.Equal(http.StatusBadRequest, s.base.doJSON(http.MethodDelete, destURL, nil, nil))
		s.Equal(http.StatusNotFound, s.base.doJSON(http.MethodDelete, s.base.apiURL("/tenants/"+tenant.ID+"/destinations/"+id), nil, nil))
	})

	s.Run("tenant counts include MCP subscriptions", func() {
		var t map[string]any
		s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiURL("/tenants/"+tenant.ID), nil, &t))
		s.Equal(float64(2), t["destinations_count"])
	})
}

// =============================================================================
// Errors
// =============================================================================

func (s *mcpSuite) TestSubscribeErrors() {
	tenant := s.base.createTenant()
	base, path := s.newSubscription("order.created", nil)

	with := func(mutate func(*mcpSubscription)) map[string]any {
		sub := base
		mutate(&sub)
		return sub.body()
	}
	invalid := func(field, reason string) map[string]any {
		return map[string]any{"field": field, "reason": reason}
	}

	for _, tc := range []struct {
		name    string
		tenant  string
		body    any
		kind    string
		code    int
		message string
		data    map[string]any
	}{
		{"unknown topic", tenant.ID, with(func(m *mcpSubscription) { m.Name = "no.such.topic" }), "not_found", -32011, "NotFound", map[string]any{"kind": "event"}},
		{"topic without MCP", tenant.ID, with(func(m *mcpSubscription) { m.Name = "user.created" }), "not_found", -32011, "NotFound", map[string]any{"kind": "event"}},
		{"topic outside allowed_topics", tenant.ID, with(func(m *mcpSubscription) { m.Extra = map[string]any{"allowed_topics": []string{"order.shipped"}} }), "not_found", -32011, "NotFound", map[string]any{"kind": "event"}},
		{"unknown tenant", "missing_" + idgen.String(), base.body(), "not_found", -32011, "NotFound", map[string]any{"kind": "tenant"}},
		{"mode push", tenant.ID, with(func(m *mcpSubscription) { m.Mode = "push" }), "unsupported", -32014, "Unsupported", map[string]any{"feature": "deliveryMode", "value": "push"}},
		{"bad secret", tenant.ID, with(func(m *mcpSubscription) { m.Secret = "whsec_c2hvcnQ=" }), "invalid_params", -32602, "InvalidParams", invalid("delivery.secret", "invalid_secret")},
		{"secret without prefix", tenant.ID, with(func(m *mcpSubscription) { m.Secret = "not-a-secret" }), "invalid_params", -32602, "InvalidParams", invalid("delivery.secret", "invalid_secret")},
		{"credentials in URL", tenant.ID, with(func(m *mcpSubscription) { m.URL = strings.Replace(m.URL, "http://", "http://user:pw@", 1) }), "invalid_params", -32602, "InvalidParams", invalid("delivery.url", "invalid_url")},
		{"private address over https", tenant.ID, with(func(m *mcpSubscription) { m.URL = "https://10.1.2.3/hook" }), "invalid_params", -32602, "InvalidParams", invalid("delivery.url", "address_not_allowed")},
		{"private address over http", tenant.ID, with(func(m *mcpSubscription) { m.URL = "http://10.1.2.3/hook" }), "invalid_params", -32602, "InvalidParams", invalid("delivery.url", "https_required")},
		{"argument of the wrong type", tenant.ID, with(func(m *mcpSubscription) { m.Arguments = map[string]any{"total": "lots"} }), "invalid_params", -32602, "InvalidParams", nil},
		{"hidden argument", tenant.ID, with(func(m *mcpSubscription) { m.Arguments = map[string]any{"orderId": "ord_1"} }), "invalid_params", -32602, "InvalidParams", nil},
		{"missing principal", tenant.ID, with(func(m *mcpSubscription) { m.Principal = "" }), "invalid_params", -32602, "InvalidParams", invalid("principal", "required")},
	} {
		s.Run(tc.name, func() {
			status, raw := s.put(tc.tenant, tc.body)
			if tc.data == nil {
				// Schema failures list their errors; check the shape only.
				var body mcpErrorBody
				s.Require().Equal(http.StatusUnprocessableEntity, status, "%s", raw)
				s.Require().NoError(json.Unmarshal(raw, &body))
				s.Equal(tc.kind, body.MCPError.Kind)
				s.Equal(tc.code, body.MCPError.Code)
				s.Equal("arguments", body.MCPError.Data["field"])
				s.Equal("invalid", body.MCPError.Data["reason"])
				s.NotContains(string(raw), "lots", "argument values never appear in errors")
				return
			}
			s.requireMCPError(status, raw, tc.kind, tc.code, tc.message, tc.data)
		})
	}
	s.Empty(s.receiver.matching(path, nil), "no challenge is sent for an invalid request")

	s.Run("callback failures", func() {
		for _, tc := range []struct {
			script mcpScript
			reason string
		}{
			{mcpScript{wrongChallenge: true}, "challenge_failed"},
			{mcpScript{challengeStatus: http.StatusInternalServerError}, "http_5xx"},
			{mcpScript{challengeStatus: http.StatusNotFound}, "http_4xx"},
		} {
			sub, p := s.newSubscription("order.created", nil)
			s.receiver.script(p, tc.script)
			status, raw := s.put(tenant.ID, sub.body())
			s.requireMCPError(status, raw, "callback_endpoint_error", -32015, "CallbackEndpointError", map[string]any{"reason": tc.reason})
			s.Len(s.receiver.matching(p, isMCPVerification), 1)
			status, _ = s.getDestinationV2(tenant.ID, sub.id(s.T()))
			s.Equal(http.StatusNotFound, status, "nothing is created")
		}

		// A closed port.
		sub, _ := s.newSubscription("order.created", nil)
		sub.URL = "http://127.0.0.1:1/hook"
		status, raw := s.put(tenant.ID, sub.body())
		s.requireMCPError(status, raw, "callback_endpoint_error", -32015, "CallbackEndpointError", map[string]any{"reason": "connection_refused"})
	})

	s.Run("API v2 only", func() {
		status := s.base.doJSON(http.MethodPut, s.base.apiURL("/tenants/"+tenant.ID+"/mcp/subscriptions"), base.body(), nil)
		s.Equal(http.StatusNotFound, status)
	})
}

// MAX_MCP_SUBSCRIPTIONS_PER_PRINCIPAL (3 here) caps a principal's
// subscriptions in a tenant; refreshes don't count.
func (s *mcpSuite) TestPrincipalLimit() {
	tenant := s.base.createTenant()
	path, callbackURL := s.receiver.newPath()
	principal := newMCPPrincipal()
	secret := newStandardWebhooksSecret()
	sub := func(currency string) mcpSubscription {
		return mcpSubscription{Principal: principal, Name: "order.created", Arguments: map[string]any{"currency": currency}, URL: callbackURL, Secret: secret}
	}
	for _, currency := range []string{"USD", "EUR", "GBP"} {
		s.subscribe(tenant.ID, sub(currency))
	}

	status, raw := s.put(tenant.ID, sub("JPY").body())
	s.requireMCPError(status, raw, "resource_exhausted", -32013, "ResourceExhausted",
		map[string]any{"limit": "principal_subscriptions", "max": float64(mcpPrincipalLimit)})

	refreshed := s.subscribe(tenant.ID, sub("USD"))
	s.NotNil(refreshed.DeliveryStatus, "a refresh is not a new subscription")

	// Another principal has its own limit.
	other := sub("JPY")
	other.Principal = newMCPPrincipal()
	s.subscribe(tenant.ID, other)

	s.Equal("{}", s.unsubscribe(tenant.ID, sub("EUR").unsubscribeBody()))
	s.subscribe(tenant.ID, sub("JPY"))
	s.Len(s.receiver.matching(path, isMCPVerification), 2, "one challenge per (principal, url)")
}

// =============================================================================
// Standalone: no allowlist
// =============================================================================

// Without MCP_CALLBACK_ALLOWLIST, loopback callbacks are refused before any
// request is sent, with the same answer whatever the reason.
func TestE2E_MCP_NoAllowlist(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()

	receiver := newMCPReceiver()
	defer receiver.Close()

	cfg := configs.Basic(t, configs.BasicOpts{LogStorage: configs.LogStorageTypePostgres})
	withMCPTopics(&cfg, orderCreatedSchema)
	a := startStandaloneApp(t, cfg)
	tenantID := a.createTenant()

	path, loopbackURL := receiver.newPath()
	httpsURL := strings.Replace(loopbackURL, "http://", "https://", 1)
	for _, tc := range []struct {
		url    string
		reason string
	}{
		{loopbackURL, "https_required"},
		{strings.Replace(loopbackURL, "127.0.0.1", "localhost", 1), "https_required"},
		{httpsURL, "address_not_allowed"},
		{strings.Replace(httpsURL, "127.0.0.1", "localhost", 1), "address_not_allowed"},
		{"https://[::1]:8443/hook", "address_not_allowed"},
		{"https://169.254.169.254/latest/meta-data", "address_not_allowed"},
		{"https://does-not-exist.invalid/hook", "address_not_allowed"},
	} {
		sub := mcpSubscription{Principal: "user_1", Name: "order.created", URL: tc.url, Secret: newStandardWebhooksSecret()}
		status, raw := a.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d: %s", tc.url, status, raw)
		}
		want := fmt.Sprintf(`{"mcp_error":{"kind":"invalid_params","code":-32602,"message":"InvalidParams","data":{"field":"delivery.url","reason":%q}}}`, tc.reason)
		if string(raw) != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.url, raw, want)
		}
	}
	if got := receiver.matching(path, nil); len(got) != 0 {
		t.Errorf("the callback got %d requests", len(got))
	}
}
