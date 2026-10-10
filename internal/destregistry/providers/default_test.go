package destregistrydefault_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Signature template validation lives in destwebhook.New, so registration is
// where a bad template fails startup.
func TestRegisterDefault_WebhookSignatureTemplates(t *testing.T) {
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  "v0={{.Body}}", // header templates have no .Body — invalid at render
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
	})
	assert.ErrorContains(t, err, "can't evaluate field Body")
}

// Standard mode swaps the provider's metadata for the entry that carries the
// Standard Webhooks verification instructions.
func TestRegisterDefault_WebhookStandardMetadata(t *testing.T) {
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			MetadataName:             destwebhook.StandardMetadataName,
			HeaderPrefix:             destwebhook.StandardHeaderPrefix,
			SignatureContentTemplate: destwebhook.StandardSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.StandardSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.StandardEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.StandardSigningSecretTmpl,
			SignatureSecretEncoding:  destwebhook.StandardSecretEncoding,
			SignatureSecretPrefix:    destwebhook.StandardSecretPrefix,
		},
		AWSEventBridge: &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
	})
	require.NoError(t, err)

	provider, err := registry.ResolveProvider(&models.Destination{Type: "webhook"})
	require.NoError(t, err)
	instructions := provider.Metadata().Instructions
	assert.Contains(t, instructions, "webhook-signature")
}

func TestRegisterDefault_WebhookCompatSignature(t *testing.T) {
	webhookConfig := func(compat *destwebhook.CompatSignatureConfig) *destregistrydefault.DestWebhookConfig {
		return &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
			Compat:                   compat,
		}
	}

	t.Run("registers with a valid compat signature", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
			Webhook: webhookConfig(&destwebhook.CompatSignatureConfig{
				SignatureHeaderName:      "x-legacy-signature",
				SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
				SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
				SignatureEncoding:        destwebhook.DefaultEncoding,
				SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			}),
			AWSEventBridge: &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
		})
		assert.NoError(t, err)
	})

	t.Run("rejects an invalid compat signature", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		err := destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
			Webhook: webhookConfig(&destwebhook.CompatSignatureConfig{
				SignatureHeaderName:      "x-legacy-signature",
				SignatureContentTemplate: "{{.Nope}}",
				SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
				SignatureEncoding:        destwebhook.DefaultEncoding,
				SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			}),
		})
		assert.ErrorContains(t, err, "compat signature")
	})
}

// fakeVerifier passes every callback.
type fakeVerifier struct{}

func (fakeVerifier) Verify(context.Context, mcpevents.VerifyRequest) error { return nil }
func (fakeVerifier) Verified(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func mcpTestCatalog(t *testing.T) *topicschema.Catalog {
	t.Helper()
	cat, err := topicschema.NewCatalog([]string{"order.created"}, topicschema.Definitions{
		"order.created": {
			PayloadSchema: json.RawMessage(`{"type":"object","properties":{"total":{"type":"number"}}}`),
			MCP:           topicschema.MCPSettings{Enabled: true},
		},
	})
	require.NoError(t, err)
	return cat
}

func mcpSubscription(cat *topicschema.Catalog, callbackURL string) *models.Destination {
	id := mcpevents.DeriveSubscriptionID("user_1", callbackURL, "order.created", json.RawMessage("{}"))
	return &models.Destination{
		ID:       id,
		TenantID: "tenant_1",
		Type:     "mcp",
		Topics:   models.Topics{"order.created"},
		Config: models.Config{
			"url":             callbackURL,
			"subscription_id": id,
			"principal":       "user_1",
			"event":           "order.created",
			"arguments":       "{}",
			"schema_hash":     cat.Snapshot().TopicHash("order.created"),
		},
		Credentials: models.Credentials{"secret": "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))},
	}
}

func mcpEvent() *models.Event {
	return &models.Event{ID: "evt_1", Topic: "order.created", Time: time.Now(), Data: json.RawMessage(`{"total":1}`)}
}

// With no MCP options the provider still registers, last, with safe
// defaults: it validates nothing and refuses non-global addresses.
func TestRegisterDefault_MCPWithoutOptions(t *testing.T) {
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, destregistrydefault.RegisterDefault(registry, defaultOptions("Outpost/test", nil)))

	list := registry.ListProviderMetadata()
	require.NotEmpty(t, list)
	last := list[len(list)-1]
	assert.Equal(t, "mcp", last.Type, "mcp is registered last")
	assert.Equal(t, metadata.CreateModeExternal, last.CreateMode)
	for _, meta := range list[:len(list)-1] {
		assert.Empty(t, meta.CreateMode, meta.Type)
	}

	provider, err := registry.ResolveProvider(&models.Destination{Type: "mcp"})
	require.NoError(t, err)
	timeouter, ok := provider.(destregistry.DeliveryTimeouter)
	require.True(t, ok)
	assert.Equal(t, 10*time.Second, timeouter.DeliveryTimeout())
	bypasser, ok := provider.(destregistry.PublisherCacheBypasser)
	require.True(t, ok)
	assert.True(t, bypasser.BypassPublisherCache())

	// The empty catalog offers no event.
	cat := mcpTestCatalog(t)
	err = registry.ValidateDestination(context.Background(), mcpSubscription(cat, "https://receiver.example.com/hook"))
	var verr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &verr)
	var mcpErr *mcpevents.Error
	require.ErrorAs(t, err, &mcpErr)
	assert.Equal(t, mcpevents.KindNotFound, mcpErr.Kind)

	// The strict guard refuses loopback receivers.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	attempt, err := registry.PublishEvent(context.Background(), mcpSubscription(cat, srv.URL+"/hook"), mcpEvent())
	require.Error(t, err)
	require.NotNil(t, attempt)
	assert.Equal(t, "address_not_allowed", attempt.Code)
	assert.Zero(t, hits.Load())
}

