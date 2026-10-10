package apirouter_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/deliverystatus"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Topic catalog
// ---------------------------------------------------------------------------

// mcpTopics are the TOPICS of the MCP tests: two MCP-enabled topics and one
// without MCP.
var mcpTopics = []string{"order.created", "user.created", "order.shipped"}

const orderSchema = `{
	"type": "object",
	"properties": {
		"order_id": {"type": "string", "x-mcp-filter": false},
		"total": {"type": "number", "description": "Order total."},
		"currency": {"type": "string"}
	}
}`

func mcpCatalog(t *testing.T) *topicschema.Catalog {
	t.Helper()
	catalog, err := topicschema.NewCatalog(mcpTopics, topicschema.Definitions{
		"order.created": {Description: "A new order.", PayloadSchema: json.RawMessage(orderSchema), MCP: topicschema.MCPSettings{Enabled: true}},
		"order.shipped": {PayloadSchema: json.RawMessage(orderSchema), MCP: topicschema.MCPSettings{Enabled: true}},
		"user.created":  {PayloadSchema: json.RawMessage(`{"type":"object"}`)},
	})
	require.NoError(t, err)
	require.True(t, catalog.MCPEnabled())
	return catalog
}

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

// mcpInstructionsTemplate is the fake mcp provider's instructions template,
// a Go text/template like the real one.
const mcpInstructionsTemplate = "Connect {{if .ServerURL}}{{.ServerURL}}{{else}}your MCP server{{end}} to your agent.\n\n{{range .Topics}}- {{.}}\n{{end}}"

// fakeMCPProvider stands in for the mcp provider: it validates like it
// (catalog, arguments, secret) with a scriptable verification, and rotates
// secrets on Preprocess.
type fakeMCPProvider struct {
	catalog *topicschema.Catalog
	meta    *metadata.ProviderMetadata

	mu sync.Mutex
	// verify runs last in Validate, as the verification challenge does.
	verify      func(ctx context.Context, d *models.Destination) error
	validations int
}

func newFakeMCPProvider(catalog *topicschema.Catalog) *fakeMCPProvider {
	return &fakeMCPProvider{
		catalog: catalog,
		meta: &metadata.ProviderMetadata{
			Type:             models.DestinationTypeMCP,
			Label:            "MCP",
			Description:      "MCP Events subscriptions",
			Instructions:     mcpInstructionsTemplate,
			CredentialFields: []metadata.FieldSchema{{Type: "text", Key: "secret", Sensitive: true}},
		},
	}
}

func (p *fakeMCPProvider) setVerify(verify func(ctx context.Context, d *models.Destination) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.verify = verify
}

func (p *fakeMCPProvider) validationCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.validations
}

func mcpValidationErr(cause *mcpevents.Error) error {
	return &destregistry.ErrDestinationValidation{
		Errors: []destregistry.ValidationErrorDetail{{Field: "params", Type: "invalid"}},
		Cause:  cause,
	}
}

func (p *fakeMCPProvider) Validate(ctx context.Context, d *models.Destination) error {
	p.mu.Lock()
	p.validations++
	verify := p.verify
	p.mu.Unlock()

	event := d.Config["event"]
	if _, ok := p.catalog.MCPEvent(event); !ok {
		return mcpValidationErr(mcpevents.NotFound(mcpevents.NotFoundEvent))
	}
	if errs := p.catalog.ValidateArguments(event, []byte(d.Config["arguments"])); len(errs) > 0 {
		return mcpValidationErr(mcpevents.InvalidParamsWithErrors(mcpevents.FieldArguments, mcpevents.ReasonInvalid, errs))
	}
	if _, err := mcpevents.DecodeSecret(d.Credentials["secret"]); err != nil {
		return mcpValidationErr(mcpevents.InvalidParams(mcpevents.FieldDeliverySecret, mcpevents.ReasonInvalidSecret))
	}
	if verify != nil {
		return verify(ctx, d)
	}
	return nil
}

func (p *fakeMCPProvider) CreatePublisher(context.Context, *models.Destination) (destregistry.Publisher, error) {
	return nil, errors.New("not supported")
}

