package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
)

type mcpFixture interface {
	Name() string
	ConfigureOutpost(cfg *config.Config)
	Start(t *testing.T, env mcpFixtureEnv) mcpFixtureServer
}

type mcpFixtureServer interface {
	URL() string
	Close()
}

type mcpFixtureEnv struct {
	OutpostURL string
	APIKey     string
	TTL        mcpTTLRules
	Principals map[string]mcpPrincipal
}

type mcpTTLRules struct {
	Default, Min, Max time.Duration
}

type mcpPrincipal struct {
	Tenant        string
	AllowedTopics []string
}

var mcpFixtures []mcpFixture

func registerMCPFixture(f mcpFixture) { mcpFixtures = append(mcpFixtures, f) }

var mcpTTL = mcpTTLRules{Default: 5 * time.Second, Min: 2 * time.Second, Max: 8 * time.Second}

const (
	mcpCodeInvalidParams = -32602
	mcpCodeHeader        = -32020
	mcpCodeNotFound      = -32023
	mcpCodeForbidden     = -32024
	mcpCodeUnsupported   = -32026
	mcpCodeCallback      = -32027
)

func TestMCP_Subscriptions(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	if len(mcpFixtures) == 0 {
		t.Skip("no MCP subscription fixture registered")
	}
	for _, fx := range mcpFixtures {
		t.Run(fx.Name(), func(t *testing.T) {
			suite.Run(t, &mcpSubscriptionsSuite{fx: fx})
		})
	}
}

type mcpSubscriptionsSuite struct {
	suite.Suite
	fx     mcpFixture
	m      *mcpSuite
	server mcpFixtureServer

	tenantMain     string
	alice, limited string
	pool           []string
	next           int
}

// poolSize bounds the tests in the suite: each one takes its own principal,
// because verification is rate limited per principal.
const poolSize = 24

func (s *mcpSubscriptionsSuite) SetupSuite() {
	file := filepath.Join(s.T().TempDir(), "events.yaml")
	s.Require().NoError(os.WriteFile(file, []byte(mcpEventsYAML), 0o600))
	s.m = &mcpSuite{configure: func(cfg *config.Config) {
		withMCPDelivery(cfg)
		cfg.Topics = testutil.TestTopics
		s.Require().NoError(cfg.Experimental.MCP.EventsSchema.UnmarshalText([]byte(file)))
		cfg.MaxDestinationsPerTenant = 100
		s.fx.ConfigureOutpost(cfg)
	}}
	s.m.SetT(s.T())
	s.m.SetupSuite()

	s.tenantMain = s.m.base.createTenant().ID
	suffix := idgen.String()
	s.alice, s.limited = "alice-"+suffix, "limited-"+suffix
	principals := map[string]mcpPrincipal{
		s.alice:   {Tenant: s.tenantMain},
		s.limited: {Tenant: s.tenantMain, AllowedTopics: []string{"user.updated"}},
	}
	for i := 0; i < poolSize; i++ {
		name := fmt.Sprintf("user-%d-%s", i, suffix)
		s.pool = append(s.pool, name)
		principals[name] = mcpPrincipal{Tenant: s.tenantMain}
	}
	s.server = s.fx.Start(s.T(), mcpFixtureEnv{
		OutpostURL: s.m.base.apiURL(""),
		APIKey:     s.m.base.config.APIKey,
		TTL:        mcpTTL,
		Principals: principals,
	})
}

func (s *mcpSubscriptionsSuite) SetupTest() {
	s.m.SetT(s.T())
	s.m.SetupTest()
}

func (s *mcpSubscriptionsSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Close()
	}
	if s.m != nil {
		s.m.TearDownSuite()
	}
}

// =============================================================================
// Helpers
// =============================================================================

func (s *mcpSubscriptionsSuite) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	s.T().Cleanup(cancel)
	return ctx
}

func (s *mcpSubscriptionsSuite) fresh() string {
	s.Require().Less(s.next, len(s.pool), "principal pool exhausted")
	p := s.pool[s.next]
	s.next++
	return p
}

func (s *mcpSubscriptionsSuite) session(principal string) *mcpclient.Client {
	s.T().Helper()
	c := mcpclient.New(s.server.URL(), principal)
	_, _, err := c.Initialize(s.ctx())
	s.Require().NoError(err)
	return c
}

type mcpSub struct {
	principal string
	name      string
	args      map[string]any
	path, url string
	secret    string
	id        string
}

