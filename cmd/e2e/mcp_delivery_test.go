package e2e_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/suite"
)

// TestMCP_Delivery runs the MCP destination against a reference receiver that
// checks every request against the delivery rules.
func TestMCP_Delivery(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &mcpSuite{})
}

const (
	mcpTopic = "user.created"
	// mcpRetryWait covers the retries of the MCP schedule with margin.
	mcpRetryWait = 4 * time.Second
	// mcpVerifyTimeout is the fixed verification timeout.
	mcpVerifyTimeout = 10 * time.Second
)

var challengeCategories = []string{"challenge_failed", "connection_refused", "timeout", "tls_error"}

type mcpSuite struct {
	suite.Suite
	base *basicSuite
	r    *mcpreceiver.Receiver
	// configure overrides the delivery config; nil means withMCPDelivery.
	configure func(*config.Config)
}

func (s *mcpSuite) SetupSuite() {
	r, err := mcpreceiver.New()
	if errors.Is(err, mcpreceiver.ErrCannotBind) {
		s.T().Skipf("MCP delivery suite needs 127.0.0.2: %v", err)
	}
	s.Require().NoError(err)
	s.r = r
	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		configure:      withMCPDelivery,
	}
	if s.configure != nil {
		s.base.configure = s.configure
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
	s.base.httpClient.Timeout = mcpVerifyTimeout + 5*time.Second
}

func (s *mcpSuite) SetupTest() {
	s.base.SetT(s.T())
}

func (s *mcpSuite) TearDownSuite() {
	if s.r == nil {
		return
	}
	violations := s.r.Violations()
	s.r.Close()
	for _, v := range violations {
		s.T().Errorf("%s: %s", v.Check, v.Detail)
	}
	s.base.TearDownSuite()
}

func (s *mcpSuite) check(ok bool, format string, args ...any) {
	s.T().Helper()
	s.Assert().True(ok, append([]any{format}, args...)...)
}

// =============================================================================
// Fixtures
// =============================================================================

// mcpDefaultPrincipal is the principal subscriptions are made for unless a
// test picks another.
const mcpDefaultPrincipal = "e2e"

// mcpPath is one callback path with the secret used on it and the ID a
// subscription of principal gets.
type mcpPath struct {
	path, url, destID, secret, principal string
	// args is the canonical JSON of the subscription's arguments; empty for none.
	args string
}

func newWhsec() string {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return "whsec_" + base64.StdEncoding.EncodeToString(key)
}

// subscriptionID is the ID the subscribe route derives for a subscription to
// mcpTopic.
func subscriptionID(principal, callbackURL, args string) string {
	_, normalized, err := mcpevents.NormalizeCallbackURL(callbackURL)
	if err != nil {
		return ""
	}
	return mcpevents.DeriveSubscriptionID(principal, normalized, mcpTopic, json.RawMessage(args))
}

// forPrincipal returns the path as seen by another principal's subscription.
func (p mcpPath) forPrincipal(principal string) mcpPath {
	p.principal = principal
	p.destID = subscriptionID(principal, p.url, p.args)
	return p
}

// withArguments returns the path as seen by a subscription with the given
// canonical JSON arguments, and makes the receiver expect its ID.
func (s *mcpSuite) withArguments(p mcpPath, args string) mcpPath {
	p.args = args
	p.destID = subscriptionID(p.principal, p.url, p.args)
	s.r.Set(p.path, mcpreceiver.PathConfig{Secrets: []string{p.secret}, SubscriptionID: p.destID})
	return p
}

// newPath registers a callback path answering as cfg says; the receiver
// checks its requests against the returned secret and the ID of the default
// principal's subscription.
func (s *mcpSuite) newPath(cfg mcpreceiver.PathConfig) mcpPath {
	p := mcpPath{secret: newWhsec(), principal: mcpDefaultPrincipal}
	cfg.Secrets = []string{p.secret}
	p.path, p.url = s.r.Register(cfg)
	p.destID = subscriptionID(p.principal, p.url, "")
	cfg.SubscriptionID = p.destID
	s.r.Set(p.path, cfg)
	return p
}

// mcpSubscribeOpts overrides what subscribe sends for a path.
type mcpSubscribeOpts struct {
	secret    string
	ttlMillis int64
}

