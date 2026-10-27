package apirouter_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/tenantstore/memtenantstore"
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

func TestAPI_MCPDestinations(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		t.Run("api key creates a subscription with an expiry", func(t *testing.T) {
			v := &stubVerifier{}
			h := newMCPAPITest(t, v)
			expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

			resp := createMCP(t, h, mcpDestination(func(d map[string]any) { d["expires_at"] = expires.Format(time.RFC3339) }))

			require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
			body := responseBody(t, resp)
			assert.Equal(t, "sub_1", body["id"])
			assert.Equal(t, "mcp", body["type"])
			assert.Equal(t, expires.Format(time.RFC3339), body["expires_at"])
			assert.Equal(t, mcpCallbackURL, body["target"])
			assert.NotContains(t, resp.Body.String(), mcpSecret(1), "the secret is masked")
			reqs := v.Requests()
			require.Len(t, reqs, 1)
			assert.Equal(t, "t1", reqs[0].TenantID)
			assert.Equal(t, "sub_1", reqs[0].SubscriptionID)
			assert.Equal(t, "user-1", reqs[0].Principal)

			got, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
			require.NoError(t, err)
			require.NotNil(t, got.ExpiresAt)
			assert.True(t, got.ExpiresAt.Equal(expires))
		})

		t.Run("expires_at is shown on get and list", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{})
			expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			createResp := createMCP(t, h, mcpDestination(func(d map[string]any) { d["expires_at"] = expires.Format(time.RFC3339) }))
			require.Equal(t, http.StatusCreated, createResp.Code, createResp.Body.String())

			get := h.do(h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/sub_1", nil)))
			require.Equal(t, http.StatusOK, get.Code, get.Body.String())
			assert.Equal(t, expires.Format(time.RFC3339), responseBody(t, get)["expires_at"])

			list := h.do(h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)))
			require.Equal(t, http.StatusOK, list.Code, list.Body.String())
			var items []map[string]any
			require.NoError(t, json.Unmarshal(list.Body.Bytes(), &items))
			require.Len(t, items, 1)
			assert.Equal(t, expires.Format(time.RFC3339), items[0]["expires_at"])
		})

		t.Run("tenant jwt gets 403", func(t *testing.T) {
			v := &stubVerifier{}
			h := newMCPAPITest(t, v)

			resp := h.do(h.withJWT(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", mcpDestination()), "t1"))

			require.Equal(t, http.StatusForbidden, resp.Code)
			assert.Empty(t, v.Requests(), "nothing is sent for a tenant")
			dests, err := h.tenantStore.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: "t1"})
			require.NoError(t, err)
			assert.Empty(t, dests)
		})

		t.Run("past expires_at returns 422", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{})
			resp := createMCP(t, h, mcpDestination(func(d map[string]any) {
				d["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			}))
			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			assert.Contains(t, resp.Body.String(), "expires_at must be in the future")
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

		t.Run("challenge failures return the validation shape", func(t *testing.T) {
			for _, tt := range []struct {
				name string
				err  error
				typ  string
			}{
				{"challenge_failed", mcpevents.CallbackEndpointError(mcpevents.ReasonChallengeFailed), "challenge_failed"},
				{"connection_refused", mcpevents.CallbackEndpointError(mcpevents.ReasonConnectionRefused), "connection_refused"},
				{"timeout", mcpevents.CallbackEndpointError(mcpevents.ReasonTimeout), "timeout"},
				{"tls_error", mcpevents.CallbackEndpointError(mcpevents.ReasonTLSError), "tls_error"},
				{"http_4xx", mcpevents.CallbackEndpointError(mcpevents.ReasonHTTP4xx), "http_4xx"},
				{"http_5xx", mcpevents.CallbackEndpointError(mcpevents.ReasonHTTP5xx), "http_5xx"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					h := newMCPAPITest(t, &stubVerifier{err: tt.err})
					resp := createMCP(t, h, mcpDestination())
					requireValidation422(t, resp, "config.url failed "+tt.typ+" validation")
					dests, err := h.tenantStore.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: "t1"})
					require.NoError(t, err)
					assert.Empty(t, dests, "nothing is stored")
				})
			}
		})

		t.Run("a verification limit returns 429 with Retry-After", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{err: mcpevents.ResourceExhausted(mcpevents.LimitVerificationRate, 10, 1500*time.Millisecond)})
			resp := createMCP(t, h, mcpDestination())
			require.Equal(t, http.StatusTooManyRequests, resp.Code, resp.Body.String())
			assert.Equal(t, "2", resp.Header().Get("Retry-After"))
			body := responseBody(t, resp)
			assert.Equal(t, map[string]any{"code": "rate_limited"}, body["data"])
			dests, err := h.tenantStore.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: "t1"})
			require.NoError(t, err)
			assert.Empty(t, dests)
		})

		t.Run("an unavailable verification store returns 503", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{err: errors.New("redis: connection refused")})
			resp := createMCP(t, h, mcpDestination())
			require.Equal(t, http.StatusServiceUnavailable, resp.Code, resp.Body.String())
			assert.Empty(t, resp.Header().Get("Retry-After"))
			assert.Equal(t, map[string]any{"code": "verification_unavailable"}, responseBody(t, resp)["data"])
			assert.NotContains(t, resp.Body.String(), "redis")
		})

		t.Run("refused addresses and schemes", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{})
			resp := createMCP(t, h, mcpDestination(func(d map[string]any) {
				d["config"] = map[string]string{"url": "http://receiver.example.com/hook"}
			}))
			requireValidation422(t, resp, "config.url failed https_required validation")

			resp = createMCP(t, h, mcpDestination(func(d map[string]any) {
				d["config"] = map[string]string{"url": "https://127.0.0.1/hook"}
			}))
			requireValidation422(t, resp, "config.url must resolve to a public address")
		})

		t.Run("each subscription counts toward the tenant limit", func(t *testing.T) {
			h := newMCPAPITest(t, &stubVerifier{})
			require.Equal(t, http.StatusCreated, createMCP(t, h, mcpDestination()).Code)
			resp := createMCP(t, h, mcpDestination(func(d map[string]any) { d["id"] = "sub_2" }))
			require.Equal(t, http.StatusCreated, resp.Code)
		})

		t.Run("a duplicate id sends no challenge", func(t *testing.T) {
			verifier := &stubVerifier{}
			h := newMCPAPITest(t, verifier)
			require.Equal(t, http.StatusCreated, createMCP(t, h, mcpDestination()).Code)
			require.Len(t, verifier.Requests(), 1)

			resp := createMCP(t, h, mcpDestination())

			require.Equal(t, http.StatusBadRequest, resp.Code)
			assert.Equal(t, "destination already exists", responseBody(t, resp)["message"])
			assert.Len(t, verifier.Requests(), 1)
		})

		t.Run("a full tenant sends no challenge", func(t *testing.T) {
			verifier := &stubVerifier{}
			h := newMCPAPITest(t, verifier,
				withTenantStore(memtenantstore.New(memtenantstore.WithMaxDestinationsPerTenant(1))),
				withMaxDestinations(1))
			require.Equal(t, http.StatusCreated, createMCP(t, h, mcpDestination()).Code)
			require.Len(t, verifier.Requests(), 1)

			resp := createMCP(t, h, mcpDestination(func(d map[string]any) { d["id"] = "sub_2" }))

			require.Equal(t, http.StatusBadRequest, resp.Code)
			assert.Equal(t, "maximum number of destinations per tenant reached", responseBody(t, resp)["message"])
			assert.Len(t, verifier.Requests(), 1)
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
		setup := func(t *testing.T) (*apiTest, *stubVerifier) {
			v := &stubVerifier{}
			h := newMCPAPITest(t, v)
			require.Equal(t, http.StatusCreated, createMCP(t, h, mcpDestination()).Code)
			return h, v
		}

		t.Run("sets and clears expires_at", func(t *testing.T) {
			h, _ := setup(t)
			expires := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

			resp := patchMCP(t, h, map[string]any{"expires_at": expires.Format(time.RFC3339)}, false)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assert.Equal(t, expires.Format(time.RFC3339), responseBody(t, resp)["expires_at"])

			resp = patchMCP(t, h, map[string]any{"expires_at": nil}, false)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assert.NotContains(t, responseBody(t, resp), "expires_at")

			got, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
			require.NoError(t, err)
			assert.Nil(t, got.ExpiresAt)
		})

		t.Run("past expires_at returns 422", func(t *testing.T) {
			h, _ := setup(t)
			resp := patchMCP(t, h, map[string]any{"expires_at": time.Now().Add(-time.Second).UTC().Format(time.RFC3339)}, false)
			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			assert.Contains(t, resp.Body.String(), "expires_at must be in the future")
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

		t.Run("immutable fields return 422", func(t *testing.T) {
			h, _ := setup(t)
			for _, tt := range []struct {
				name  string
				patch map[string]any
				want  string
			}{
				{"url", map[string]any{"config": map[string]string{"url": "https://other.example.com/hook"}}, "config.url failed immutable validation"},
				{"principal", map[string]any{"config": map[string]string{"principal": "user-2"}}, "config.principal failed immutable validation"},
				{"arguments", map[string]any{"config": map[string]string{"arguments": `{"status":"open"}`}}, "config.arguments failed immutable validation"},
				{"topics", map[string]any{"topics": []string{"user.deleted"}}, "topics failed immutable validation"},
				{"config cleared", map[string]any{"config": nil}, "config.url failed immutable validation"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					resp := patchMCP(t, h, tt.patch, false)
					require.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
					assert.Contains(t, resp.Body.String(), tt.want)
				})
			}
			got, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
			require.NoError(t, err)
			assert.Equal(t, mcpCallbackURL, got.Config["url"])
		})

		t.Run("repeating immutable values is accepted", func(t *testing.T) {
			h, _ := setup(t)
			resp := patchMCP(t, h, map[string]any{
				"topics": []string{"user.created"},
				"config": map[string]string{"url": mcpCallbackURL, "principal": "user-1"},
			}, false)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		})

		t.Run("a new secret rotates", func(t *testing.T) {
			h, _ := setup(t)
			resp := patchMCP(t, h, map[string]any{"credentials": map[string]string{"secret": mcpSecret(2)}}, false)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			got, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "sub_1")
			require.NoError(t, err)
			assert.Equal(t, mcpSecret(2), got.Credentials["secret"])
			assert.Equal(t, mcpSecret(1), got.Credentials["previous_secret"])
			assert.NotEmpty(t, got.Credentials["previous_secret_invalid_at"])
		})

		t.Run("a refresh covered by the cache sends no challenge", func(t *testing.T) {
			v := &stubVerifier{verified: true}
			h := newMCPAPITest(t, v)
			require.Equal(t, http.StatusCreated, createMCP(t, h, mcpDestination()).Code)
			resp := patchMCP(t, h, map[string]any{"credentials": map[string]string{"secret": mcpSecret(2)}}, false)
			require.Equal(t, http.StatusOK, resp.Code)
			assert.Empty(t, v.Requests())
		})

		t.Run("tenant jwt gets 403", func(t *testing.T) {
			h, _ := setup(t)
			resp := patchMCP(t, h, map[string]any{"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, true)
			require.Equal(t, http.StatusForbidden, resp.Code)
		})

		t.Run("tenant jwt can still delete", func(t *testing.T) {
			h, _ := setup(t)
			resp := h.do(h.withJWT(h.jsonReq(http.MethodDelete, "/api/v1/tenants/t1/destinations/sub_1", nil), "t1"))
			require.Equal(t, http.StatusOK, resp.Code)
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