func (p *fakeMCPProvider) Metadata() *metadata.ProviderMetadata { return p.meta }

func (p *fakeMCPProvider) ObfuscateDestination(d *models.Destination) *models.Destination {
	result := *d
	result.Credentials = make(map[string]string, len(d.Credentials))
	for k, v := range d.Credentials {
		if k == "secret" || k == "previous_secret" {
			v = destregistry.ObfuscateValue(v)
		}
		result.Credentials[k] = v
	}
	return &result
}

func (p *fakeMCPProvider) ComputeTarget(d *models.Destination) destregistry.DestinationTarget {
	return destregistry.DestinationTarget{Target: d.Config["url"]}
}

func (p *fakeMCPProvider) Preprocess(d *models.Destination, orig *models.Destination, _ *destregistry.PreprocessDestinationOpts) error {
	if orig == nil {
		return nil
	}
	if orig.Credentials["secret"] != d.Credentials["secret"] {
		d.Credentials["previous_secret"] = orig.Credentials["secret"]
		d.Credentials["previous_secret_invalid_at"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		return nil
	}
	for _, k := range []string{"previous_secret", "previous_secret_invalid_at"} {
		if v := orig.Credentials[k]; v != "" {
			d.Credentials[k] = v
		}
	}
	return nil
}

// fakeWebhookProvider is a minimal form-created type.
type fakeWebhookProvider struct{}

func (fakeWebhookProvider) Validate(context.Context, *models.Destination) error { return nil }
func (fakeWebhookProvider) CreatePublisher(context.Context, *models.Destination) (destregistry.Publisher, error) {
	return nil, errors.New("not supported")
}
func (fakeWebhookProvider) Metadata() *metadata.ProviderMetadata {
	return &metadata.ProviderMetadata{
		Type:         "webhook",
		Label:        "Webhook",
		Instructions: "Point it at {{MCP_SERVER_URL}}.",
		ConfigFields: []metadata.FieldSchema{{Type: "text", Key: "url", Required: true}},
	}
}
func (fakeWebhookProvider) ObfuscateDestination(d *models.Destination) *models.Destination {
	result := *d
	return &result
}
func (fakeWebhookProvider) ComputeTarget(d *models.Destination) destregistry.DestinationTarget {
	return destregistry.DestinationTarget{Target: d.Config["url"]}
}
func (fakeWebhookProvider) Preprocess(*models.Destination, *models.Destination, *destregistry.PreprocessDestinationOpts) error {
	return nil
}

// newWebhookOnlyRegistry registers a webhook type and no mcp provider.
func newWebhookOnlyRegistry(t *testing.T) destregistry.Registry {
	t.Helper()
	reg := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, reg.RegisterProvider("webhook", fakeWebhookProvider{}))
	return reg
}

// newMCPRegistry registers a webhook type and the mcp provider.
func newMCPRegistry(t *testing.T, mcp destregistry.Provider) destregistry.Registry {
	t.Helper()
	reg := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, reg.RegisterProvider("webhook", fakeWebhookProvider{}))
	require.NoError(t, reg.RegisterProvider(models.DestinationTypeMCP, mcp))
	return reg
}

// ---------------------------------------------------------------------------
// MCP deps fakes
// ---------------------------------------------------------------------------

type fakeNotifier struct {
	mu           sync.Mutex
	full         bool
	terminations []mcpevents.Termination
}

func (n *fakeNotifier) Enqueue(t mcpevents.Termination) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.full {
		return false
	}
	n.terminations = append(n.terminations, t)
	return true
}

func (n *fakeNotifier) sent() []mcpevents.Termination {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]mcpevents.Termination(nil), n.terminations...)
}

type recordingEmitter struct {
	mu     sync.Mutex
	events []opevents.Event
}

func (e *recordingEmitter) Emit(_ context.Context, ev opevents.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return nil
}

func (e *recordingEmitter) byTopic(topic string) []opevents.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var events []opevents.Event
	for _, ev := range e.events {
		if ev.Topic == topic {
			events = append(events, ev)
		}
	}
	return events
}

