package destwebhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	standardContentTmpl = "{{.EventID}}.{{.Timestamp.Unix}}.{{.Body}}"
	standardHeaderTmpl  = "v1,{{index .Signatures 0}}{{range slice .Signatures 1}} v1,{{.}}{{end}}"
)

// standardWebhooksCompat is the compat config for the Standard Webhooks scheme.
func standardWebhooksCompat() *destwebhook.CompatSignatureConfig {
	return &destwebhook.CompatSignatureConfig{
		SignatureHeaderName:      "webhook-signature",
		SignatureContentTemplate: standardContentTmpl,
		SignatureHeaderTemplate:  standardHeaderTmpl,
		SignatureEncoding:        "base64",
		SignatureAlgorithm:       "hmac-sha256",
		SecretEncoding:           destwebhook.SecretEncodingBase64,
		SecretPrefix:             "whsec_",
		Headers: map[string]string{
			"webhook-id":        "{{.EventID}}",
			"webhook-timestamp": "{{.Timestamp.Unix}}",
		},
	}
}

func TestCompatSignature_SentAlongsidePrimary(t *testing.T) {
	t.Parallel()

	keyBytes := []byte("0123456789abcdef0123456789abcdef")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(keyBytes)

	provider := NewTestProvider(t, destwebhook.WithCompatSignature(standardWebhooksCompat()))
	req, body := formatRequest(t, provider, webhookDestination(secret))

	// Primary signature: raw secret string as key, hex over the body.
	primary := hmac.New(sha256.New, []byte(secret))
	primary.Write([]byte(body))
	assert.Equal(t, "v0="+hex.EncodeToString(primary.Sum(nil)), req.Header.Get("x-outpost-signature"))

	// Compat signature, verified the way a Standard Webhooks receiver would:
	// base64-decoded key over "<id>.<unix ts>.<body>".
	id := req.Header.Get("webhook-id")
	ts := req.Header.Get("webhook-timestamp")
	require.NotEmpty(t, id)
	require.NotEmpty(t, ts)
	assert.Equal(t, req.Header.Get("x-outpost-event-id"), id)

	compat := hmac.New(sha256.New, keyBytes)
	compat.Write([]byte(fmt.Sprintf("%s.%s.%s", id, ts, body)))
	assert.Equal(t, "v1,"+base64.StdEncoding.EncodeToString(compat.Sum(nil)), req.Header.Get("webhook-signature"))

	// Both schemes sign the same instant.
	primaryTS, err := time.Parse(time.RFC3339, req.Header.Get("x-outpost-timestamp"))
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprint(primaryTS.Unix()), ts)
}

func TestCompatSignature_NotConfigured(t *testing.T) {
	t.Parallel()

	provider := NewTestProvider(t)
	req, _ := formatRequest(t, provider, webhookDestination("test-secret"))

	assert.NotEmpty(t, req.Header.Get("x-outpost-signature"))
	assert.Empty(t, req.Header.Get("webhook-signature"))
	assert.Empty(t, req.Header.Get("webhook-id"))
}

