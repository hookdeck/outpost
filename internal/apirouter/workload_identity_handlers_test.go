package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/hookdeck/outpost/internal/workloadidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIssuerURL   = "https://outpost.example.com/workload-identity"
	testWIFProvider = "projects/123456/locations/global/workloadIdentityPools/outpost/providers/outpost"
	testSAKeyJSON   = `{"type":"service_account","project_id":"my-project"}`
)

func newTestWorkloadIdentity(t *testing.T) *workloadidentity.Issuer {
	t.Helper()
	issuer, err := workloadidentity.New(workloadidentity.Config{
		Issuer:     testIssuerURL,
		SigningKey: testutil.SigningKeyPEM(t),
	})
	require.NoError(t, err)
	return issuer
}

// registryWithWorkloadIdentity builds the default registry the way the
// service does, with issuer nil when the feature is off.
func registryWithWorkloadIdentity(t *testing.T, issuer *workloadidentity.Issuer) destregistry.Registry {
	t.Helper()
	reg := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, destregistrydefault.RegisterDefault(reg, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
		},
		AWSEventBridge:   &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
		WorkloadIdentity: issuer,
	}))
	return reg
}

// newWorkloadIdentityAPITest returns an API with the feature on or off.
func newWorkloadIdentityAPITest(t *testing.T, enabled bool) *apiTest {
	t.Helper()
	var issuer *workloadidentity.Issuer
	if enabled {
		issuer = newTestWorkloadIdentity(t)
	}
	h := newAPITest(t,
		withDestRegistry(registryWithWorkloadIdentity(t, issuer)),
		withWorkloadIdentity(issuer),
	)
	h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
	h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
	return h
}

func validationMessages(t *testing.T, resp *httptest.ResponseRecorder) []string {
	t.Helper()
	require.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	var body struct {
		Message string   `json:"message"`
		Data    []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "validation error", body.Message)
	return body.Data
}

func TestWorkloadIdentity_IssuerEndpoints(t *testing.T) {
	t.Run("enabled: discovery", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := h.do(h.jsonReq(http.MethodGet, "/workload-identity/.well-known/openid-configuration", nil))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, "public, max-age=300", resp.Header().Get("Cache-Control"))

		var doc workloadidentity.Discovery
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &doc))
		assert.Equal(t, testIssuerURL, doc.Issuer)
		assert.Equal(t, testIssuerURL+"/jwks.json", doc.JWKSURI)
		assert.Equal(t, []string{"ES256"}, doc.IDTokenSigningAlgValuesSupported)
	})

	t.Run("enabled: jwks", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := h.do(h.jsonReq(http.MethodGet, "/workload-identity/jwks.json", nil))
		require.Equal(t, http.StatusOK, resp.Code)

		var jwks workloadidentity.JWKS
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &jwks))
		require.Len(t, jwks.Keys, 1)
		assert.Equal(t, "EC", jwks.Keys[0].Kty)
		assert.NotEmpty(t, jwks.Keys[0].Kid)
	})

	t.Run("disabled: not served", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, false)
		for _, path := range []string{"/workload-identity/.well-known/openid-configuration", "/workload-identity/jwks.json"} {
			w := httptest.NewRecorder()
			h.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			assert.NotContains(t, w.Body.String(), "jwks", path)
		}
	})
}

func TestWorkloadIdentity_TenantRoute(t *testing.T) {
	const path = "/api/v1/tenants/t1/workload-identity"

	t.Run("tenant jwt", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := h.do(h.withJWT(h.jsonReq(http.MethodGet, path, nil), "t1"))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `{"issuer":"`+testIssuerURL+`","subject":"tenant:t1"}`, resp.Body.String())
	})

	t.Run("api key", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, path, nil)))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `{"issuer":"`+testIssuerURL+`","subject":"tenant:t1"}`, resp.Body.String())
	})

	t.Run("another tenant's jwt", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		testutil.RequireErrorResponse(t, h.do(h.withJWT(h.jsonReq(http.MethodGet, path, nil), "t2")), http.StatusForbidden, "forbidden")
	})

	t.Run("no auth", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		testutil.RequireErrorResponse(t, h.do(h.jsonReq(http.MethodGet, path, nil)), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("unknown tenant", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/nope/workload-identity", nil)))
		testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "tenant not found")
	})

	t.Run("disabled", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, false)
		testutil.RequireErrorResponse(t, h.do(h.withAPIKey(h.jsonReq(http.MethodGet, path, nil))), http.StatusNotFound, "not found")
	})
}