func (s *mcpSubscriptionsSuite) newSub(principal, name string, args map[string]any, challenge mcpreceiver.Behavior) *mcpSub {
	s.T().Helper()
	sub := &mcpSub{principal: principal, name: name, args: args, secret: mcpclient.NewSecret()}
	sub.path, sub.url = s.m.r.Register(mcpreceiver.PathConfig{})
	_, normalized, err := mcpevents.NormalizeCallbackURL(sub.url)
	s.Require().NoError(err)
	var canonical []byte
	if args != nil {
		canonical, err = mcpevents.CanonicalJSON(args)
		s.Require().NoError(err)
	}
	sub.id = mcpevents.DeriveSubscriptionID(principal, normalized, name, canonical)
	s.m.r.Set(sub.path, mcpreceiver.PathConfig{Secrets: []string{sub.secret}, SubscriptionID: sub.id, Challenge: challenge})
	return sub
}

func (sub *mcpSub) subscribeParams(ttl any) mcpclient.SubscribeParams {
	return mcpclient.SubscribeParams{Name: sub.name, Arguments: sub.args, URL: sub.url, Secret: sub.secret, TTLMs: ttl}
}

func (s *mcpSubscriptionsSuite) subscribe(c *mcpclient.Client, sub *mcpSub, ttl any) (*mcpclient.Response, mcpclient.SubscribeResult) {
	s.T().Helper()
	resp, err := c.Subscribe(s.ctx(), sub.subscribeParams(ttl))
	s.Require().NoError(err)
	var res mcpclient.SubscribeResult
	if resp.Error == nil {
		s.Require().NoError(resp.Decode(&res), "%s", resp.Raw)
	}
	return resp, res
}

func (s *mcpSubscriptionsSuite) mustSubscribe(c *mcpclient.Client, sub *mcpSub, ttl any) mcpclient.SubscribeResult {
	s.T().Helper()
	resp, res := s.subscribe(c, sub, ttl)
	s.Require().Nil(resp.Error, "subscribe: %s", resp.Raw)
	s.Require().Equal(sub.id, res.ID)
	return res
}

func (s *mcpSubscriptionsSuite) unsubscribe(c *mcpclient.Client, sub *mcpSub) *mcpclient.Response {
	s.T().Helper()
	resp, err := c.Unsubscribe(s.ctx(), mcpclient.UnsubscribeParams{Name: sub.name, Arguments: sub.args, URL: sub.url})
	s.Require().NoError(err)
	return resp
}

func (s *mcpSubscriptionsSuite) refreshBefore(res mcpclient.SubscribeResult) time.Time {
	s.T().Helper()
	s.Require().NotNil(res.RefreshBefore, "refreshBefore")
	t, err := time.Parse(time.RFC3339, *res.RefreshBefore)
	s.Require().NoError(err)
	return t
}

// requireGrant checks that refreshBefore is the TTL after the request,
// allowing for the truncation to whole seconds.
func (s *mcpSubscriptionsSuite) requireGrant(res mcpclient.SubscribeResult, before, after time.Time, ttl time.Duration) {
	s.T().Helper()
	s.Equal("complete", res.ResultType)
	rb := s.refreshBefore(res)
	lo := before.Add(ttl - time.Second - 500*time.Millisecond)
	hi := after.Add(ttl + 500*time.Millisecond)
	s.Truef(!rb.Before(lo) && !rb.After(hi), "refreshBefore %s, want within [%s, %s] (ttl %s)", rb.Format(time.RFC3339), lo.Format(time.RFC3339), hi.Format(time.RFC3339), ttl)
}

func (s *mcpSubscriptionsSuite) publish(tenant string, data map[string]any) publishResponse {
	s.T().Helper()
	return s.m.publishMCP(tenant, data)
}

func (s *mcpSubscriptionsSuite) destinationStatus(tenant, id string) int {
	s.T().Helper()
	return s.m.base.doJSON(http.MethodGet, s.m.destURL(tenant, "/"+id), nil, nil)
}

func (s *mcpSubscriptionsSuite) requireEventsAfterWait(sub *mcpSub, n int) {
	s.T().Helper()
	time.Sleep(mcpRetryWait)
	got := s.m.r.Events(sub.path)
	s.Lenf(got, n, "events at %s", sub.path)
}

// =============================================================================
// Scenarios
// =============================================================================

func (s *mcpSubscriptionsSuite) TestInitializeAdvertisesEventsCapability() {
	c := mcpclient.New(s.server.URL(), s.alice)
	res, _, err := c.Initialize(s.ctx())
	s.Require().NoError(err)
	s.Contains(res.Capabilities.Extensions, mcpclient.ExtensionID)
}