type resumeCall struct {
	tenantID, destinationID, key string
}

type fakeResumer struct {
	mu    sync.Mutex
	calls []resumeCall
}

func (r *fakeResumer) Resume(_ context.Context, tenantID, destinationID, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, resumeCall{tenantID, destinationID, key})
}

func (r *fakeResumer) resumed() []resumeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]resumeCall(nil), r.calls...)
}

// fakeAlertResetter records resets and whether the destination was still
// disabled when each ran.
type fakeAlertResetter struct {
	store tenantstore.TenantStore

	mu            sync.Mutex
	resets        []string
	disabledAtRun []bool
	// generations records ResetDestination calls.
	generations []string
}

func (r *fakeAlertResetter) ResetDestination(_ context.Context, _, destinationID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generations = append(r.generations, destinationID)
	return nil
}

func (r *fakeAlertResetter) generationResets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.generations...)
}

func (r *fakeAlertResetter) ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error {
	disabled := false
	if d, err := r.store.RetrieveDestination(ctx, tenantID, destinationID); err == nil && d != nil {
		disabled = d.DisabledAt != nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resets = append(r.resets, destinationID)
	r.disabledAtRun = append(r.disabledAtRun, disabled)
	return nil
}

func (r *fakeAlertResetter) calls() ([]string, []bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.resets...), append([]bool(nil), r.disabledAtRun...)
}

type fakeStatusReader struct {
	mu     sync.Mutex
	status *deliverystatus.Status
	err    error
}

func (r *fakeStatusReader) set(status *deliverystatus.Status, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status, r.err = status, err
}

func (r *fakeStatusReader) GetAttemptStatus(context.Context, string, string) (*deliverystatus.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, r.err
}

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

type mcpTest struct {
	*apiTest
	t        *testing.T
	catalog  *topicschema.Catalog
	provider *fakeMCPProvider
	notifier *fakeNotifier
	emitter  *recordingEmitter
	resumer  *fakeResumer
	alerts   *fakeAlertResetter
	status   *fakeStatusReader
	deps     *apirouter.MCPDeps
}

type mcpTestConfig struct {
	typeLimit       int
	principalLimit  int
	profile         mcpevents.CodeProfile
	allowNoExpiry   bool
	serverURL       string
	catalog         *topicschema.Catalog
	withoutMCPDeps  bool
	extraAPIOptions []apiTestOption
	mutateDeps      func(*apirouter.MCPDeps)
	wrapStore       func(tenantstore.TenantStore) tenantstore.TenantStore
}

type mcpTestOption func(*mcpTestConfig)

func withMCPLimits(perTenant, perPrincipal int) mcpTestOption {
	return func(c *mcpTestConfig) { c.typeLimit, c.principalLimit = perTenant, perPrincipal }
}

func withMCPProfile(p mcpevents.CodeProfile) mcpTestOption {
	return func(c *mcpTestConfig) { c.profile = p }
}

func withMCPAllowNoExpiry() mcpTestOption {
	return func(c *mcpTestConfig) { c.allowNoExpiry = true }
}

func withMCPServerURL(u string) mcpTestOption {
	return func(c *mcpTestConfig) { c.serverURL = u }
}

func withMCPCatalog(catalog *topicschema.Catalog) mcpTestOption {
	return func(c *mcpTestConfig) { c.catalog = catalog }
}

func withoutMCPDeps() mcpTestOption {
	return func(c *mcpTestConfig) { c.withoutMCPDeps = true }
}

func withMCPAPIOptions(opts ...apiTestOption) mcpTestOption {
	return func(c *mcpTestConfig) { c.extraAPIOptions = append(c.extraAPIOptions, opts...) }
}

// withMCPStoreWrapper puts a wrapper around the memory store the router
// uses. The fakes keep the unwrapped store.
func withMCPStoreWrapper(wrap func(tenantstore.TenantStore) tenantstore.TenantStore) mcpTestOption {
	return func(c *mcpTestConfig) { c.wrapStore = wrap }
}