func TestCompatSignature_SecretRotation(t *testing.T) {
	t.Parallel()

	current := "whsec_" + base64.StdEncoding.EncodeToString([]byte("current-key-current-key-current!"))
	previous := "whsec_" + base64.StdEncoding.EncodeToString([]byte("previous-key-previous-key-previo"))

	provider := NewTestProvider(t, destwebhook.WithCompatSignature(standardWebhooksCompat()))
	destination := webhookDestination(current, testutil.DestinationFactory.WithCredentials(map[string]string{
		"secret":                     current,
		"previous_secret":            previous,
		"previous_secret_invalid_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	}))
	req, _ := formatRequest(t, provider, destination)

	assert.Len(t, strings.Split(req.Header.Get("webhook-signature"), " "), 2)
	assert.Len(t, strings.Split(strings.TrimPrefix(req.Header.Get("x-outpost-signature"), "v0="), ","), 2)
}

func TestCompatSignature_UndecodablePreviousSecretKeepsCurrent(t *testing.T) {
	t.Parallel()

	current := "whsec_" + base64.StdEncoding.EncodeToString([]byte("current-key-current-key-current!"))

	provider := NewTestProvider(t, destwebhook.WithCompatSignature(standardWebhooksCompat()))
	destination := webhookDestination(current, testutil.DestinationFactory.WithCredentials(map[string]string{
		"secret":                     current,
		"previous_secret":            "not base64!",
		"previous_secret_invalid_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	}))
	req, _ := formatRequest(t, provider, destination)

	assert.Len(t, strings.Split(req.Header.Get("webhook-signature"), " "), 1)
	assert.Len(t, strings.Split(strings.TrimPrefix(req.Header.Get("x-outpost-signature"), "v0="), ","), 2)
}

func TestCompatSignature_UndecodableSecretSkipsCompat(t *testing.T) {
	t.Parallel()

	provider := NewTestProvider(t, destwebhook.WithCompatSignature(standardWebhooksCompat()))
	req, _ := formatRequest(t, provider, webhookDestination("not base64!"))

	assert.NotEmpty(t, req.Header.Get("x-outpost-signature"))
	assert.Empty(t, req.Header.Get("webhook-signature"))
	assert.Empty(t, req.Header.Get("webhook-id"))
}

func TestCompatSignature_ProviderValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*destwebhook.CompatSignatureConfig)
		opts    []destwebhook.Option
		wantErr string
	}{
		{
			name:    "compat header collides with compat signature header",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.Headers["Webhook-Signature"] = "{{.EventID}}" },
			wantErr: "collides with the compat signature header",
		},
		{
			name:    "compat signature header collides with the primary signature header",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.SignatureHeaderName = "X-Outpost-Signature" },
			wantErr: "collides with the primary signature header",
		},
		{
			name:    "compat header collides with a primary system header",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.Headers["x-outpost-timestamp"] = "{{.Timestamp.Unix}}" },
			wantErr: "collides with the primary timestamp header",
		},
		{
			name:    "reserved header name",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.Headers["Content-Type"] = "text/plain" },
			wantErr: "reserved header",
		},
		{
			name:    "invalid header name",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.SignatureHeaderName = "bad header" },
			wantErr: "invalid compat signature header name",
		},
		{
			name:    "content template references a missing field",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.SignatureContentTemplate = "{{.Signatures}}" },
			wantErr: "compat signature",
		},
		{
			name:    "header template references a missing field",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.Headers["webhook-id"] = "{{.Nope}}" },
			wantErr: "compat header template",
		},
		{
			name:    "invalid secret encoding",
			mutate:  func(c *destwebhook.CompatSignatureConfig) { c.SecretEncoding = "base32" },
			wantErr: "invalid secret encoding",
		},
		{
			name:    "invalid primary secret encoding",
			mutate:  func(c *destwebhook.CompatSignatureConfig) {},
			opts:    []destwebhook.Option{destwebhook.WithSecretEncoding("base32", "")},
			wantErr: "invalid secret encoding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := standardWebhooksCompat()
			tt.mutate(cfg)
			opts := append([]destwebhook.Option{
				destwebhook.WithHeaderPrefix(destwebhook.DefaultHeaderPrefix),
				destwebhook.WithSignatureContentTemplate(destwebhook.DefaultSignatureContentTmpl),
				destwebhook.WithSignatureHeaderTemplate(destwebhook.DefaultSignatureHeaderTmpl),
				destwebhook.WithSignatureEncoding(destwebhook.DefaultEncoding),
				destwebhook.WithSignatureAlgorithm(destwebhook.DefaultAlgorithm),
				destwebhook.WithSigningSecretTemplate(destwebhook.DefaultSigningSecretTmpl),
				destwebhook.WithCompatSignature(cfg),
			}, tt.opts...)

			_, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil, opts...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// Header precedence on a name conflict: custom_headers < compat < primary.
func TestCompatSignature_HeaderPrecedence(t *testing.T) {
	t.Parallel()

	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

	t.Run("compat overrides custom headers", func(t *testing.T) {
		t.Parallel()
		provider := NewTestProvider(t, destwebhook.WithCompatSignature(standardWebhooksCompat()))
		destination := webhookDestination(secret, testutil.DestinationFactory.WithConfig(map[string]string{
			"url":            "http://example.com/webhook",
			"custom_headers": `{"webhook-id":"custom"}`,
		}))
		req, _ := formatRequest(t, provider, destination)
		assert.Equal(t, req.Header.Get("x-outpost-event-id"), req.Header.Get("webhook-id"))
	})

}
