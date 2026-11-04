package mcpcheck

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
)

// Options configures Run.
type Options struct {
	ServerURL string
	// Principal is the bearer token of the main subscriber.
	Principal string
	// SecondPrincipal is the other subscriber of the isolation check
	// (default Principal + "-2").
	SecondPrincipal string
	// Event is the event to subscribe to (default: the first listed webhook
	// event with a scalar argument, else the first webhook event).
	Event string
	// Arguments are the main subscription's arguments (nil = none).
	Arguments map[string]any
	// Data and OtherData replace the derived publish payloads: the one that
	// matches the filter and the one that doesn't.
	Data      map[string]any
	OtherData map[string]any
	TTL       time.Duration
	// Receiver is started by the caller; callback URLs come from Register.
	Receiver *mcpreceiver.Receiver
	// Publish triggers an event upstream and returns its event ID ("" if
	// unknown). Nil means wait mode: wait for an upstream event instead,
	// and skip the checks that need to control what is published.
	Publish func(ctx context.Context, event string, data map[string]any) (eventID string, err error)
	// Wait is how long to wait for each expected delivery (default 30 s).
	Wait time.Duration
	// Quiet is the window in which an unwanted delivery would show
	// (default 3 s).
	Quiet      time.Duration
	Logf       func(format string, args ...any)
	HTTPClient *http.Client
}

const (
	clientName = "outpost-mcp-dev"
	// httpProbeURL is a reserved documentation address (RFC 5737): no
	// third party is contacted if the server accepts the http callback.
	httpProbeURL = "http://192.0.2.1/mcp-check"
	httpAccepted = "server accepted an http callback (development exception); SEP requires https"
	maxListPages = 100
	maxBodyBytes = 256 << 10
	probeTimeout = 15 * time.Second
	unknownEvent = "mcp-check.unknown"
)

var order = []string{
	"discover.extension", "list.result", "list.descriptors", "list.pagination", "list.event",
	"auth.subscribe", "auth.unsubscribe", "headers.name-missing", "headers.name-mismatch",
	"subscribe.unknown-event", "subscribe.invalid-secret", "subscribe.https-required",
	"subscribe.challenge-failed", "subscribe.result", "subscribe.ttl-grant", "subscribe.verification",
	"delivery.received", "delivery.data-matches", "delivery.retry", "delivery.name", "delivery.post-json",
	"delivery.headers", "delivery.subscription-id", "delivery.signature", "delivery.webhook-id",
	"delivery.webhook-id-per-message", "delivery.retry-resigns", "delivery.envelope",
	"delivery.type-discriminator", "delivery.control-signed", "delivery.verify-first", "delivery.body-size",
	"refresh.same-id", "refresh.result", "refresh.rotate-secret", "refresh.dual-sign",
	"unsubscribe.result", "unsubscribe.stops-delivery", "unsubscribe.not-found",
	"filter.match", "filter.no-match", "subscribe.isolation",
}

var sections = map[string]string{
	"discover.": "Capability Negotiation", "list.": "Listing", "auth.": "Subscription identity",
	"headers.": "Streamable HTTP: Routing Headers", "subscribe.unknown-event": "Error Handling",
	"subscribe.invalid-secret": "Subscribing", "subscribe.https-required": "Webhook security",
	"subscribe.challenge-failed": "Webhook security", "subscribe.result": "Subscribing",
	"subscribe.ttl-grant": "Subscription TTL", "subscribe.verification": "Endpoint verification",
	"subscribe.isolation": "Subscription identity", "delivery.retry": "Signature scheme",
	"delivery.": "Webhook event delivery", "refresh.": "Subscription identity",
	"unsubscribe.not-found": "Error Handling", "unsubscribe.": "Unsubscribing", "filter.": "Subscribing",
}

func section(id string) string {
	if s, ok := sections[id]; ok {
		return s
	}
	prefix, _, _ := strings.Cut(id, ".")
	return sections[prefix+"."]
}

var publishOnly = []string{
	"delivery.data-matches", "delivery.retry", "refresh.rotate-secret", "refresh.dual-sign",
	"unsubscribe.stops-delivery", "filter.match", "filter.no-match", "subscribe.isolation",
}

