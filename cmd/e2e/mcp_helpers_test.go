package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/mcpevents"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// MCP topics
// =============================================================================

// orderShippedSchema is a second MCP-enabled topic, without validation, so
// it can carry payloads over the 256 KiB envelope limit.
const orderShippedSchema = `{
	"type": "object",
	"properties": {
		"orderId": {"type": "string", "x-mcp-filter": false},
		"carrier": {"type": "string", "enum": ["ups", "fedex"], "description": "Shipping carrier."},
		"weight": {"type": "number"}
	},
	"required": ["orderId"]
}`

// mcpTopicSchemasJSON enables MCP on order.created (the spec's example,
// with the given payload schema) and order.shipped. orderCreatedMCP false
// leaves order.created out of MCP.
func mcpTopicSchemasJSON(orderCreated string, orderCreatedMCP bool) string {
	return `{
		"order.created": {
			"description": "Fires when a new order is placed.",
			"payload_schema": ` + orderCreated + `,
			"validation": "enforce",
			"mcp": {"enabled": ` + strconv.FormatBool(orderCreatedMCP) + `}
		},
		"order.shipped": {
			"description": "Fires when an order ships.",
			"payload_schema": ` + orderShippedSchema + `,
			"mcp": {"enabled": true}
		}
	}`
}

// withMCPTopics adds the MCP topics to a clone of the test topics.
func withMCPTopics(cfg *config.Config, orderCreated string) {
	cfg.Topics = append(slices.Clone(cfg.Topics), "order.created", "order.shipped")
	cfg.TopicsSchemas = config.NewTopicSchemas(mcpTopicSchemasJSON(orderCreated, true))
}

// withMCPTestSettings makes MCP fast enough to test: loopback callbacks,
// second-long lifetimes and retries, a 1s sweep and no verification limits
// in the way.
func withMCPTestSettings(cfg *config.Config) {
	cfg.MCP.CallbackAllowlist = config.StringList{"127.0.0.0/8", "::1/128"}
	cfg.MCP.TTLMin = config.Duration(time.Second)
	cfg.MCP.RetrySchedule = []int{1, 1, 1}
	cfg.MCP.ExpirySweepInterval = config.Duration(time.Second)
	cfg.MCP.VerificationRateLimit = 1000
	cfg.MCP.VerificationFailureLimit = 1000
	cfg.RetryPollBackoffMs = 50
}

// =============================================================================
// Receiver
// =============================================================================

// mcpRequest is one request an MCP callback received.
type mcpRequest struct {
	Header   http.Header
	Body     []byte
	Received time.Time
}

// envelope decodes the request body.
func (r mcpRequest) envelope() mcpEnvelope {
	var e mcpEnvelope
	_ = json.Unmarshal(r.Body, &e)
	return e
}

type mcpEnvelope struct {
	// Type is "verification" or "terminated" for control envelopes, "" for
	// events.
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	EventID   string          `json:"eventId"`
	Name      string          `json:"name"`
	Data      json.RawMessage `json:"data"`
	Error     *struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	} `json:"error"`
}

func isMCPEvent(r mcpRequest) bool        { return r.envelope().EventID != "" }
func isMCPVerification(r mcpRequest) bool { return r.envelope().Type == "verification" }
func isMCPTerminated(r mcpRequest) bool   { return r.envelope().Type == "terminated" }

// mcpScript scripts the answers of one callback path.
type mcpScript struct {
	// statuses answer deliveries and control envelopes in order; then
	// fallback (0: 200).
	statuses []int
	fallback int
	// delay holds every answer but challenges.
	delay time.Duration
	// challengeStatus answers challenges with this status instead of
	// echoing them; wrongChallenge echoes another challenge.
	challengeStatus int
	wrongChallenge  bool
}

// mcpReceiver is an MCP Events callback on plain http 127.0.0.1: it answers
// verification challenges, records every request per path and answers as
// each path's script says (200 by default). It never blocks on the test.
type mcpReceiver struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests map[string][]mcpRequest
	scripts  map[string]*mcpScript
}

func newMCPReceiver() *mcpReceiver {
	r := &mcpReceiver{requests: map[string][]mcpRequest{}, scripts: map[string]*mcpScript{}}
	r.server = httptest.NewServer(http.HandlerFunc(r.serve))
	return r
}