func (s *mcpSuite) destURL(tenantID string, parts ...string) string {
	return s.base.apiURL("/tenants/" + tenantID + "/destinations" + strings.Join(parts, "/"))
}

// mcpErrorBody is the failure of a subscribe call.
type mcpErrorBody struct {
	MCPError struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	} `json:"mcp_error"`
}

func (b mcpErrorBody) reason() string {
	reason, _ := b.MCPError.Data["reason"].(string)
	return reason
}

func parseMCPError(raw json.RawMessage) mcpErrorBody {
	var b mcpErrorBody
	_ = json.Unmarshal(raw, &b)
	return b
}

// subscribe posts the subscribe call of p's principal and returns the status
// and raw body.
func (s *mcpSuite) subscribe(tenantID string, p mcpPath, o mcpSubscribeOpts) (int, json.RawMessage) {
	s.T().Helper()
	secret := p.secret
	if o.secret != "" {
		secret = o.secret
	}
	params := map[string]any{
		"name":     mcpTopic,
		"delivery": map[string]any{"mode": "webhook", "url": p.url, "secret": secret},
	}
	if o.ttlMillis != 0 {
		params["ttlMs"] = o.ttlMillis
	}
	if p.args != "" {
		params["arguments"] = json.RawMessage(p.args)
	}
	var raw json.RawMessage
	status := s.base.doJSON(http.MethodPost, s.base.apiURL("/tenants/"+tenantID+"/mcp/events/subscribe"),
		map[string]any{"principal": p.principal, "params": params}, &raw)
	return status, raw
}

// mustSubscribe subscribes and checks the subscription got the ID of p.
func (s *mcpSuite) mustSubscribe(tenantID string, p mcpPath, o mcpSubscribeOpts) {
	s.T().Helper()
	status, raw := s.subscribe(tenantID, p, o)
	s.Require().Equal(http.StatusOK, status, "subscribe %s: %s", p.destID, raw)
	var res struct {
		ID string `json:"id"`
	}
	s.Require().NoError(json.Unmarshal(raw, &res))
	s.Require().Equal(p.destID, res.ID)
}

func (s *mcpSuite) publishMCP(tenantID string, data any) publishResponse {
	s.T().Helper()
	return s.base.publish(tenantID, mcpTopic, data, withRetry())
}

type mcpAttempt struct {
	ID            string `json:"id"`
	EventID       string `json:"event_id"`
	DestinationID string `json:"destination_id"`
	Status        string `json:"status"`
	Code          string `json:"code"`
	AttemptNumber int    `json:"attempt_number"`
}

func (s *mcpSuite) attempts(tenantID, destID string) []mcpAttempt {
	s.T().Helper()
	var resp struct {
		Models []mcpAttempt `json:"models"`
	}
	status := s.base.doJSON(http.MethodGet, s.base.apiURL("/attempts?tenant_id="+tenantID+"&destination_id="+destID+"&limit=100"), nil, &resp)
	s.Require().Equal(http.StatusOK, status)
	return resp.Models
}

// waitAttempts polls until the destination has at least n attempts.
func (s *mcpSuite) waitAttempts(tenantID, destID string, n int, timeout time.Duration) []mcpAttempt {
	s.T().Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := s.attempts(tenantID, destID)
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func codes(as []mcpAttempt) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Code
	}
	return out
}

func allCode(as []mcpAttempt, code string) bool {
	for _, a := range as {
		if a.Code != code {
			return false
		}
	}
	return len(as) > 0
}

func (s *mcpSuite) destination(tenantID, destID string) (disabled bool) {
	return s.base.getDestination(tenantID, destID).DisabledAt != nil
}

func (s *mcpSuite) sentinelURLs() (v4, v6 string) {
	if p := s.r.SentinelV4Port(); p != 0 {
		v4 = fmt.Sprintf("https://127.0.0.1:%d/", p)
	}
	if p := s.r.SentinelV6Port(); p != 0 {
		v6 = fmt.Sprintf("https://[::1]:%d/", p)
	}
	return v4, v6
}

// =============================================================================
// Envelope, headers, signature, IDs, verification first
// =============================================================================