var receiverChecks = map[string]string{
	mcpreceiver.CheckPostJSON:         "delivery.post-json",
	mcpreceiver.CheckHeaders:          "delivery.headers",
	mcpreceiver.CheckSubscriptionID:   "delivery.subscription-id",
	mcpreceiver.CheckSignatureFormula: "delivery.signature",
	mcpreceiver.CheckWebhookIDFormat:  "delivery.webhook-id",
	mcpreceiver.CheckWebhookIDPerMsg:  "delivery.webhook-id-per-message",
	mcpreceiver.CheckRetryResigns:     "delivery.retry-resigns",
	mcpreceiver.CheckEnvelopeShape:    "delivery.envelope",
	mcpreceiver.CheckTypeDiscrim:      "delivery.type-discriminator",
	mcpreceiver.CheckControlSigned:    "delivery.control-signed",
	mcpreceiver.CheckVerifyFirst:      "delivery.verify-first",
}

type runner struct {
	o   Options
	ctx context.Context
	rcv *mcpreceiver.Receiver

	c, c2, anon *mcpclient.Client

	results map[string]Result
	blocked string

	event    mcpclient.Event
	fc       FilterCaseResult
	fcErr    error
	mainData map[string]any
	ttlMs    int64

	path, url, subID string
	filterPath       string
	secrets          []string
	delivered        bool
}

// Run executes the checks. It never panics and never exits; a failed
// precondition turns the checks that depend on it into SKIP.
func Run(ctx context.Context, o Options) Report {
	if o.Wait <= 0 {
		o.Wait = 30 * time.Second
	}
	if o.Quiet <= 0 {
		o.Quiet = 3 * time.Second
	}
	if o.TTL <= 0 {
		o.TTL = 10 * time.Minute
	}
	if o.SecondPrincipal == "" {
		o.SecondPrincipal = o.Principal + "-2"
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	mk := func(principal string) *mcpclient.Client {
		return mcpclient.New(o.ServerURL, principal, mcpclient.Stateless(),
			mcpclient.WithClientInfoName(clientName), mcpclient.WithHTTPClient(hc))
	}
	r := &runner{o: o, ctx: ctx, rcv: o.Receiver, results: map[string]Result{}, ttlMs: o.TTL.Milliseconds(),
		c: mk(o.Principal), c2: mk(o.SecondPrincipal), anon: mk("")}
	r.flow()
	return r.report()
}

func (r *runner) publishMode() bool { return r.o.Publish != nil }

// applies reports whether a check belongs to the receiver's profile.
func (r *runner) applies(id string) bool {
	return id != "delivery.verify-first" || r.rcv == nil || r.rcv.Profile() == mcpreceiver.ProfileOutpost
}

func (r *runner) set(id string, st Status, format string, args ...any) {
	if _, done := r.results[id]; done {
		return
	}
	r.results[id] = Result{ID: id, Section: section(id), Status: st, Detail: fmt.Sprintf(format, args...)}
}

func (r *runner) pass(id, format string, args ...any) { r.set(id, Pass, format, args...) }
func (r *runner) fail(id, format string, args ...any) { r.set(id, Fail, format, args...) }
func (r *runner) warn(id, format string, args ...any) { r.set(id, Warn, format, args...) }
func (r *runner) skip(id, format string, args ...any) { r.set(id, Skip, format, args...) }

func (r *runner) skipAll(reason string, ids ...string) {
	for _, id := range ids {
		r.skip(id, "%s", reason)
	}
}

func (r *runner) report() Report {
	if r.blocked == "" {
		r.blocked = "not run"
	}
	var out Report
	for _, id := range order {
		if !r.applies(id) {
			continue
		}
		res, ok := r.results[id]
		if !ok {
			res = Result{ID: id, Section: section(id), Status: Skip, Detail: r.blocked}
		}
		out = append(out, res)
	}
	return out
}

func (r *runner) flow() {
	r.discover()
	if !r.list() {
		return
	}
	if !r.publishMode() {
		r.skipAll(publishOnlyDetail, publishOnly...)
	}
	r.deriveCases()
	r.probes()
	if !r.mainSubscribe() {
		return
	}
	r.deliveries()
	r.refresh()
	r.unsubscribe()
	r.filterPhase()
	r.receiverRows()
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func (r *runner) params(url, secret string, args map[string]any) mcpclient.SubscribeParams {
	return mcpclient.SubscribeParams{Name: r.event.Name, Arguments: args, URL: url, Secret: secret, TTLMs: r.ttlMs}
}