func (r *mcpReceiver) Close() { r.server.Close() }

// newPath returns a fresh callback path and its URL.
func (r *mcpReceiver) newPath() (string, string) {
	path := "/mcp-events/" + idgen.String()
	return path, r.server.URL + path
}

func (r *mcpReceiver) script(path string, s mcpScript) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts[path] = &s
}

func (r *mcpReceiver) serve(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	rec := mcpRequest{Header: req.Header.Clone(), Body: body, Received: time.Now()}
	env := rec.envelope()

	r.mu.Lock()
	r.requests[req.URL.Path] = append(r.requests[req.URL.Path], rec)
	script := r.scripts[req.URL.Path]
	status, delay := http.StatusOK, time.Duration(0)
	var challengeStatus int
	var wrongChallenge bool
	if script != nil {
		challengeStatus, wrongChallenge = script.challengeStatus, script.wrongChallenge
		if env.Type != "verification" {
			delay = script.delay
			switch {
			case len(script.statuses) > 0:
				status, script.statuses = script.statuses[0], script.statuses[1:]
			case script.fallback != 0:
				status = script.fallback
			}
		}
	}
	r.mu.Unlock()

	if env.Type == "verification" {
		if challengeStatus != 0 {
			w.WriteHeader(challengeStatus)
			return
		}
		challenge := env.Challenge
		if wrongChallenge {
			challenge = "not-" + challenge
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": challenge})
		return
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	w.WriteHeader(status)
}

// matching returns the requests of path that match.
func (r *mcpReceiver) matching(path string, match func(mcpRequest) bool) []mcpRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []mcpRequest
	for _, req := range r.requests[path] {
		if match == nil || match(req) {
			out = append(out, req)
		}
	}
	return out
}

// waitFor polls until path has at least n matching requests.
func (r *mcpReceiver) waitFor(t testing.TB, path string, n int, match func(mcpRequest) bool) []mcpRequest {
	t.Helper()
	deadline := time.Now().Add(mockServerPollTimeout)
	var got []mcpRequest
	for time.Now().Before(deadline) {
		if got = r.matching(path, match); len(got) >= n {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d requests on %s (got %d)", n, path, len(got))
	return nil
}

// verifyMCPSignature verifies a request's Standard Webhooks signature with
// one secret.
func verifyMCPSignature(req mcpRequest, secret string) error {
	wh, err := standardwebhooks.NewWebhook(secret)
	if err != nil {
		return err
	}
	return wh.Verify(req.Body, req.Header)
}

// =============================================================================
// Requests
// =============================================================================

// mcpSubscription is an events/subscribe request.
type mcpSubscription struct {
	Principal string
	Name      string
	// Arguments nil leaves them out ({}).
	Arguments map[string]any
	URL       string
	Secret    string
	// Mode "" sends "webhook".
	Mode string
	// TTLMs nil leaves ttlMs out.
	TTLMs any
	// Extra are body fields beside principal and params.
	Extra map[string]any
}

func (m mcpSubscription) params() map[string]any {
	mode := m.Mode
	if mode == "" {
		mode = "webhook"
	}
	params := map[string]any{
		"name":     m.Name,
		"delivery": map[string]any{"mode": mode, "url": m.URL, "secret": m.Secret},
		"cursor":   nil,
	}
	if m.Arguments != nil {
		params["arguments"] = m.Arguments
	}
	if m.TTLMs != nil {
		params["ttlMs"] = m.TTLMs
	}
	return params
}

func (m mcpSubscription) body() map[string]any {
	body := map[string]any{"principal": m.Principal, "params": m.params()}
	for k, v := range m.Extra {
		body[k] = v
	}
	return body
}

// unsubscribeBody is the events/unsubscribe request of the subscription.
func (m mcpSubscription) unsubscribeBody() map[string]any {
	params := map[string]any{"name": m.Name, "delivery": map[string]any{"url": m.URL}}
	if m.Arguments != nil {
		params["arguments"] = m.Arguments
	}
	return map[string]any{"principal": m.Principal, "params": params}
}

// id derives the subscription ID the way the guide does.
func (m mcpSubscription) id(t testing.TB) string {
	t.Helper()
	var args json.RawMessage
	if m.Arguments != nil {
		canonical, err := mcpevents.CanonicalJSON(m.Arguments)
		require.NoError(t, err)
		args = canonical
	}
	return mcpevents.DeriveSubscriptionID(m.Principal, m.URL, m.Name, args)
}

// mcpSubscribeResult is the events/subscribe result.
type mcpSubscribeResult struct {
	ID             string              `json:"id"`
	RefreshBefore  *string             `json:"refreshBefore"`
	Cursor         *string             `json:"cursor"`
	Truncated      bool                `json:"truncated"`
	DeliveryStatus *mcpDeliveryStatusV `json:"deliveryStatus"`
}

type mcpDeliveryStatusV struct {
	Active         bool    `json:"active"`
	LastDeliveryAt *string `json:"lastDeliveryAt"`
	LastError      *string `json:"lastError"`
}

// mcpErrorBody is the 422 body of an MCP failure.
type mcpErrorBody struct {
	MCPError struct {
		Kind    string         `json:"kind"`
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	} `json:"mcp_error"`
}

// mcpAttempt is an attempt as the API lists it.
type mcpAttempt struct {
	ID              string `json:"id"`
	EventID         string `json:"event_id"`
	DestinationID   string `json:"destination_id"`
	DestinationType string `json:"destination_type"`
	Status          string `json:"status"`
	Code            string `json:"code"`
	AttemptNumber   int    `json:"attempt_number"`
	Manual          bool   `json:"manual"`
}

// =============================================================================
// Standalone app (tests that boot Outpost themselves)
// =============================================================================

// standaloneApp is an Outpost run by a test, outside basicSuite.
type standaloneApp struct {
	t      *testing.T
	cfg    config.Config
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
	client *http.Client
}

// prepareStandaloneConfig validates cfg and applies its migrations.
func prepareStandaloneConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	require.NoError(t, cfg.Validate(config.Flags{}))
	configs.ApplyMigrations(t, cfg)
}

// startStandaloneApp boots Outpost and waits until it is healthy.
func startStandaloneApp(t *testing.T, cfg config.Config) *standaloneApp {
	t.Helper()
	prepareStandaloneConfig(t, &cfg)
	ctx, cancel := context.WithCancel(context.Background())
	a := &standaloneApp{t: t, cfg: cfg, cancel: cancel, done: make(chan error, 1), client: &http.Client{Timeout: 30 * time.Second}}
	go func() { a.done <- app.New(&a.cfg).Run(ctx) }()
	t.Cleanup(a.stop)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-a.done:
			t.Fatalf("outpost exited during startup: %v", err)
		default:
		}
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/healthz", cfg.APIPort))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return a
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("outpost did not become healthy on port %d", cfg.APIPort)
	return nil
}

// stop shuts Outpost down and waits for it.
func (a *standaloneApp) stop() {
	a.once.Do(func() {
		a.cancel()
		select {
		case <-a.done:
		case <-time.After(30 * time.Second):
			a.t.Log("outpost did not shut down within 30s")
		}
	})
}

// do sends an admin request to API v2 and returns the status and body.
func (a *standaloneApp) do(method, path string, body any) (int, []byte) {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(a.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://localhost:%d/api/v2%s", a.cfg.APIPort, path), reader)
	require.NoError(a.t, err)
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	require.NoError(a.t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(a.t, err)
	return resp.StatusCode, respBody
}

// createTenant creates a tenant with a random ID.
func (a *standaloneApp) createTenant() string {
	a.t.Helper()
	id := idgen.String()
	status, body := a.do(http.MethodPut, "/tenants/"+id, nil)
	require.Equal(a.t, http.StatusCreated, status, string(body))
	return id
}

// doRawWithAuthBody sends an admin request without a body and returns the
// status and the raw response body.
func (s *basicSuite) doRawWithAuthBody(method, url string) (int, json.RawMessage) {
	s.T().Helper()
	var raw json.RawMessage
	status := s.doRawWithAuth(method, url, s.adminAuth(), nil, &raw)
	return status, raw
}