func (s *mcpSuite) TestDeliveryBasics() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})

	challenges := s.r.Challenges(p.path)
	s.check(len(challenges) == 1, "challenges received at subscribe: %d", len(challenges))
	s.Require().Empty(s.r.Events(p.path), "no event before the first publish")

	pub1 := s.publishMCP(tenant, map[string]any{"n": 1})
	pub2 := s.publishMCP(tenant, map[string]any{"n": 2})
	s.Equal([]string{p.destID}, pub1.DestinationIDs)
	events := s.r.WaitFor(p.path, 2, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 2)
	got := map[string]bool{events[0].Envelope().EventID: true, events[1].Envelope().EventID: true}
	s.True(got[pub1.ID] && got[pub2.ID], "eventIds %v, published %s %s", got, pub1.ID, pub2.ID)
	s.Equal("user.created", events[0].Envelope().Name)
}

// With MCP on, publishing to an mcp destination by ID delivers.
func (s *mcpSuite) TestPublishByID() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})

	var pub publishResponse
	status := s.base.doJSON(http.MethodPost, s.base.apiURL("/publish"), map[string]any{
		"tenant_id": tenant, "topic": mcpTopic, "destination_id": p.destID,
		"data": map[string]any{"n": 1},
	}, &pub)
	s.Equal(http.StatusAccepted, status)
	s.Equal([]string{p.destID}, pub.DestinationIDs)
	s.Require().Len(s.r.WaitFor(p.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout), 1)
}

// =============================================================================
// Retries re-sign
// =============================================================================

func (s *mcpSuite) TestRetryResigns() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{Event: mcpreceiver.Sequence(500, 500)})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	s.publishMCP(tenant, map[string]any{"n": 1})
	events := s.r.WaitFor(p.path, 3, mcpreceiver.Request.IsEvent, 15*time.Second)
	s.Len(events, 3, "1 attempt + 2 retries")
	s.check(
		events[0].Header.Get("webhook-id") != "" &&
			events[0].Header.Get("webhook-id") == events[1].Header.Get("webhook-id") &&
			events[1].Header.Get("webhook-id") == events[2].Header.Get("webhook-id") &&
			!strings.Contains(events[0].Header.Get("webhook-id"), events[0].Envelope().EventID),
		"webhook-ids %q %q %q", events[0].Header.Get("webhook-id"), events[1].Header.Get("webhook-id"), events[2].Header.Get("webhook-id"))
	time.Sleep(mcpRetryWait)
	s.Len(s.r.Events(p.path), 3, "success on the third stops the retries")
}

// =============================================================================
// Verification
// =============================================================================

func (s *mcpSuite) TestVerificationFailures() {
	tenant := s.base.createTenant().ID

	cases := []struct {
		name    string
		cfg     mcpreceiver.PathConfig
		wantTyp string
	}{
		{"wrong-echo", mcpreceiver.PathConfig{Challenge: mcpreceiver.WrongEcho()}, "challenge_failed"},
		{"status-500", mcpreceiver.PathConfig{Challenge: mcpreceiver.Status(500)}, "challenge_failed"},
		{"status-404", mcpreceiver.PathConfig{Challenge: mcpreceiver.Status(404)}, "challenge_failed"},
		{"sleep", mcpreceiver.PathConfig{Challenge: mcpreceiver.Sleep(mcpVerifyTimeout + 2*time.Second)}, "timeout"},
		{"redirect", mcpreceiver.PathConfig{Challenge: mcpreceiver.Redirect("/redirect-target/verify")}, ""},
		{"marker", mcpreceiver.PathConfig{Challenge: mcpreceiver.Marker()}, "challenge_failed"},
	}
	for _, c := range cases {
		p := s.newPath(c.cfg)
		status, raw := s.subscribe(tenant, p, mcpSubscribeOpts{})
		failure := parseMCPError(raw)
		reason := failure.reason()
		okReason := reason == c.wantTyp
		if c.wantTyp == "" {
			okReason = contains(challengeCategories, reason)
		}
		s.check(status == http.StatusUnprocessableEntity && failure.MCPError.Code == -32027 && failure.MCPError.Message == "CallbackEndpointError" && okReason,
			"%s: status %d error %+v, body %s", c.name, status, failure.MCPError, raw)
		s.check(!strings.Contains(string(raw), mcpreceiver.MarkerText) && !strings.Contains(string(raw), p.url),
			"%s: response body %s", c.name, raw)

		s.publishMCP(tenant, map[string]any{"c": c.name})
		time.Sleep(200 * time.Millisecond)
		s.Empty(s.r.Events(p.path), "%s: no event delivered to a destination that failed verification", c.name)
	}
	s.Empty(s.r.Requests("/redirect-target/verify"), "verification redirect followed")
}

