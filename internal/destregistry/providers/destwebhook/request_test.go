package destwebhook_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebhookRequest(t *testing.T) {
	t.Parallel()

	url := "https://example.com/webhook"
	data := map[string]string{"foo": "bar"}
	rawBody, err := json.Marshal(data)
	require.NoError(t, err)

	metadata := map[string]string{"Key1": "Value1", "Key2": "Value2"}
	headerPrefix := "x-outpost-"

	t.Run("should create request with valid secrets", func(t *testing.T) {
		t.Parallel()

		secrets := []destwebhook.WebhookSecret{
			{
				Key:       "secret1",
				CreatedAt: time.Now(),
			},
			{
				Key:       "secret2",
				CreatedAt: time.Now(),
			},
		}

		req := destwebhook.NewWebhookRequest(url, rawBody, metadata, headerPrefix, secrets)
		assert.Equal(t, url, req.URL)
		assert.Equal(t, rawBody, req.RawBody)
		assert.Len(t, req.Signatures, 2)
	})

	t.Run("should skip expired secrets", func(t *testing.T) {
		t.Parallel()

		secrets := []destwebhook.WebhookSecret{
			{
				Key:       "secret1",
				CreatedAt: time.Now().Add(-25 * time.Hour), // Expired
			},
			{
				Key:       "secret2",
				CreatedAt: time.Now(), // Valid
			},
		}

		req := destwebhook.NewWebhookRequest(url, rawBody, metadata, headerPrefix, secrets)
		assert.Len(t, req.Signatures, 1)
	})

	t.Run("should always use single secret regardless of age", func(t *testing.T) {
		t.Parallel()

		oldSecret := destwebhook.WebhookSecret{
			Key:       "old_secret",
			CreatedAt: time.Now().Add(-48 * time.Hour), // 48 hours old
		}

		req := destwebhook.NewWebhookRequest(url, rawBody, metadata, headerPrefix, []destwebhook.WebhookSecret{oldSecret})
		require.Len(t, req.Signatures, 1, "should generate signature for single secret regardless of age")
	})

	t.Run("should always use latest secret for signing", func(t *testing.T) {
		t.Parallel()

		secrets := []destwebhook.WebhookSecret{
			{
				Key:       "latest",
				CreatedAt: time.Now().Add(-48 * time.Hour), // Old but latest
			},
			{
				Key:       "older",
				CreatedAt: time.Now().Add(-72 * time.Hour), // Older
			},
			{
				Key:       "oldest",
				CreatedAt: time.Now().Add(-96 * time.Hour), // Oldest
			},
		}

		req := destwebhook.NewWebhookRequest(url, rawBody, metadata, headerPrefix, secrets)
		assert.Len(t, req.Signatures, 1, "should only use latest secret")
	})
}

func TestWebhookRequest_ToHTTPRequest(t *testing.T) {
	t.Parallel()

	url := "https://example.com/webhook"
	data := map[string]string{"foo": "bar"}
	rawBody, err := json.Marshal(data)
	require.NoError(t, err)

	metadata := map[string]string{"Key1": "Value1", "Key2": "Value2"}
	headerPrefix := "x-outpost-"

	t.Run("should create HTTP request with signatures and metadata", func(t *testing.T) {
		t.Parallel()

		secrets := []destwebhook.WebhookSecret{
			{
				Key:       "secret1",
				CreatedAt: time.Now(),
			},
		}

		webhookReq := destwebhook.NewWebhookRequest(url, rawBody, metadata, headerPrefix, secrets)
		httpReq, err := webhookReq.ToHTTPRequest(context.Background())
		require.NoError(t, err)

		assert.Equal(t, "POST", httpReq.Method)
		assert.Equal(t, url, httpReq.URL.String())
		assert.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))
		assert.NotEmpty(t, httpReq.Header.Get(headerPrefix+"signature"))
		assert.Equal(t, "Value1", httpReq.Header.Get(headerPrefix+"key1"))
		assert.Equal(t, "Value2", httpReq.Header.Get(headerPrefix+"key2"))
	})

	t.Run("should handle secret rotation", func(t *testing.T) {
		t.Parallel()

		secrets := []destwebhook.WebhookSecret{
			{
				Key:       "new_secret",
				CreatedAt: time.Now(),
			},
			{
				Key:       "old_secret",
				CreatedAt: time.Now().Add(-12 * time.Hour), // Still valid but older
			},
			{
				Key:       "old_secret2",
				CreatedAt: time.Now().Add(-18 * time.Hour), // Still valid but older
			},
		}

		webhookReq := destwebhook.NewWebhookRequest(url, rawBody, metadata, headerPrefix, secrets)
		httpReq, err := webhookReq.ToHTTPRequest(context.Background())
		require.NoError(t, err)

		signatureHeader := httpReq.Header.Get(headerPrefix + "signature")
		parts := strings.SplitN(signatureHeader, ",", 2)
		require.True(t, len(parts) >= 2, "signature header should have timestamp and signatures")

		// First part should be timestamp
		assert.True(t, strings.HasPrefix(parts[0], "t="))

		// Second part should start with v0= and contain all signatures
		assert.True(t, strings.HasPrefix(parts[1], "v0="))
		signatures := strings.Split(strings.TrimPrefix(parts[1], "v0="), ",")
		require.Len(t, signatures, 3, "should have signatures from all secrets")

		for _, secret := range secrets {
			assertValidSignature(t, secret.Key, rawBody, signatureHeader)
		}
	})
}
