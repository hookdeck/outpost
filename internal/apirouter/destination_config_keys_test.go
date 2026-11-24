package apirouter_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/desthookdeck"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func customEndpointsRegistry(t *testing.T, allow bool) destregistry.Registry {
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
		AWSEventBridge:       &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
		AllowCustomEndpoints: allow,
	}))
	return reg
}

type verifyTransport struct{}

func (verifyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"name":"verified-name","url":"https://events.hookdeck.com/e/x","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","disabled_at":null}`)
	return w.Result(), nil
}

func hookdeckRegistry(t *testing.T) destregistry.Registry {
	t.Helper()
	reg := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	provider, err := desthookdeck.New(reg.MetadataLoader(), nil, desthookdeck.WithHTTPClient(&http.Client{Transport: verifyTransport{}}))
	require.NoError(t, err)
	require.NoError(t, reg.RegisterProvider("hookdeck", provider))
	return reg
}

func decodeDisplay(t *testing.T, body []byte) destregistry.DestinationDisplay {
	t.Helper()
	var d destregistry.DestinationDisplay
	require.NoError(t, json.Unmarshal(body, &d))
	return d
}

func TestDestinationConfigKeys_Webhook(t *testing.T) {
	const path = "/api/v1/tenants/t1/destinations"

	t.Run("create drops undeclared keys", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(webhookStandardRegistry(t)))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, path, map[string]any{
			"id":          "d1",
			"type":        "webhook",
			"topics":      []string{"user.created"},
			"config":      map[string]string{"url": "https://example.com/hook", "extra": "x"},
			"credentials": map[string]string{"extra": "x"},
		})))
		require.Equal(t, http.StatusCreated, resp.Code)

		created := decodeDisplay(t, resp.Body.Bytes())
		assert.Equal(t, models.MapStringString{"url": "https://example.com/hook"}, created.Config)
		assert.NotContains(t, created.Credentials, "extra")
		assert.Contains(t, created.Credentials, "secret")

		stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Equal(t, models.MapStringString{"url": "https://example.com/hook"}, stored.Config)
		assert.NotContains(t, stored.Credentials, "extra")
		assert.Contains(t, stored.Credentials, "secret")
	})

	t.Run("update drops undeclared keys", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(webhookStandardRegistry(t)))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(
			df.WithID("d1"), df.WithTenantID("t1"), df.WithType("webhook"),
			df.WithConfig(map[string]string{"url": "https://example.com/hook"}),
			df.WithCredentials(map[string]string{"secret": "whsec_c2VjcmV0c2VjcmV0c2VjcmV0"}),
		)))

		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, path+"/d1", map[string]any{
			"config":      map[string]string{"extra": "x"},
			"credentials": map[string]string{"extra": "x"},
		})))
		require.Equal(t, http.StatusOK, resp.Code)

		updated := decodeDisplay(t, resp.Body.Bytes())
		assert.Equal(t, models.MapStringString{"url": "https://example.com/hook"}, updated.Config)
		assert.NotContains(t, updated.Credentials, "extra")

		stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Equal(t, models.MapStringString{"url": "https://example.com/hook"}, stored.Config)
		assert.NotContains(t, stored.Credentials, "extra")
		assert.Equal(t, "whsec_c2VjcmV0c2VjcmV0c2VjcmV0", stored.Credentials["secret"])
	})

	t.Run("update removes undeclared keys stored earlier", func(t *testing.T) {
		for name, patch := range map[string]map[string]any{
			"topics only": {"topics": []string{"user.deleted"}},
			"config only": {"config": map[string]string{"url": "https://example.com/other"}},
		} {
			t.Run(name, func(t *testing.T) {
				h := newAPITest(t, withDestRegistry(webhookStandardRegistry(t)))
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(
					df.WithID("d1"), df.WithTenantID("t1"), df.WithType("webhook"),
					df.WithConfig(map[string]string{"url": "https://example.com/hook", "extra": "x"}),
					df.WithCredentials(map[string]string{"secret": "whsec_c2VjcmV0c2VjcmV0c2VjcmV0", "extra": "x"}),
				)))

				resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, path+"/d1", patch)))
				require.Equal(t, http.StatusOK, resp.Code)

				updated := decodeDisplay(t, resp.Body.Bytes())
				assert.NotContains(t, updated.Config, "extra")
				assert.NotContains(t, updated.Credentials, "extra")

				stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
				require.NoError(t, err)
				assert.NotContains(t, stored.Config, "extra")
				assert.NotContains(t, stored.Credentials, "extra")
				assert.Equal(t, "whsec_c2VjcmV0c2VjcmV0c2VjcmV0", stored.Credentials["secret"])
				assert.Contains(t, stored.Config, "url")
			})
		}
	})

	t.Run("metadata and delivery_metadata keep arbitrary keys", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(webhookStandardRegistry(t)))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, path, map[string]any{
			"id":                "d1",
			"type":              "webhook",
			"topics":            []string{"user.created"},
			"config":            map[string]string{"url": "https://example.com/hook"},
			"metadata":          map[string]string{"anything": "a"},
			"delivery_metadata": map[string]string{"anything": "b"},
		})))
		require.Equal(t, http.StatusCreated, resp.Code)

		resp = h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, path+"/d1", map[string]any{
			"metadata": map[string]string{"other": "c"},
		})))
		require.Equal(t, http.StatusOK, resp.Code)

		stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Equal(t, models.MapStringString{"anything": "a", "other": "c"}, stored.Metadata)
		assert.Equal(t, models.MapStringString{"anything": "b"}, stored.DeliveryMetadata)
	})
}

func TestDestinationConfigKeys_AWS(t *testing.T) {
	const path = "/api/v1/tenants/t1/destinations"
	sqsConfig := map[string]string{"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/q"}
	s3Config := map[string]string{"bucket": "b", "region": "us-east-1"}

	for name, tc := range map[string]struct {
		destType string
		config   map[string]string
	}{
		"aws_sqs": {"aws_sqs", sqsConfig},
		"aws_s3":  {"aws_s3", s3Config},
	} {
		t.Run(name+" credentials keep session and drop extras", func(t *testing.T) {
			h := newAPITest(t, withDestRegistry(customEndpointsRegistry(t, false)))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, path, map[string]any{
				"id":          "d1",
				"type":        tc.destType,
				"topics":      []string{"user.created"},
				"config":      tc.config,
				"credentials": map[string]string{"key": "k", "secret": "s", "session": "session-token-value", "extra": "x"},
			})))
			require.Equal(t, http.StatusCreated, resp.Code)

			created := decodeDisplay(t, resp.Body.Bytes())
			assert.NotContains(t, created.Credentials, "extra")
			assert.Contains(t, created.Credentials, "session")
			assert.NotEqual(t, "session-token-value", created.Credentials["session"], "session is masked")

			stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
			require.NoError(t, err)
			assert.Equal(t, models.MapStringString{"key": "k", "secret": "s", "session": "session-token-value"}, stored.Credentials)
		})

		t.Run(name+" endpoint survives create and update when custom endpoints are allowed", func(t *testing.T) {
			h := newAPITest(t, withDestRegistry(customEndpointsRegistry(t, true)))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			config := map[string]string{"endpoint": "http://localhost:4566"}
			for k, v := range tc.config {
				config[k] = v
			}

			resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, path, map[string]any{
				"id":          "d1",
				"type":        tc.destType,
				"topics":      []string{"user.created"},
				"config":      config,
				"credentials": map[string]string{"key": "k", "secret": "s"},
			})))
			require.Equal(t, http.StatusCreated, resp.Code)
			stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
			require.NoError(t, err)
			assert.Equal(t, "http://localhost:4566", stored.Config["endpoint"])

			resp = h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, path+"/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})))
			require.Equal(t, http.StatusOK, resp.Code)
			stored, err = h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
			require.NoError(t, err)
			assert.Equal(t, "http://localhost:4566", stored.Config["endpoint"])
		})

		t.Run(name+" stored endpoint is kept by a topics update and rejected by a config update when custom endpoints are off", func(t *testing.T) {
			h := newAPITest(t, withDestRegistry(customEndpointsRegistry(t, false)))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			config := map[string]string{"endpoint": "http://localhost:4566"}
			for k, v := range tc.config {
				config[k] = v
			}
			require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithType(tc.destType),
				df.WithConfig(config),
				df.WithCredentials(map[string]string{"key": "k", "secret": "s"}),
			)))

			resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, path+"/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})))
			require.Equal(t, http.StatusOK, resp.Code)
			stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
			require.NoError(t, err)
			assert.Equal(t, "http://localhost:4566", stored.Config["endpoint"])

			resp = h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, path+"/d1", map[string]any{
				"config": config,
			})))
			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})
	}
}

// An update that leaves config untouched shares the stored destination's
// config map; a rejected update must not change what is stored.
func TestDestinationConfigKeys_RejectedUpdateLeavesStoredDestination(t *testing.T) {
	h := newAPITest(t, withDestRegistry(customEndpointsRegistry(t, false)))
	h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
	require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(
		df.WithID("d1"), df.WithTenantID("t1"), df.WithType("aws_sqs"),
		df.WithConfig(map[string]string{"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/q", "extra": "x"}),
		df.WithCredentials(map[string]string{"key": "k", "secret": "s"}),
	)))

	resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
		"credentials": map[string]any{"key": nil},
	})))
	require.Equal(t, http.StatusUnprocessableEntity, resp.Code)

	stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
	require.NoError(t, err)
	assert.Equal(t, "x", stored.Config["extra"])
	assert.Equal(t, models.MapStringString{"key": "k", "secret": "s"}, stored.Credentials)
}

func TestDestinationConfigKeys_HookdeckSourceFromTokenOnly(t *testing.T) {
	const path = "/api/v1/tenants/t1/destinations"
	token := base64.StdEncoding.EncodeToString([]byte("src_Verified123:secret"))
	requested := map[string]string{"source_id": "src_Requested", "source_name": "requested"}
	verified := models.MapStringString{"source_id": "src_Verified123", "source_name": "verified-name"}

	t.Run("create", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(hookdeckRegistry(t)))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, path, map[string]any{
			"id":          "d1",
			"type":        "hookdeck",
			"topics":      []string{"user.created"},
			"config":      requested,
			"credentials": map[string]string{"token": token},
		})))
		require.Equal(t, http.StatusCreated, resp.Code)

		stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Equal(t, verified, stored.Config)
		assert.Equal(t, "verified-name", decodeDisplay(t, resp.Body.Bytes()).Target)
	})

	t.Run("update with the same token", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(hookdeckRegistry(t)))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(
			df.WithID("d1"), df.WithTenantID("t1"), df.WithType("hookdeck"),
			df.WithConfig(verified),
			df.WithCredentials(map[string]string{"token": token}),
		)))

		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, path+"/d1", map[string]any{
			"config": requested,
		})))
		require.Equal(t, http.StatusOK, resp.Code)

		stored, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
		require.NoError(t, err)
		assert.Equal(t, verified, stored.Config)
		assert.Equal(t, "verified-name", decodeDisplay(t, resp.Body.Bytes()).Target)
	})
}