func (s *mcpSuite) TestVerificationCache() {
	tenant := s.base.createTenant().ID
	first := s.newPath(mcpreceiver.PathConfig{})
	s.r.Set(first.path, mcpreceiver.PathConfig{Secrets: []string{first.secret}})
	alice := first.forPrincipal("alice")
	s.mustSubscribe(tenant, alice, mcpSubscribeOpts{})
	s.Len(s.r.Challenges(first.path), 1)

	s.mustSubscribe(tenant, alice, mcpSubscribeOpts{})
	cached := len(s.r.Challenges(first.path)) == 1

	s.mustSubscribe(tenant, first.forPrincipal("bob"), mcpSubscribeOpts{})
	reverified := len(s.r.Challenges(first.path)) == 2

	s.check(cached && reverified,
		"challenges after the same principal and URL: cached=%v; after other principal: %d total", cached, len(s.r.Challenges(first.path)))
}

// =============================================================================
// SSRF and TLS
// =============================================================================

func (s *mcpSuite) TestDeliveryRedirectNotFollowed() {
	tenant := s.base.createTenant().ID
	target := "/redirect-target/" + idgen.String()
	p := s.newPath(mcpreceiver.PathConfig{Event: mcpreceiver.Redirect(target)})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	s.publishMCP(tenant, map[string]any{"n": 1})
	attempts := s.waitAttempts(tenant, p.destID, 1, attemptPollTimeout)
	time.Sleep(500 * time.Millisecond)
	hits := len(s.r.Requests(target))
	s.check(hits == 0 && len(attempts) > 0 && attempts[0].Code == "307",
		"redirect target hits %d, attempt codes %v", hits, codes(attempts))
}

func (s *mcpSuite) TestRejectedCallbackURLs() {
	tenant := s.base.createTenant().ID
	v4, v6 := s.sentinelURLs()
	s.Require().NotEmpty(v4, "127.0.0.1 sentinel must be bound")
	routable := map[string]string{
		"loopback v4":    v4,
		"localhost":      "https://localhost/",
		"loopback v6":    v6,
		"private":        "https://10.0.0.1/",
		"link-local v4":  "https://169.254.169.254/",
		"link-local v6":  "https://[fe80::1]/",
		"unspecified v4": "https://0.0.0.0/",
	}
	for name, url := range routable {
		if url == "" {
			s.T().Logf("skipping %s: [::1] is not available on this host", name)
			continue
		}
		p := mcpPath{secret: newWhsec(), url: url, principal: mcpDefaultPrincipal}
		status, raw := s.subscribe(tenant, p, mcpSubscribeOpts{})
		failure := parseMCPError(raw)
		s.check(status == http.StatusUnprocessableEntity && failure.MCPError.Code == -32602 && failure.MCPError.Message == "InvalidParams",
			"%s %s: status %d body %s", name, url, status, raw)
	}
	time.Sleep(300 * time.Millisecond)
	s.check(s.r.SentinelAccepts() == 0, "sentinel accepts: %d", s.r.SentinelAccepts())
}

func (s *mcpSuite) TestAddressCheckedAtDeliveryTime() {
	v4, _ := s.sentinelURLs()
	s.Require().NotEmpty(v4, "127.0.0.1 sentinel must be bound")
	tenant := s.base.createTenant().ID
	destID := idgen.Destination()
	writeDestinationToStore(s.T(), s.base.config, models.Destination{
		ID:          destID,
		TenantID:    tenant,
		Type:        "mcp",
		Topics:      models.Topics{mcpTopic},
		Config:      models.Config{"url": v4},
		Credentials: models.Credentials{"secret": newWhsec()},
	})
	s.publishMCP(tenant, map[string]any{"n": 1})
	attempts := s.waitAttempts(tenant, destID, 1, attemptPollTimeout)
	s.check(
		len(attempts) > 0 && allCode(attempts, "address_not_allowed") && s.r.SentinelAccepts() == 0,
		"attempt codes %v, sentinel accepts %d", codes(attempts), s.r.SentinelAccepts())
}

