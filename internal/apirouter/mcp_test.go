package apirouter_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mcpCallbackURL = "https://receiver.example.com/mcp-events/abc123"

type publicResolver struct{}

func (publicResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

// stubVerifier answers every verification with err and records tenant purges.
type stubVerifier struct {
	mu       sync.Mutex
	verified bool
	err      error
	requests []mcpevents.VerifyRequest
	purged   []string
}

func (v *stubVerifier) Verify(_ context.Context, req mcpevents.VerifyRequest) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.requests = append(v.requests, req)
	return v.err
}

func (v *stubVerifier) Verified(context.Context, string, string, string) (bool, error) {
	return v.verified, nil
}

func (v *stubVerifier) PurgeTenant(_ context.Context, tenantID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.purged = append(v.purged, tenantID)
	return nil
}

func (v *stubVerifier) Requests() []mcpevents.VerifyRequest {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]mcpevents.VerifyRequest(nil), v.requests...)
}

func mcpSecret(b byte) string {
	return mcpevents.SecretPrefix + base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string([]byte{b}), 32)))
}

// newMCPAPITest returns a harness whose registry has the mcp type enabled
// (verifier == nil: disabled).
func newMCPAPITest(t *testing.T, verifier *stubVerifier, extra ...apiTestOption) *apiTest {
	t.Helper()
	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, destregistrydefault.RegisterDefault(registry, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
		AWSEventBridge: &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
	}))
	opts := []apiTestOption{withDestRegistry(registry)}
	if verifier != nil {
		guard := &netguard.Guard{Resolver: publicResolver{}}
		client, err := destmcp.NewHTTPClient(guard, destregistry.HTTPClientConfig{})
		require.NoError(t, err)
		t.Cleanup(client.CloseIdleConnections)
		p, err := destmcp.New(registry.MetadataLoader(), destmcp.Config{
			Client:   client,
			Guard:    guard,
			Verifier: verifier,
			Catalog:  mcpEventsCatalog(t),
		})
		require.NoError(t, err)
		require.NoError(t, registry.RegisterProvider(destmcp.Type, p))
		opts = append(opts, withMCPVerifier(verifier))
	}
	h := newAPITest(t, append(opts, extra...)...)
	require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	return h
}

func mcpDestination(mutate ...func(map[string]any)) map[string]any {
	d := map[string]any{
		"id":          "sub_1",
		"type":        "mcp",
		"topics":      []string{"user.created"},
		"config":      map[string]string{"url": mcpCallbackURL, "principal": "user-1", "arguments": `{"status":"paid"}`},
		"credentials": map[string]string{"secret": mcpSecret(1)},
	}
	for _, m := range mutate {
		m(d)
	}
	return d
}

func responseBody(t *testing.T, resp *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	return body
}

