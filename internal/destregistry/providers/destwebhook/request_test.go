package destwebhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
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
	eventID := "evt_123"
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

		req := destwebhook.NewWebhookRequest(url, eventID, rawBody, metadata, headerPrefix, secrets)
		assert.Equal(t, url, req.URL)
		assert.Equal(t, eventID, req.EventID)
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

		req := destwebhook.NewWebhookRequest(url, eventID, rawBody, metadata, headerPrefix, secrets)
		assert.Len(t, req.Signatures, 1)
	})

	t.Run("should always use single secret regardless of age", func(t *testing.T) {
		t.Parallel()

		oldSecret := destwebhook.WebhookSecret{
			Key:       "old_secret",
			CreatedAt: time.Now().Add(-48 * time.Hour), // 48 hours old
		}

		req := destwebhook.NewWebhookRequest(url, eventID, rawBody, metadata, headerPrefix, []destwebhook.WebhookSecret{oldSecret})
		require.Len(t, req.Signatures, 1, "should generate signature for single secret regardless of age")
	})
}

func TestWebhookRequest_ToHTTPRequest(t *testing.T) {
	t.Parallel()

	url := "https://example.com/webhook"
	eventID := "evt_123"
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

		webhookReq := destwebhook.NewWebhookRequest(url, eventID, rawBody, metadata, headerPrefix, secrets)
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

		webhookReq := destwebhook.NewWebhookRequest(url, eventID, rawBody, metadata, headerPrefix, secrets)
		httpReq, err := webhookReq.ToHTTPRequest(context.Background())
		require.NoError(t, err)

		signatureHeader := httpReq.Header.Get(headerPrefix + "signature")
		signatures := strings.Split(signatureHeader, " ")
		require.Len(t, signatures, 3, "should have signatures from all secrets")

		for i, secret := range secrets {
			assertValidSignature(t, secret.Key, eventID, rawBody, signatures[i])
		}
	})
}

func assertValidSignature(t *testing.T, secret string, eventID string, rawBody []byte, signatureHeader string) {
	t.Helper()

	// Parse "t={timestamp},v1={signature}" format
	parts := strings.Split(signatureHeader, ",")
	require.Len(t, parts, 2, "signature header should have timestamp and signature parts")

	timestampStr := strings.TrimPrefix(parts[0], "t=")
	signature := strings.TrimPrefix(parts[1], "v1=")

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	require.NoError(t, err, "timestamp should be a valid integer")

	// Reconstruct the signed content
	signedContent := fmt.Sprintf("%s.%d.%s", eventID, timestamp, rawBody)

	// Generate HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedContent))
	expectedSignature := fmt.Sprintf("%x", mac.Sum(nil))

	assert.Equal(t, expectedSignature, signature, "signature should match expected value")
}