func (s *mcpSuite) TestHTTPSRequired() {
	tenant := s.base.createTenant().ID
	p := mcpPath{secret: newWhsec(), url: "http://93.184.215.14/x", principal: mcpDefaultPrincipal}
	status, raw := s.subscribe(tenant, p, mcpSubscribeOpts{})
	failure := parseMCPError(raw)
	s.check(status == http.StatusUnprocessableEntity && failure.MCPError.Code == -32602 && failure.MCPError.Message == "InvalidParams",
		"status %d body %s", status, raw)
}

// =============================================================================
// Non-retryable outcomes
// =============================================================================

// nonRetryable sends more events than the consecutive-failure limit to a
// path answering status, and expects one attempt each, no retry and an
// enabled destination.
func (s *mcpSuite) nonRetryable(status int) {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{Event: mcpreceiver.Status(status)})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	const events = 6
	for i := range events {
		s.publishMCP(tenant, map[string]any{"n": i})
	}
	s.r.WaitFor(p.path, events, mcpreceiver.Request.IsEvent, 15*time.Second)
	time.Sleep(mcpRetryWait)
	requests := len(s.r.Events(p.path))
	attempts := s.waitAttempts(tenant, p.destID, events, 5*time.Second)
	disabled := s.destination(tenant, p.destID)
	s.publishMCP(tenant, map[string]any{"n": "next"})
	next := len(s.r.WaitFor(p.path, events+1, mcpreceiver.Request.IsEvent, 5*time.Second))
	s.check(requests == events && len(attempts) == events && allCode(attempts, fmt.Sprint(status)) && !disabled && next == events+1,
		"requests %d, attempts %v, disabled %v, next-event requests %d", requests, codes(attempts), disabled, next)
}

// TestRepeatedServerErrorsDisableDestination is the control for the
// non-retryable tests: with the same failure threshold, 500s do disable.
func (s *mcpSuite) TestRepeatedServerErrorsDisableDestination() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{Event: mcpreceiver.Status(http.StatusInternalServerError)})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	for i := range 6 {
		s.publishMCP(tenant, map[string]any{"n": i})
	}
	deadline := time.Now().Add(20 * time.Second)
	disabled := s.destination(tenant, p.destID)
	for !disabled && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		disabled = s.destination(tenant, p.destID)
	}
	s.check(disabled, "destination still enabled after repeated 500s, attempts %v", codes(s.attempts(tenant, p.destID)))
}

func (s *mcpSuite) TestGone() {
	s.nonRetryable(http.StatusGone)
}

func (s *mcpSuite) TestPayloadTooLargeStatus() {
	s.nonRetryable(http.StatusRequestEntityTooLarge)
}

func (s *mcpSuite) TestOversizeEnvelope() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	s.publishMCP(tenant, map[string]any{"blob": strings.Repeat("a", 300*1024)})
	attempts := s.waitAttempts(tenant, p.destID, 1, attemptPollTimeout)
	time.Sleep(mcpRetryWait)
	attempts = s.attempts(tenant, p.destID)
	s.check(len(s.r.Events(p.path)) == 0 && len(attempts) == 1 && allCode(attempts, "payload_too_large"),
		"event requests %d, attempt codes %v", len(s.r.Events(p.path)), codes(attempts))
}

// =============================================================================
// Retry policy and timeout
// =============================================================================