func (s *mcpSubscriptionsSuite) TestDiscoverAdvertisesEventsExtension() {
	c := mcpclient.New(s.server.URL(), "", mcpclient.Stateless())
	res, _, err := c.Discover(s.ctx())
	s.Require().NoError(err)
	s.Equal("complete", res.ResultType)
	s.Contains(res.SupportedVersions, mcpclient.DefaultProtocolVersion)
	s.Contains(res.Capabilities.Extensions, mcpclient.ExtensionID)
}

func (s *mcpSubscriptionsSuite) TestEventsListShowsAllowedTopics() {
	names := func(principal string) []string {
		resp, err := s.session(principal).EventsList(s.ctx())
		s.Require().NoError(err)
		var list mcpclient.ListResult
		s.Require().NoError(resp.Decode(&list), "%s", resp.Raw)
		s.Equal("complete", list.ResultType)
		var out []string
		for _, e := range list.Events {
			out = append(out, e.Name)
			s.NotEmpty(e.InputSchema, "inputSchema of %s", e.Name)
		}
		return out
	}
	s.ElementsMatch([]string{"user.created", "user.updated"}, names(s.alice))
	s.Equal([]string{"user.updated"}, names(s.limited))
}

func (s *mcpSubscriptionsSuite) TestSubscribeVerifiesCallbackAndReturnsDerivedID() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())

	before := time.Now()
	res := s.mustSubscribe(c, sub, nil)
	after := time.Now()

	s.Len(s.m.r.Challenges(sub.path), 1, "one challenge, before any event")
	s.Empty(s.m.r.Events(sub.path))
	s.True(strings.HasPrefix(res.ID, "sub_"))
	s.Len(res.ID, len("sub_")+32)
	s.Nil(res.Cursor)
	s.False(res.Truncated)
	s.requireGrant(res, before, after, mcpTTL.Default)
	s.Equal(http.StatusOK, s.destinationStatus(s.tenantMain, sub.id), "stored under the derived ID")
}

func (s *mcpSubscriptionsSuite) TestSubscribeWithoutDeliveryModeIsWebhook() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())
	p := sub.subscribeParams(nil)
	p.OmitMode = true
	resp, err := c.Subscribe(s.ctx(), p)
	s.Require().NoError(err)
	s.Require().Nil(resp.Error, "%s", resp.Raw)
	s.Equal(http.StatusOK, s.destinationStatus(s.tenantMain, sub.id))
}

func (s *mcpSubscriptionsSuite) TestSubscribedEventsAreDeliveredWhenArgumentsMatch() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", map[string]any{"status": "paid"}, mcpreceiver.Echo())
	s.mustSubscribe(c, sub, nil)

	matching := s.publish(s.tenantMain, map[string]any{"status": "paid", "total": 10})
	events := s.m.r.WaitFor(sub.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	s.Equal(matching.ID, events[0].Envelope().EventID)
	s.Equal("user.created", events[0].Envelope().Name)
	s.Contains(matching.DestinationIDs, sub.id)

	other := s.publish(s.tenantMain, map[string]any{"status": "pending"})
	s.NotContains(other.DestinationIDs, sub.id)
	s.requireEventsAfterWait(sub, 1)
}

func (s *mcpSubscriptionsSuite) TestRefreshKeepsIDAndRotatesSecret() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())
	first := s.mustSubscribe(c, sub, nil)

	time.Sleep(1100 * time.Millisecond)
	old := sub.secret
	sub.secret = mcpclient.NewSecret()
	s.m.r.SetSecrets(sub.path, old, sub.secret)
	second := s.mustSubscribe(c, sub, nil)

	s.Equal(first.ID, second.ID)
	s.True(s.refreshBefore(second).After(s.refreshBefore(first)), "refreshBefore moves forward")
	s.Len(s.m.r.Challenges(sub.path), 1, "no new challenge for the same principal and URL")
	s.Require().NotNil(second.DeliveryStatus)
	s.True(second.DeliveryStatus.Active)

	s.publish(s.tenantMain, map[string]any{"n": 1})
	events := s.m.r.WaitFor(sub.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	s.Len(mcpreceiver.SignatureValues(events[0].Header), 2, "signed with both secrets")
	s.Equal([]bool{true, true}, events[0].VerifiedWith(old, sub.secret))
}