// withMCPDeps changes the MCP deps before the router is built.
func withMCPDeps(mutate func(*apirouter.MCPDeps)) mcpTestOption {
	return func(c *mcpTestConfig) { c.mutateDeps = mutate }
}

const mcpTenant = "t1"

// newMCPTest builds a router with an MCP-enabled catalog, the fake mcp
// provider, a memory store with MCP limits and fakes for every MCP
// dependency. The tenant mcpTenant exists.
func newMCPTest(t *testing.T, opts ...mcpTestOption) *mcpTest {
	t.Helper()
	cfg := mcpTestConfig{typeLimit: 100, principalLimit: 20}
	for _, o := range opts {
		o(&cfg)
	}
	catalog := cfg.catalog
	if catalog == nil {
		catalog = mcpCatalog(t)
	}
	store := tenantstore.NewMemTenantStore(
		tenantstore.MemWithTypeLimits(map[string]int{models.DestinationTypeMCP: cfg.typeLimit}),
		tenantstore.MemWithIndexedTypes(models.DestinationTypeMCP),
	)
	m := &mcpTest{
		t:        t,
		catalog:  catalog,
		provider: newFakeMCPProvider(catalog),
		notifier: &fakeNotifier{},
		emitter:  &recordingEmitter{},
		resumer:  &fakeResumer{},
		alerts:   &fakeAlertResetter{store: store},
		status:   &fakeStatusReader{},
	}
	m.deps = &apirouter.MCPDeps{
		Notifier:      m.notifier,
		Emitter:       m.emitter,
		Resumer:       m.resumer,
		AlertResetter: m.alerts,
		StatusReader:  m.status,
		Config: apirouter.MCPHandlerConfig{
			TTL: mcpevents.TTLConfig{
				Default:       time.Hour,
				Min:           5 * time.Minute,
				Max:           24 * time.Hour,
				AllowNoExpiry: cfg.allowNoExpiry,
			},
			CodeProfile:                  cfg.profile,
			MaxSubscriptionsPerPrincipal: cfg.principalLimit,
			ServerURL:                    cfg.serverURL,
		},
	}
	if cfg.mutateDeps != nil {
		cfg.mutateDeps(m.deps)
	}
	deps := m.deps
	if cfg.withoutMCPDeps {
		deps = nil
	}
	var routerStore tenantstore.TenantStore = store
	if cfg.wrapStore != nil {
		routerStore = cfg.wrapStore(store)
	}
	apiOpts := append([]apiTestOption{
		withTenantStore(routerStore),
		withTopics(mcpTopics),
		withTopicCatalog(catalog),
		withDestRegistry(newMCPRegistry(t, m.provider)),
		withMCP(deps),
	}, cfg.extraAPIOptions...)
	m.apiTest = newAPITest(t, apiOpts...)
	require.NoError(t, store.UpsertTenant(t.Context(), tf.Any(tf.WithID(mcpTenant))))
	return m
}

// ---------------------------------------------------------------------------
// Requests
// ---------------------------------------------------------------------------