func (s *mcpSuite) TestRetriesBounded() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{Event: mcpreceiver.Status(500)})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	s.publishMCP(tenant, map[string]any{"n": 1})
	want := 1 + len(mcpRetrySchedule)
	s.waitAttempts(tenant, p.destID, want, 15*time.Second)
	time.Sleep(mcpRetryWait)
	mcpAttempts := s.attempts(tenant, p.destID)
	mcpRequests := len(s.r.Events(p.path))

	// A webhook destination in the same run keeps the global retry config.
	wpath, wurl := s.r.Register(mcpreceiver.PathConfig{Event: mcpreceiver.Status(500), Unchecked: true})
	wid := idgen.Destination()
	var created destinationResponse
	status := s.base.doJSON(http.MethodPost, s.destURL(tenant), map[string]any{
		"id": wid, "type": "webhook", "topics": []string{mcpTopic}, "config": map[string]any{"url": wurl},
	}, &created)
	s.Require().Equal(http.StatusCreated, status)
	s.publishMCP(tenant, map[string]any{"n": 2})
	wantWebhook := 1 + s.base.config.RetryMaxLimit
	s.waitAttempts(tenant, wid, wantWebhook, 30*time.Second)
	time.Sleep(2 * time.Second)
	webhookAttempts := s.attempts(tenant, wid)

	s.check(
		len(mcpAttempts) == want && mcpRequests == want && len(webhookAttempts) == wantWebhook && len(s.r.Requests(wpath)) == wantWebhook,
		"mcp attempts %d (want %d), receiver %d; webhook attempts %d (want %d)",
		len(mcpAttempts), want, mcpRequests, len(webhookAttempts), wantWebhook)
}

func (s *mcpSuite) TestTimeout() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{Event: mcpreceiver.Sleep(mcpDeliveryTimeoutSeconds*time.Second + time.Second)})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	s.publishMCP(tenant, map[string]any{"n": 1})
	attempts := s.waitAttempts(tenant, p.destID, 2, 20*time.Second)
	s.check(len(attempts) >= 2 && allCode(attempts, "timeout"), "attempt codes %v", codes(attempts))
}

// =============================================================================
// Secret rotation
// =============================================================================

func (s *mcpSuite) TestDualSignOnRotation() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	newSecret := newWhsec()

	s.mustSubscribe(tenant, p, mcpSubscribeOpts{secret: newSecret})
	s.r.SetSecrets(p.path, p.secret, newSecret)

	s.publishMCP(tenant, map[string]any{"n": 1})
	events := s.r.WaitFor(p.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	verified := events[0].VerifiedWith(p.secret, newSecret)
	s.check(
		verified[0] && verified[1] && len(mcpreceiver.SignatureValues(events[0].Header)) == 2,
		"verified with old=%v new=%v, v1 signatures %d", verified[0], verified[1], len(mcpreceiver.SignatureValues(events[0].Header)))
}

// =============================================================================
// expires_at
// =============================================================================

func (s *mcpSuite) TestExpiry() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{Event: mcpreceiver.Status(500)})
	subscribed := time.Now()
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{ttlMillis: 2000})
	expires := subscribed.Add(2 * time.Second)

	before := s.publishMCP(tenant, map[string]any{"n": "before"})
	first := s.r.WaitFor(p.path, 1, mcpreceiver.Request.IsEvent, 5*time.Second)
	time.Sleep(time.Until(expires.Add(mcpRetryWait)))
	events := s.r.Events(p.path)
	pendingStopped := true
	for _, e := range events {
		if e.Received.After(expires.Add(500 * time.Millisecond)) {
			pendingStopped = false
		}
	}

	after := s.publishMCP(tenant, map[string]any{"n": "after"})
	time.Sleep(time.Second)
	afterAttempts := 0
	for _, a := range s.attempts(tenant, p.destID) {
		if a.EventID == after.ID {
			afterAttempts++
		}
	}
	afterRequests := 0
	for _, e := range s.r.Events(p.path) {
		if e.Envelope().EventID == after.ID {
			afterRequests++
		}
	}
	s.check(
		len(first) >= 1 && first[0].Envelope().EventID == before.ID && pendingStopped && len(after.DestinationIDs) == 0 && afterAttempts == 0 && afterRequests == 0,
		"delivered before expiry: %d, no send after expiry: %v, after-expiry fan-out %v, attempts %d, requests %d",
		len(first), pendingStopped, after.DestinationIDs, afterAttempts, afterRequests)
}

// =============================================================================
// One event, two subscriptions
// =============================================================================