func TestRegisterDefault_MCPOptions(t *testing.T) {
	var gotUA atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA.Store(r.Header.Get("User-Agent"))
	}))
	defer srv.Close()

	allow, _, err := netguard.ParseAllowlist([]string{"localhost"})
	require.NoError(t, err)
	guard := &netguard.Guard{Allowlist: allow}
	client, err := netguard.NewHTTPClient(netguard.ClientConfig{Guard: guard})
	require.NoError(t, err)
	cat := mcpTestCatalog(t)

	newRegistry := func(t *testing.T, mcp *destregistrydefault.DestMCPConfig) destregistry.Registry {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		require.NoError(t, destregistrydefault.RegisterDefault(registry, defaultOptions("Outpost/default", mcp)))
		return registry
	}

	t.Run("subscribe and deliver", func(t *testing.T) {
		registry := newRegistry(t, &destregistrydefault.DestMCPConfig{
			Catalog:     cat,
			Client:      client,
			Guard:       guard,
			HostLimiter: netguard.NewHostLimiter(4),
			Verifier:    fakeVerifier{},
			CodeProfile: mcpevents.CodeProfileSEP3415,
			UserAgent:   "Outpost/mcp",
		})
		dest := mcpSubscription(cat, srv.URL+"/hook")
		require.NoError(t, registry.ValidateDestination(context.Background(), dest))
		require.NoError(t, registry.PreprocessDestination(dest, nil, &destregistry.PreprocessDestinationOpts{}))
		attempt, err := registry.PublishEvent(context.Background(), dest, mcpEvent())
		require.NoError(t, err)
		assert.Equal(t, "200", attempt.Code)
		assert.Equal(t, "Outpost/mcp", gotUA.Load())

		unknown := mcpSubscription(cat, srv.URL+"/hook")
		unknown.Config["event"] = "nope"
		var mcpErr *mcpevents.Error
		require.ErrorAs(t, registry.ValidateDestination(context.Background(), unknown), &mcpErr)
		assert.Equal(t, -32023, mcpErr.Code(), "MCP_ERROR_CODES applies to validation errors")
	})

	t.Run("user agent defaults to the destinations one", func(t *testing.T) {
		registry := newRegistry(t, &destregistrydefault.DestMCPConfig{Catalog: cat, Client: client, Guard: guard})
		_, err := registry.PublishEvent(context.Background(), mcpSubscription(cat, srv.URL+"/hook"), mcpEvent())
		require.NoError(t, err)
		assert.Equal(t, "Outpost/default", gotUA.Load())
	})

	t.Run("no verifier fails closed", func(t *testing.T) {
		var typedNil *mcpevents.Verifier
		for _, v := range []destmcp.Verifier{nil, typedNil} {
			registry := newRegistry(t, &destregistrydefault.DestMCPConfig{Catalog: cat, Client: client, Guard: guard, Verifier: v})
			err := registry.ValidateDestination(context.Background(), mcpSubscription(cat, srv.URL+"/hook"))
			assert.ErrorIs(t, err, destmcp.ErrVerificationUnavailable)
		}
	})

	t.Run("invalid code profile fails registration", func(t *testing.T) {
		registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
		err := destregistrydefault.RegisterDefault(registry, defaultOptions("", &destregistrydefault.DestMCPConfig{CodeProfile: "nope"}))
		assert.ErrorContains(t, err, "error code profile")
	})
}

// defaultOptions are the options the services pass, with mcp set.
func defaultOptions(userAgent string, mcp *destregistrydefault.DestMCPConfig) destregistrydefault.RegisterDefaultDestinationOptions {
	return destregistrydefault.RegisterDefaultDestinationOptions{
		UserAgent: userAgent,
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
		AWSEventBridge: &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
		MCP:            mcp,
	}
}