func createMCP(t *testing.T, h *apiTest, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return h.do(h.withAPIKey(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", body)))
}

func patchMCP(t *testing.T, h *apiTest, body map[string]any, jwt bool) *httptest.ResponseRecorder {
	t.Helper()
	req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/sub_1", body)
	if jwt {
		return h.do(h.withJWT(req, "t1"))
	}
	return h.do(h.withAPIKey(req))
}

func requireValidation422(t *testing.T, resp *httptest.ResponseRecorder, want ...string) {
	t.Helper()
	require.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	body := responseBody(t, resp)
	assert.Equal(t, "validation error", body["message"])
	var data []string
	for _, d := range body["data"].([]any) {
		data = append(data, d.(string))
	}
	assert.Equal(t, want, data)
}

// seedMCP stores an mcp destination directly: the destinations API doesn't
// create them.
func seedMCP(t *testing.T, h *apiTest, mutate ...func(*models.Destination)) {
	t.Helper()
	d := df.Any(
		df.WithID("sub_1"), df.WithTenantID("t1"), df.WithType("mcp"),
		df.WithTopics([]string{"user.created"}),
		df.WithConfig(map[string]string{"url": mcpCallbackURL, "principal": "user-1"}),
		df.WithCredentials(map[string]string{"secret": mcpSecret(1)}),
	)
	for _, m := range mutate {
		m(&d)
	}
	require.NoError(t, h.tenantStore.CreateDestination(t.Context(), d))
}

func TestAPI_MCPDestinations(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		t.Run("answers like an unknown type, with MCP on and off", func(t *testing.T) {
			for name, verifier := range map[string]*stubVerifier{"on": {}, "off": nil} {
				h := newMCPAPITest(t, verifier)
				for label, mutate := range map[string]func(map[string]any){
					"valid":         func(map[string]any) {},
					"invalid topic": func(d map[string]any) { d["topics"] = []string{"nope.topic"} },
				} {
					mcpResp := createMCP(t, h, mcpDestination(mutate))
					unknownResp := createMCP(t, h, mcpDestination(mutate, func(d map[string]any) { d["type"] = "nope" }))
					require.Equal(t, http.StatusUnprocessableEntity, mcpResp.Code, name+" "+label+": "+mcpResp.Body.String())
					assert.Equal(t, strings.ReplaceAll(unknownResp.Body.String(), "nope", "mcp"), mcpResp.Body.String(), name+" "+label)
				}

				jwtResp := h.do(h.withJWT(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", mcpDestination()), "t1"))
				assert.Equal(t, http.StatusUnprocessableEntity, jwtResp.Code, name+": a tenant gets the same answer")
				assert.Zero(t, mcpDestinationCount(t, h), name)
			}
		})
	})

	t.Run("Update", func(t *testing.T) {
		t.Run("answers 404 and changes nothing", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{})
			seedMCP(t, h)
			for name, jwt := range map[string]bool{"api key": false, "jwt": true} {
				for _, patch := range []map[string]any{
					{"credentials": map[string]string{"secret": mcpSecret(2)}},
					{"config": map[string]string{"url": "https://other.example.com/hook"}},
					{"topics": []string{"user.updated"}},
					{"disabled_at": time.Now().UTC().Format(time.RFC3339)},
				} {
					resp := patchMCP(t, h, patch, jwt)
					require.Equal(t, http.StatusNotFound, resp.Code, name+": "+resp.Body.String())
				}
			}
			got, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
			require.NoError(t, err)
			assert.Equal(t, mcpCallbackURL, got.Config["url"])
			assert.Equal(t, mcpSecret(1), got.Credentials["secret"])
			assert.Equal(t, models.Topics{"user.created"}, got.Topics)
			assert.Nil(t, got.DisabledAt)
		})
	})

	t.Run("Hidden", func(t *testing.T) {
		for name, verifier := range map[string]*stubVerifier{"on": {}, "off": nil} {
			t.Run("MCP "+name, func(t *testing.T) {
				h := newMCPAPITest(t, verifier)
				seedMCP(t, h)
				require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(
					df.WithID("hook_1"), df.WithTenantID("t1"), df.WithType("webhook"),
					df.WithTopics([]string{"user.created"}),
				)))

				for role, auth := range map[string]func(*http.Request) *http.Request{
					"api key": h.withAPIKey,
					"jwt":     func(r *http.Request) *http.Request { return h.withJWT(r, "t1") },
				} {
					list := h.do(auth(httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)))
					require.Equal(t, http.StatusOK, list.Code, list.Body.String())
					assert.NotContains(t, list.Body.String(), "sub_1", role)
					assert.Contains(t, list.Body.String(), "hook_1", role)

					for _, route := range []struct{ method, path string }{
						{http.MethodGet, "/api/v1/tenants/t1/destinations/sub_1"},
						{http.MethodDelete, "/api/v1/tenants/t1/destinations/sub_1"},
						{http.MethodPut, "/api/v1/tenants/t1/destinations/sub_1/enable"},
						{http.MethodPut, "/api/v1/tenants/t1/destinations/sub_1/disable"},
						{http.MethodGet, "/api/v1/tenants/t1/destinations/sub_1/attempts"},
						{http.MethodGet, "/api/v1/tenants/t1/destinations/sub_1/attempts/att_1"},
					} {
						resp := h.do(auth(httptest.NewRequest(route.method, route.path, nil)))
						assert.Equal(t, http.StatusNotFound, resp.Code, role+" "+route.method+" "+route.path)
					}
				}
				assert.Equal(t, 1, mcpDestinationCount(t, h), "the subscription is still stored")
			})
		}
	})

	t.Run("DestinationTypes", func(t *testing.T) {
		types := func(t *testing.T, resp *httptest.ResponseRecorder) []string {
			t.Helper()
			require.Equal(t, http.StatusOK, resp.Code)
			var list []struct {
				Type string `json:"type"`
			}
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
			var out []string
			for _, m := range list {
				out = append(out, m.Type)
			}
			return out
		}
		for name, verifier := range map[string]*stubVerifier{"on": {}, "off": nil} {
			h := newMCPAPITest(t, verifier)
			for role, auth := range map[string]func(*http.Request) *http.Request{
				"api key": h.withAPIKey,
				"jwt":     func(r *http.Request) *http.Request { return h.withJWT(r, "t1") },
			} {
				list := h.do(auth(httptest.NewRequest(http.MethodGet, "/api/v1/destination-types", nil)))
				assert.NotContains(t, types(t, list), "mcp", name+" "+role)

				one := h.do(auth(httptest.NewRequest(http.MethodGet, "/api/v1/destination-types/mcp", nil)))
				assert.Equal(t, http.StatusNotFound, one.Code, name+" "+role)
			}
		}
	})

	t.Run("tenant delete purges the verification cache", func(t *testing.T) {
		v := &stubVerifier{}
		h := newMCPAPITest(t, v)
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodDelete, "/api/v1/tenants/t1", nil)))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, []string{"t1"}, v.purged)
	})
}