func TestWorkloadIdentity_DestinationTypes(t *testing.T) {
	get := func(t *testing.T, h *apiTest) metadata.ProviderMetadata {
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/destination-types/gcp_pubsub", nil)))
		require.Equal(t, http.StatusOK, resp.Code)
		var meta metadata.ProviderMetadata
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &meta))
		return meta
	}

	t.Run("enabled: auth method with both options", func(t *testing.T) {
		meta := get(t, newWorkloadIdentityAPITest(t, true))
		auth, ok := meta.Field("auth_method")
		require.True(t, ok)
		assert.Len(t, auth.Options, 2)
		_, ok = meta.Field("workload_identity_provider")
		assert.True(t, ok)
	})

	t.Run("disabled: service account key only", func(t *testing.T) {
		meta := get(t, newWorkloadIdentityAPITest(t, false))
		_, ok := meta.Field("auth_method")
		assert.False(t, ok)
		_, ok = meta.Field("workload_identity_provider")
		assert.False(t, ok)
		key, ok := meta.Field("service_account_json")
		require.True(t, ok)
		assert.True(t, key.Required)
		assert.Nil(t, key.VisibleWhen)
	})
}

func TestWorkloadIdentity_Destinations(t *testing.T) {
	create := func(h *apiTest, id string, config, credentials map[string]string) *httptest.ResponseRecorder {
		cfg := map[string]string{"project_id": "my-project", "topic": "my-topic"}
		for k, v := range config {
			cfg[k] = v
		}
		return h.do(h.withJWT(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
			"id":          id,
			"type":        "gcp_pubsub",
			"topics":      "*",
			"config":      cfg,
			"credentials": credentials,
		}), "t1"))
	}
	patch := func(h *apiTest, id string, body map[string]any) *httptest.ResponseRecorder {
		return h.do(h.withJWT(h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/"+id, body), "t1"))
	}
	wif := map[string]string{"auth_method": "workload_identity"}

	t.Run("create with workload identity", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := create(h, "d1", wif, map[string]string{"workload_identity_provider": testWIFProvider})
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())

		var dest destregistry.DestinationDisplay
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
		assert.Equal(t, "workload_identity", dest.Config["auth_method"])
		assert.Equal(t, testWIFProvider, dest.Credentials["workload_identity_provider"], "provider name is not obfuscated")
	})

	t.Run("create without auth method uses a key", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := create(h, "d1", nil, map[string]string{"service_account_json": testSAKeyJSON})
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())

		var dest destregistry.DestinationDisplay
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
		assert.NotContains(t, dest.Config, "auth_method", "the stored destination is not rewritten")
	})

	t.Run("create with both methods' credentials", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := create(h, "d1", wif, map[string]string{"workload_identity_provider": testWIFProvider, "service_account_json": testSAKeyJSON})
		assert.Equal(t, []string{"credentials.service_account_json is forbidden"}, validationMessages(t, resp))
	})

	t.Run("create with workload identity but no provider", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := create(h, "d1", wif, nil)
		assert.Equal(t, []string{"credentials.workload_identity_provider is required"}, validationMessages(t, resp))
	})

	t.Run("disabled: create with workload identity rejected", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, false)
		resp := create(h, "d1", wif, map[string]string{"workload_identity_provider": testWIFProvider, "service_account_json": testSAKeyJSON})
		assert.Equal(t, []string{"config.auth_method failed enum validation"}, validationMessages(t, resp))
	})

	t.Run("disabled: create with a key", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, false)
		resp := create(h, "d1", nil, map[string]string{"service_account_json": testSAKeyJSON})
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
	})

	t.Run("switch from key to workload identity", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		require.Equal(t, http.StatusCreated, create(h, "d1", nil, map[string]string{"service_account_json": testSAKeyJSON}).Code)

		resp := patch(h, "d1", map[string]any{
			"config":      map[string]any{"auth_method": "workload_identity"},
			"credentials": map[string]any{"workload_identity_provider": testWIFProvider},
		})
		assert.Equal(t, []string{"credentials.service_account_json is forbidden"}, validationMessages(t, resp),
			"the key has to be removed in the same request")

		resp = patch(h, "d1", map[string]any{
			"config":      map[string]any{"auth_method": "workload_identity"},
			"credentials": map[string]any{"workload_identity_provider": testWIFProvider, "service_account_json": nil},
		})
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var dest destregistry.DestinationDisplay
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
		assert.Equal(t, map[string]string{"workload_identity_provider": testWIFProvider, "service_account_json": ""}, map[string]string(dest.Credentials))
	})

	t.Run("switch from key to workload identity with empty strings", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		require.Equal(t, http.StatusCreated, create(h, "d1", nil, map[string]string{"service_account_json": testSAKeyJSON}).Code)

		resp := patch(h, "d1", map[string]any{
			"config":      map[string]any{"auth_method": "workload_identity"},
			"credentials": map[string]any{"workload_identity_provider": testWIFProvider, "service_account_json": ""},
		})
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var dest destregistry.DestinationDisplay
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
		assert.Equal(t, map[string]string{"workload_identity_provider": testWIFProvider, "service_account_json": ""}, map[string]string(dest.Credentials))
	})

	t.Run("create with an empty key, as typed clients send it", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		resp := create(h, "d1", wif, map[string]string{"workload_identity_provider": testWIFProvider, "service_account_json": ""})
		require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
	})

	t.Run("switch from workload identity to key", func(t *testing.T) {
		h := newWorkloadIdentityAPITest(t, true)
		require.Equal(t, http.StatusCreated, create(h, "d1", wif, map[string]string{"workload_identity_provider": testWIFProvider}).Code)

		resp := patch(h, "d1", map[string]any{
			"config":      map[string]any{"auth_method": "service_account_key"},
			"credentials": map[string]any{"workload_identity_provider": nil, "service_account_json": testSAKeyJSON},
		})
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var dest destregistry.DestinationDisplay
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
		assert.Equal(t, []string{"service_account_json"}, keys(dest.Credentials))
	})
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Clients built against a schema where credentials.service_account_json is
// required must be able to read workload identity destinations.
func TestWorkloadIdentity_ResponsesIncludeServiceAccountJSON(t *testing.T) {
	h := newWorkloadIdentityAPITest(t, true)
	createResp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
		"id":          "d1",
		"type":        "gcp_pubsub",
		"topics":      "*",
		"config":      map[string]string{"project_id": "p", "topic": "t", "auth_method": "workload_identity"},
		"credentials": map[string]string{"workload_identity_provider": testWIFProvider},
	})))
	require.Equal(t, http.StatusCreated, createResp.Code, createResp.Body.String())

	requireKey := func(name string, body []byte) {
		t.Helper()
		var dest struct {
			Credentials map[string]*string `json:"credentials"`
		}
		require.NoError(t, json.Unmarshal(body, &dest), name)
		v, ok := dest.Credentials["service_account_json"]
		require.True(t, ok, "%s: credentials.service_account_json missing", name)
		require.NotNil(t, v, name)
		assert.Equal(t, "", *v, name)
	}
	requireKey("create", createResp.Body.Bytes())

	get := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)))
	require.Equal(t, http.StatusOK, get.Code)
	requireKey("get", get.Body.Bytes())

	list := h.do(h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)))
	require.Equal(t, http.StatusOK, list.Code)
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &items))
	require.Len(t, items, 1)
	requireKey("list", items[0])

	patched := h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
		"credentials": map[string]any{"service_account_email": "publisher@p.iam.gserviceaccount.com"},
	})))
	require.Equal(t, http.StatusOK, patched.Code, patched.Body.String())
	requireKey("patch", patched.Body.Bytes())

	stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
	require.NoError(t, err)
	assert.NotContains(t, stored.Credentials, "service_account_json", "storage is unchanged")
}