func (s *mcpSuite) TestEventIDPerSubscription() {
	tenant := s.base.createTenant().ID
	base := s.newPath(mcpreceiver.PathConfig{})
	s.r.Set(base.path, mcpreceiver.PathConfig{Secrets: []string{base.secret}})
	a, b := base.forPrincipal("u1"), base.forPrincipal("u2")

	s.mustSubscribe(tenant, a, mcpSubscribeOpts{})
	s.mustSubscribe(tenant, b, mcpSubscribeOpts{})
	pub := s.publishMCP(tenant, map[string]any{"n": 1})
	events := s.r.WaitFor(a.path, 2, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 2)
	e0, e1 := events[0], events[1]
	subs := map[string]bool{e0.Header.Get("X-MCP-Subscription-Id"): true, e1.Header.Get("X-MCP-Subscription-Id"): true}
	s.check(
		e0.Envelope().EventID == pub.ID && e1.Envelope().EventID == pub.ID &&
			e0.Header.Get("webhook-id") != e1.Header.Get("webhook-id") && subs[a.destID] && subs[b.destID],
		"eventIds %q %q, webhook-ids %q %q, subscription ids %v",
		e0.Envelope().EventID, e1.Envelope().EventID, e0.Header.Get("webhook-id"), e1.Header.Get("webhook-id"), subs)
}

// TestErrorResponses_MCP pins the error responses specific to mcp
// destinations.
func (s *mcpSuite) TestErrorResponses_MCP() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})
	create := map[string]any{
		"id": idgen.Destination(), "type": "mcp", "topics": []string{mcpTopic},
		"config": map[string]any{"url": p.url}, "credentials": map[string]any{"secret": p.secret},
	}
	webhookWithExpiry := map[string]any{
		"type": "webhook", "topics": []string{mcpTopic}, "config": map[string]any{"url": "https://203.0.113.10/"},
		"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	dest := "/tenants/" + tenant + "/destinations"
	admin := s.base.adminAuth()
	useSubscribe := "mcp destinations can't be created or updated here; use POST /tenants/" + tenant + "/mcp/events/subscribe"

	s.base.runErrorCases([]errorCase{
		{name: "admin creates mcp", method: http.MethodPost, path: dest, auth: admin, body: create,
			status: http.StatusUnprocessableEntity, message: useSubscribe},
		{name: "tenant token creates mcp", method: http.MethodPost, path: dest, auth: s.base.tenantAuth(tenant), body: create,
			status: http.StatusUnprocessableEntity, message: useSubscribe},
		{name: "admin updates mcp", method: http.MethodPatch, path: dest + "/" + p.destID, auth: admin,
			body:   map[string]any{"credentials": map[string]any{"secret": newWhsec()}},
			status: http.StatusUnprocessableEntity, message: useSubscribe},
		{name: "tenant token updates mcp", method: http.MethodPatch, path: dest + "/" + p.destID, auth: s.base.tenantAuth(tenant),
			body:   map[string]any{"expires_at": nil},
			status: http.StatusUnprocessableEntity, message: useSubscribe},
		{name: "expires_at on a webhook", method: http.MethodPost, path: dest, auth: admin, body: webhookWithExpiry,
			status: http.StatusUnprocessableEntity, message: "expires_at is not supported for this destination type"},
		{name: "mcp type hidden from tenants", method: http.MethodGet, path: "/destination-types/mcp", auth: s.base.tenantAuth(tenant),
			status: http.StatusNotFound, message: "destination type not found"},
	})
}

// TestDestinationsAPIReadsAndDeletes checks that a subscription is listed,
// retrieved and deleted like any destination.
func (s *mcpSuite) TestDestinationsAPIReadsAndDeletes() {
	tenant := s.base.createTenant().ID
	p := s.newPath(mcpreceiver.PathConfig{})
	s.mustSubscribe(tenant, p, mcpSubscribeOpts{})

	var got struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		ExpiresAt string `json:"expires_at"`
	}
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.destURL(tenant, "/"+p.destID), nil, &got))
	s.Equal("mcp", got.Type)
	s.NotEmpty(got.ExpiresAt)
	var list []struct {
		ID string `json:"id"`
	}
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.destURL(tenant), nil, &list))
	s.Require().Len(list, 1)
	s.Equal(p.destID, list[0].ID)

	s.Equal(http.StatusOK, s.base.doJSON(http.MethodDelete, s.destURL(tenant, "/"+p.destID), nil, nil))
	s.Equal(http.StatusNotFound, s.base.doJSON(http.MethodGet, s.destURL(tenant, "/"+p.destID), nil, nil))
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