func (s *mcpSubscriptionsSuite) TestTTLGrants() {
	user := s.fresh()
	c := s.session(user)
	cases := []struct {
		name string
		ttl  any
		want time.Duration
	}{
		{"omitted gets the default", nil, mcpTTL.Default},
		{"below the minimum gets the minimum", 1, mcpTTL.Min},
		{"above the maximum gets the maximum", int(time.Hour / time.Millisecond), mcpTTL.Max},
		{"within bounds is kept", 6500, 6500 * time.Millisecond},
		{"null gets a finite grant", mcpclient.TTLNull, mcpTTL.Max},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			sub := s.newSub(user, "user.updated", nil, mcpreceiver.Echo())
			before := time.Now()
			res := s.mustSubscribe(c, sub, tc.ttl)
			s.requireGrant(res, before, time.Now(), tc.want)
		})
	}
}

func (s *mcpSubscriptionsSuite) TestSubscriptionStopsDeliveringAfterExpiry() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())
	before := time.Now()
	s.mustSubscribe(c, sub, int(mcpTTL.Min/time.Millisecond))

	s.publish(s.tenantMain, map[string]any{"n": 1})
	s.Require().Len(s.m.r.WaitFor(sub.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout), 1)

	time.Sleep(time.Until(before.Add(mcpTTL.Min + time.Second)))
	late := s.publish(s.tenantMain, map[string]any{"n": 2})
	s.NotContains(late.DestinationIDs, sub.id)
	s.requireEventsAfterWait(sub, 1)
	s.Len(s.m.attempts(s.tenantMain, sub.id), 1)

	s.mustSubscribe(c, sub, nil)
	s.publish(s.tenantMain, map[string]any{"n": 3})
	s.Len(s.m.r.WaitFor(sub.path, 2, mcpreceiver.Request.IsEvent, mockServerPollTimeout), 2, "refresh after expiry delivers again")
}

func (s *mcpSubscriptionsSuite) TestResubscribeAfterExpiryWithNewSecretIsFresh() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())
	before := time.Now()
	s.mustSubscribe(c, sub, int(mcpTTL.Min/time.Millisecond))
	time.Sleep(time.Until(before.Add(mcpTTL.Min + time.Second)))

	old := sub.secret
	sub.secret = mcpclient.NewSecret()
	s.m.r.SetSecrets(sub.path, old, sub.secret)
	res := s.mustSubscribe(c, sub, nil)

	s.Nil(res.DeliveryStatus, "a new subscription has no refresh status")

	s.publish(s.tenantMain, map[string]any{"n": 1})
	events := s.m.r.WaitFor(sub.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout)
	s.Require().Len(events, 1)
	s.Len(mcpreceiver.SignatureValues(events[0].Header), 1, "signed with the new secret only")
	s.Equal([]bool{true, false}, events[0].VerifiedWith(sub.secret, old))
}

func (s *mcpSubscriptionsSuite) TestRefreshReportsAndReenablesSuspendedSubscription() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())
	s.mustSubscribe(c, sub, nil)

	s.m.base.disableDestination(s.tenantMain, sub.id)
	paused := s.publish(s.tenantMain, map[string]any{"n": 1})
	s.NotContains(paused.DestinationIDs, sub.id)
	s.requireEventsAfterWait(sub, 0)

	res := s.mustSubscribe(c, sub, nil)
	s.Require().NotNil(res.DeliveryStatus)
	s.False(res.DeliveryStatus.Active, "reports that it was suspended")

	s.publish(s.tenantMain, map[string]any{"n": 2})
	s.Len(s.m.r.WaitFor(sub.path, 1, mcpreceiver.Request.IsEvent, mockServerPollTimeout), 1, "delivery resumes")

	res = s.mustSubscribe(c, sub, nil)
	s.Require().NotNil(res.DeliveryStatus)
	s.True(res.DeliveryStatus.Active)
}

func (s *mcpSubscriptionsSuite) TestUnsubscribeStopsDeliveryAndIsNotIdempotent() {
	user := s.fresh()
	c := s.session(user)
	sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())
	s.mustSubscribe(c, sub, nil)

	resp := s.unsubscribe(c, sub)
	s.Require().Nil(resp.Error, "%s", resp.Raw)
	s.JSONEq(`{"resultType":"complete"}`, string(resp.Result))
	s.Equal(http.StatusNotFound, s.destinationStatus(s.tenantMain, sub.id))

	gone := s.publish(s.tenantMain, map[string]any{"n": 1})
	s.NotContains(gone.DestinationIDs, sub.id)
	s.requireEventsAfterWait(sub, 0)

	resp = s.unsubscribe(c, sub)
	s.Require().NotNil(resp.Error, "%s", resp.Raw)
	s.Equal(mcpCodeNotFound, resp.Error.Code)
	s.Equal("subscription", resp.Error.DataMap()["kind"])
}

