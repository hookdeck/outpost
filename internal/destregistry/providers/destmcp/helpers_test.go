package destmcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/stretchr/testify/require"
)

const (
	topicOrderCreated = "order.created"
	topicOrderShipped = "order.shipped"
	topicAudit        = "audit.logged" // schema-less, not MCP-enabled
	testTenantID      = "tenant_1"
	testPrincipal     = "user_8f2c"
	publicHost        = "receiver.example.com"
	publicURL         = "https://receiver.example.com/mcp-events/abc123"
)

var orderSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"orderId": {"type": "string", "x-mcp-filter": false},
		"total": {"type": "number", "description": "Order total."},
		"currency": {"type": "string"}
	}
}`)

func newCatalog(t *testing.T) *topicschema.Catalog {
	t.Helper()
	cat, err := topicschema.NewCatalog([]string{topicOrderCreated, topicOrderShipped, topicAudit}, topicschema.Definitions{
		topicOrderCreated: {PayloadSchema: orderSchema, MCP: topicschema.MCPSettings{Enabled: true}},
		topicOrderShipped: {PayloadSchema: orderSchema, MCP: topicschema.MCPSettings{Enabled: true}},
	})
	require.NoError(t, err)
	return cat
}

// testSecret returns a whsec_ secret of 32 bytes of b.
func testSecret(b byte) string {
	return mcpevents.SecretPrefix + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
}

func secretKey(t *testing.T, secret string) []byte {
	t.Helper()
	key, err := mcpevents.DecodeSecret(secret)
	require.NoError(t, err)
	return key
}

// fakeResolver answers from a fixed table; names are looked up rooted.
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
	errs    map[string]error
	calls   []string
}

func (r *fakeResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, host)
	if err, ok := r.errs[host]; ok {
		return nil, err
	}
	if addrs, ok := r.answers[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func (r *fakeResolver) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func newResolver() *fakeResolver {
	return &fakeResolver{
		answers: map[string][]netip.Addr{
			publicHost + ".":            {netip.MustParseAddr("93.184.216.34")},
			"internal.example.com.":     {netip.MustParseAddr("10.0.0.1")},
			"mixed.example.com.":        {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("192.168.1.10")},
			"metadata.example.com.":     {netip.MustParseAddr("169.254.169.254")},
			"mapped.example.com.":       {netip.MustParseAddr("::ffff:127.0.0.1")},
			"public-v6.example.com.":    {netip.MustParseAddr("2606:4700::1111")},
			"loopback-v6.example.com.":  {netip.MustParseAddr("::1")},
			"unique-local.example.com.": {netip.MustParseAddr("fd00::1")},
		},
		errs: map[string]error{
			"servfail.example.com.": &net.DNSError{Err: "server misbehaving", Name: "servfail.example.com.", IsTemporary: true},
			"timeout.example.com.":  &net.DNSError{Err: "i/o timeout", Name: "timeout.example.com.", IsTimeout: true},
		},
	}
}

// fakeVerifier records calls and answers with fixed results.
type fakeVerifier struct {
	mu          sync.Mutex
	verified    bool
	verifiedErr error
	verifyErr   error
	verifiedReq [][3]string
	verifyReqs  []mcpevents.VerifyRequest
}

func (v *fakeVerifier) Verify(ctx context.Context, req mcpevents.VerifyRequest) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.verifyReqs = append(v.verifyReqs, req)
	return v.verifyErr
}

func (v *fakeVerifier) Verified(ctx context.Context, tenantID, principal, rawURL string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.verifiedReq = append(v.verifiedReq, [3]string{tenantID, principal, rawURL})
	return v.verified, v.verifiedErr
}

func (v *fakeVerifier) VerifyCalls() []mcpevents.VerifyRequest {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]mcpevents.VerifyRequest(nil), v.verifyReqs...)
}

func (v *fakeVerifier) VerifiedCalls() [][3]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([][3]string(nil), v.verifiedReq...)
}

type providerOption func(*destmcp.Config)

func newProvider(t *testing.T, opts ...providerOption) *destmcp.Provider {
	t.Helper()
	cfg := destmcp.Config{
		Catalog:  newCatalog(t),
		Guard:    &netguard.Guard{Resolver: newResolver()},
		Verifier: &fakeVerifier{},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	p, err := destmcp.New(metadata.NewMetadataLoader(""), cfg)
	require.NoError(t, err)
	return p
}

// newSubscription returns a destination as the subscribe endpoint builds
// it, valid for p.
func newSubscription(t *testing.T, p *destmcp.Provider, mutate ...func(*models.Destination)) *models.Destination {
	t.Helper()
	d := subscriptionFor(p, testPrincipal, publicURL, topicOrderCreated, `{"currency":"USD","total":{"$gte":100}}`, testSecret(1))
	for _, m := range mutate {
		m(d)
	}
	return d
}

func subscriptionFor(p *destmcp.Provider, principal, callbackURL, event, args, secret string) *models.Destination {
	id := mcpevents.DeriveSubscriptionID(principal, callbackURL, event, json.RawMessage(args))
	return &models.Destination{
		ID:       id,
		TenantID: testTenantID,
		Type:     destmcp.Type,
		Topics:   models.Topics{event},
		Config: models.Config{
			destmcp.ConfigURL:            callbackURL,
			destmcp.ConfigSubscriptionID: id,
			destmcp.ConfigPrincipal:      principal,
			destmcp.ConfigEvent:          event,
			destmcp.ConfigArguments:      args,
			destmcp.ConfigSchemaHash:     p.SchemaHash(event),
		},
		Credentials: models.Credentials{destmcp.CredentialSecret: secret},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// rederive recomputes the derived ID after a test changed the key.
func rederive(d *models.Destination) {
	id := mcpevents.DeriveSubscriptionID(d.Config[destmcp.ConfigPrincipal], d.Config[destmcp.ConfigURL], d.Config[destmcp.ConfigEvent], json.RawMessage(d.Config[destmcp.ConfigArguments]))
	d.ID = id
	d.Config[destmcp.ConfigSubscriptionID] = id
}

// requireValidationError asserts err is an *ErrDestinationValidation with one
// detail, and returns its mcp_error cause (nil when there is none).
func requireValidationError(t *testing.T, err error, field, typ string) *mcpevents.Error {
	t.Helper()
	require.Error(t, err)
	var verr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &verr)
	require.Equal(t, []destregistry.ValidationErrorDetail{{Field: field, Type: typ}}, verr.Errors)
	var mcpErr *mcpevents.Error
	if errors.As(verr.Cause, &mcpErr) {
		return mcpErr
	}
	return nil
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// loopbackClient is a netguard client that may reach 127.0.0.0/8 over http,
// as an allowlisted local receiver.
func loopbackGuard(t *testing.T) *netguard.Guard {
	t.Helper()
	allow, _, err := netguard.ParseAllowlist([]string{"localhost"})
	require.NoError(t, err)
	return &netguard.Guard{Allowlist: allow}
}

func newClient(t *testing.T, cfg netguard.ClientConfig) *http.Client {
	t.Helper()
	c, err := netguard.NewHTTPClient(cfg)
	require.NoError(t, err)
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func hasString(list []string, s string) bool {
	for _, v := range list {
		if strings.Contains(v, s) {
			return true
		}
	}
	return false
}
