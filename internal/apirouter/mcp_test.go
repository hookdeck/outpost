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
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
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

// seedMCP stores an mcp destination directly: the destinations API no longer
// creates them.
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
		t.Run("is refused for every caller and points to the subscribe route", func(t *testing.T) {
			v := &stubVerifier{}
			h := newMCPAPITest(t, v)
			for name, resp := range map[string]*httptest.ResponseRecorder{
				"api key": createMCP(t, h, mcpDestination()),
				"jwt":     h.do(h.withJWT(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", mcpDestination()), "t1")),
			} {
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code, name+": "+resp.Body.String())
				assert.Contains(t, responseBody(t, resp)["message"], "POST /tenants/t1/mcp/events/subscribe", name)
			}
			assert.Empty(t, v.Requests(), "nothing is sent")
			assert.Zero(t, mcpDestinationCount(t, h))
		})

		t.Run("expires_at on a webhook returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			payload := validDestination()
			payload["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

			resp := createMCP(t, h, payload)

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			assert.Contains(t, resp.Body.String(), "expires_at is not supported for this destination type")
		})

		t.Run("disabled: rejected like an unknown type", func(t *testing.T) {
			h := newMCPAPITest(t, nil)
			mcpResp := createMCP(t, h, mcpDestination())
			unknownResp := createMCP(t, h, mcpDestination(func(d map[string]any) { d["type"] = "nope" }))
			require.Equal(t, http.StatusUnprocessableEntity, mcpResp.Code)
			assert.Equal(t, strings.ReplaceAll(unknownResp.Body.String(), "nope", "mcp"), mcpResp.Body.String())

			tenantResp := h.do(h.withJWT(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", mcpDestination()), "t1"))
			assert.Equal(t, http.StatusUnprocessableEntity, tenantResp.Code, "a tenant gets the unknown-type answer too")
		})
	})

	t.Run("Update", func(t *testing.T) {
		setup := func(t *testing.T) *apiTest {
			h := newMCPAPITest(t, &stubVerifier{})
			seedMCP(t, h)
			return h
		}

		t.Run("is refused for every caller and changes nothing", func(t *testing.T) {
			h := setup(t)
			for name, jwt := range map[string]bool{"api key": false, "jwt": true} {
				for _, patch := range []map[string]any{
					{"credentials": map[string]string{"secret": mcpSecret(2)}},
					{"config": map[string]string{"url": "https://other.example.com/hook"}},
					{"topics": []string{"user.updated"}},
					{"expires_at": nil},
				} {
					resp := patchMCP(t, h, patch, jwt)
					require.Equal(t, http.StatusUnprocessableEntity, resp.Code, name+": "+resp.Body.String())
					assert.Contains(t, responseBody(t, resp)["message"], "POST /tenants/t1/mcp/events/subscribe", name)
				}
			}
			got, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
			require.NoError(t, err)
			assert.Equal(t, mcpCallbackURL, got.Config["url"])
			assert.Equal(t, mcpSecret(1), got.Credentials["secret"])
			assert.Equal(t, models.Topics{"user.created"}, got.Topics)
		})

		t.Run("expires_at on a webhook returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			require.Equal(t, http.StatusCreated, createMCP(t, h, map[string]any{
				"id": "sub_1", "type": "webhook", "topics": []string{"user.created"}, "config": map[string]string{"url": "https://example.com/hook"},
			}).Code)

			resp := patchMCP(t, h, map[string]any{"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, false)

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			assert.Contains(t, resp.Body.String(), "expires_at is not supported for this destination type")
		})
	})

	t.Run("Read and delete", func(t *testing.T) {
		t.Run("get and list show the expiry", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{})
			expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			seedMCP(t, h, func(d *models.Destination) { d.ExpiresAt = &expires })

			get := h.do(h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/sub_1", nil)))
			require.Equal(t, http.StatusOK, get.Code, get.Body.String())
			body := responseBody(t, get)
			assert.Equal(t, expires.Format(time.RFC3339), body["expires_at"])
			assert.Equal(t, mcpCallbackURL, body["target"])
			assert.NotContains(t, get.Body.String(), mcpSecret(1), "the secret is masked")

			list := h.do(h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)))
			require.Equal(t, http.StatusOK, list.Code, list.Body.String())
			var items []map[string]any
			require.NoError(t, json.Unmarshal(list.Body.Bytes(), &items))
			require.Len(t, items, 1)
			assert.Equal(t, expires.Format(time.RFC3339), items[0]["expires_at"])
		})

		t.Run("the api key and a tenant jwt can delete", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{})
			seedMCP(t, h)
			seedMCP(t, h, func(d *models.Destination) { d.ID = "sub_2" })

			resp := h.do(h.withJWT(h.jsonReq(http.MethodDelete, "/api/v1/tenants/t1/destinations/sub_1", nil), "t1"))
			require.Equal(t, http.StatusOK, resp.Code)
			resp = h.do(h.withAPIKey(h.jsonReq(http.MethodDelete, "/api/v1/tenants/t1/destinations/sub_2", nil)))
			require.Equal(t, http.StatusOK, resp.Code)
			assert.Zero(t, mcpDestinationCount(t, h))
		})
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
		h := newMCPAPITest(t, &stubVerifier{})

		list := h.do(h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/destination-types", nil)))
		assert.Contains(t, types(t, list), "mcp")
		list = h.do(h.withJWT(httptest.NewRequest(http.MethodGet, "/api/v1/destination-types", nil), "t1"))
		assert.NotContains(t, types(t, list), "mcp")

		one := h.do(h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/destination-types/mcp", nil)))
		assert.Equal(t, http.StatusOK, one.Code)
		one = h.do(h.withJWT(httptest.NewRequest(http.MethodGet, "/api/v1/destination-types/mcp", nil), "t1"))
		assert.Equal(t, http.StatusNotFound, one.Code)

		off := newMCPAPITest(t, nil)
		list = off.do(off.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/destination-types", nil)))
		assert.NotContains(t, types(t, list), "mcp")
	})

	t.Run("tenant delete purges the verification cache", func(t *testing.T) {
		v := &stubVerifier{}
		h := newMCPAPITest(t, v)
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodDelete, "/api/v1/tenants/t1", nil)))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, []string{"t1"}, v.purged)
	})
}

func TestAPI_MCPOffKeepsReadsWorking(t *testing.T) {
	h := newMCPAPITest(t, nil)
	require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(
		df.WithID("sub_1"), df.WithTenantID("t1"), df.WithType("mcp"),
		df.WithTopics([]string{"user.created"}),
		df.WithConfig(map[string]string{"url": mcpCallbackURL}),
		df.WithCredentials(map[string]string{"secret": mcpSecret(1)}),
	)))

	list := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)))
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	assert.Contains(t, list.Body.String(), `"sub_1"`)

	get := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/t1/destinations/sub_1", nil)))
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	assert.Equal(t, "mcp", responseBody(t, get)["type"])
	assert.NotContains(t, get.Body.String(), mcpSecret(1), "the secret is not shown")
	assert.Equal(t, map[string]any{}, responseBody(t, get)["credentials"])
}