// testSecret returns a valid whsec_ secret derived from seed.
func testSecret(seed byte) string {
	return "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

const testCallbackURL = "https://receiver.example.com/hooks/abc"

type subscribeOpt func(body map[string]any)

func withArguments(args map[string]any) subscribeOpt {
	return func(b map[string]any) { b["params"].(map[string]any)["arguments"] = args }
}

func withSecret(secret string) subscribeOpt {
	return func(b map[string]any) {
		b["params"].(map[string]any)["delivery"].(map[string]any)["secret"] = secret
	}
}

func withURL(u string) subscribeOpt {
	return func(b map[string]any) {
		b["params"].(map[string]any)["delivery"].(map[string]any)["url"] = u
	}
}

func withParam(key string, value any) subscribeOpt {
	return func(b map[string]any) { b["params"].(map[string]any)[key] = value }
}

func withBodyField(key string, value any) subscribeOpt {
	return func(b map[string]any) { b[key] = value }
}

// subscribeBody is a valid subscribe request for principal and event name.
func subscribeBody(principal, name string, opts ...subscribeOpt) map[string]any {
	body := map[string]any{
		"principal": principal,
		"params": map[string]any{
			"name":      name,
			"arguments": map[string]any{"total": map[string]any{"$gte": 100}},
			"delivery": map[string]any{
				"mode":   "webhook",
				"url":    testCallbackURL,
				"secret": testSecret(1),
			},
			"cursor": nil,
		},
	}
	for _, o := range opts {
		o(body)
	}
	return body
}

// subscriptionID derives the ID of a subscribe body.
func subscriptionID(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(body["params"])
	require.NoError(t, err)
	params, err := mcpevents.ParseSubscribeParams(raw)
	require.NoError(t, err)
	return mcpevents.DeriveSubscriptionID(body["principal"].(string), params.Delivery.URLString, params.Name, params.Arguments)
}

func (m *mcpTest) subscribe(body any) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.do(m.withAPIKey(m.jsonReq(http.MethodPut, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions", body)))
}

// mustSubscribe subscribes and returns the result.
func (m *mcpTest) mustSubscribe(body any) apirouter.MCPSubscribeResult {
	m.t.Helper()
	resp := m.subscribe(body)
	require.Equal(m.t, http.StatusOK, resp.Code, resp.Body.String())
	var result apirouter.MCPSubscribeResult
	require.NoError(m.t, json.Unmarshal(resp.Body.Bytes(), &result))
	return result
}

func (m *mcpTest) unsubscribe(tenantID string, body any) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.do(m.withAPIKey(m.jsonReq(http.MethodPost, "/api/v2/tenants/"+tenantID+"/mcp/subscriptions/unsubscribe", body)))
}

// destination returns the live destination of mcpTenant, nil when it doesn't
// exist or was deleted.
func (m *mcpTest) destination(id string) *models.Destination {
	m.t.Helper()
	d, err := m.tenantStore.RetrieveDestination(m.t.Context(), mcpTenant, id)
	if errors.Is(err, tenantstore.ErrDestinationDeleted) {
		return nil
	}
	require.NoError(m.t, err)
	return d
}

// putDestination stores d as is, under mcpTenant.
func (m *mcpTest) putDestination(d models.Destination) {
	m.t.Helper()
	d.TenantID = mcpTenant
	require.NoError(m.t, m.tenantStore.UpsertDestination(m.t.Context(), d))
}

func listAll(tenantID string) tenantstore.ListDestinationRequest {
	return tenantstore.ListDestinationRequest{TenantID: tenantID}
}

// mcpDestination is a stored mcp subscription with the given ID.
func mcpDestination(id, principal, event string) models.Destination {
	now := time.Now()
	expiresAt := now.Add(time.Hour)
	return models.Destination{
		ID:     id,
		Type:   models.DestinationTypeMCP,
		Topics: models.Topics{event},
		Config: models.Config{
			"url":             testCallbackURL,
			"subscription_id": id,
			"principal":       principal,
			"event":           event,
			"arguments":       `{}`,
		},
		Credentials: models.Credentials{"secret": testSecret(1)},
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   &expiresAt,
	}
}

// ---------------------------------------------------------------------------
// Assertions
// ---------------------------------------------------------------------------

type mcpErrorBody struct {
	MCPError struct {
		Kind    string         `json:"kind"`
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	} `json:"mcp_error"`
}

// requireMCPError asserts a 422 mcp_error of the kind with exactly data.
func requireMCPError(t *testing.T, resp *httptest.ResponseRecorder, kind string, code int, data map[string]any) {
	t.Helper()
	require.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, []string{"mcp_error"}, keys(body), "the body holds mcp_error only")

	var parsed mcpErrorBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &parsed))
	assert.Equal(t, kind, parsed.MCPError.Kind)
	assert.Equal(t, code, parsed.MCPError.Code)
	assert.NotEmpty(t, parsed.MCPError.Message)
	assert.Equal(t, data, parsed.MCPError.Data)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range maps.Keys(m) {
		out = append(out, k)
	}
	return out
}