func (s *mcpSubscriptionsSuite) TestSubscribeErrors() {
	mk := func(principal, name string, challenge mcpreceiver.Behavior) *mcpSub {
		return s.newSub(principal, name, nil, challenge)
	}
	cases := []struct {
		name      string
		principal string
		sub       func() *mcpSub
		mutate    func(*mcpclient.SubscribeParams)
		code      int
	}{
		{"plain http to a public address", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.URL = "http://93.184.215.14/x" }, mcpCodeInvalidParams},
		{"address not allowed", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.URL = "https://169.254.169.254/" }, mcpCodeInvalidParams},
		{"secret without the whsec_ prefix", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.Secret = "not-a-webhook-secret" }, mcpCodeInvalidParams},
		{"delivery mode other than webhook", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.Mode = "poll" }, mcpCodeUnsupported},
		{"unknown event", s.alice, func() *mcpSub { return mk(s.alice, "order.unknown", mcpreceiver.Echo()) },
			nil, mcpCodeNotFound},
		{"event outside the principal's topics", s.limited, func() *mcpSub { return mk(s.limited, "user.created", mcpreceiver.Echo()) },
			nil, mcpCodeForbidden},
		{"argument not in the inputSchema", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.Arguments = map[string]any{"total": 10} }, mcpCodeInvalidParams},
		{"argument outside the enum", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.Arguments = map[string]any{"status": "refunded"} }, mcpCodeInvalidParams},
		{"list argument", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.Arguments = map[string]any{"status": []any{"paid"}} }, mcpCodeInvalidParams},
		{"operator argument", s.alice, func() *mcpSub { return mk(s.alice, "user.created", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.Arguments = map[string]any{"amount": map[string]any{"$gt": 1}} }, mcpCodeInvalidParams},
		{"arguments on a no-argument event", s.alice, func() *mcpSub { return mk(s.alice, "user.updated", mcpreceiver.Echo()) },
			func(p *mcpclient.SubscribeParams) { p.Arguments = map[string]any{"id": "x"} }, mcpCodeInvalidParams},
		{"no principal", "", func() *mcpSub { return mk("", "user.created", mcpreceiver.Echo()) },
			nil, mcpCodeForbidden},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			sub := tc.sub()
			c := mcpclient.New(s.server.URL(), tc.principal)
			_, _, err := c.Initialize(s.ctx())
			s.Require().NoError(err)
			p := sub.subscribeParams(nil)
			if tc.mutate != nil {
				tc.mutate(&p)
			}
			resp, err := c.Subscribe(s.ctx(), p)
			s.Require().NoError(err)
			s.Require().NotNil(resp.Error, "%s", resp.Raw)
			s.Equal(tc.code, resp.Error.Code, "%s", resp.Raw)
			if tc.code == mcpCodeInvalidParams || tc.code == mcpCodeForbidden {
				s.Empty(resp.Error.Data, "the spec defines no data for this error: %s", resp.Raw)
			}
			s.Empty(s.m.r.Challenges(sub.path), "no challenge for a rejected request")
		})
	}

	s.Run("failed verification reports the category only", func() {
		user := s.fresh()
		c := s.session(user)
		sub := s.newSub(user, "user.created", nil, mcpreceiver.Marker())
		resp, _ := s.subscribe(c, sub, nil)
		s.Require().NotNil(resp.Error, "%s", resp.Raw)
		s.Equal(mcpCodeCallback, resp.Error.Code)
		s.Equal(map[string]any{"reason": "challenge_failed"}, resp.Error.DataMap())
		s.NotContains(string(resp.Raw), mcpreceiver.MarkerText)
		s.Equal(http.StatusNotFound, s.destinationStatus(s.tenantMain, sub.id))
	})
}

func (s *mcpSubscriptionsSuite) TestNameHeaderMustMatchParams() {
	user := s.fresh()
	c := s.session(user)
	for name, opt := range map[string]mcpclient.CallOption{
		"different": mcpclient.NameHeader("user.updated"),
		"missing":   mcpclient.OmitNameHeader(),
	} {
		s.Run(name, func() {
			sub := s.newSub(user, "user.created", nil, mcpreceiver.Echo())
			resp, err := c.Subscribe(s.ctx(), sub.subscribeParams(nil), opt)
			s.Require().NoError(err)
			s.Equal(http.StatusBadRequest, resp.Status, "%s", resp.Raw)
			s.Require().NotNil(resp.Error, "%s", resp.Raw)
			s.Equal(mcpCodeHeader, resp.Error.Code)
			s.Empty(s.m.r.Challenges(sub.path))
			s.Equal(http.StatusNotFound, s.destinationStatus(s.tenantMain, sub.id))
		})
	}
}
