package apirouter_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func webhookRegistry(t *testing.T, block bool, allowlist ...string) destregistry.Registry {
	t.Helper()
	reg := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	err := destregistrydefault.RegisterDefault(reg, destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: &destregistrydefault.DestWebhookConfig{
			HeaderPrefix:             destwebhook.DefaultHeaderPrefix,
			SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
			SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
			SignatureEncoding:        destwebhook.DefaultEncoding,
			SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
			SigningSecretTemplate:    destwebhook.DefaultSigningSecretTmpl,
			BlockPrivateAddresses:    block,
		},
		PrivateAddressAllowlist: allowlist,
		AWSEventBridge:          &destregistrydefault.DestAWSEventBridgeConfig{Source: "outpost"},
	})
	require.NoError(t, err)
	return reg
}

func TestDestinationPrivateAddresses(t *testing.T) {
	post := func(h *apiTest, id, url string) (int, string) {
		req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
			"id":     id,
			"type":   "webhook",
			"topics": []string{"user.created"},
			"config": map[string]string{"url": url},
		})
		resp := h.do(h.withAPIKey(req))
		return resp.Code, resp.Body.String()
	}

	t.Run("blocked", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(webhookRegistry(t, true)))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		code, body := post(h, "d1", "http://10.0.0.1/hook")
		require.Equal(t, http.StatusUnprocessableEntity, code, body)
		var got struct {
			Message string   `json:"message"`
			Data    []string `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &got))
		assert.Equal(t, "validation error", got.Message)
		assert.Equal(t, []string{"config.url must resolve to a public address"}, got.Data)

		code, body = post(h, "d2", "https://8.8.8.8/hook")
		require.Equal(t, http.StatusCreated, code, body)

		patch := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d2", map[string]any{
			"config": map[string]string{"url": "http://127.0.0.1/hook"},
		})
		resp := h.do(h.withAPIKey(patch))
		assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), "config.url must resolve to a public address")
	})

	t.Run("allowlisted", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(webhookRegistry(t, true, "10.0.0.0/8")))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		code, body := post(h, "d1", "http://10.0.0.1/hook")
		assert.Equal(t, http.StatusCreated, code, body)
	})

	t.Run("switch off", func(t *testing.T) {
		h := newAPITest(t, withDestRegistry(webhookRegistry(t, false)))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		code, body := post(h, "d1", "http://10.0.0.1/hook")
		assert.Equal(t, http.StatusCreated, code, body)
	})
}
