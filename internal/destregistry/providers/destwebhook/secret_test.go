package destwebhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func webhookDestination(secret string, opts ...func(*models.Destination)) models.Destination {
	base := []func(*models.Destination){
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": "http://example.com/webhook"}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"secret": secret}),
	}
	return testutil.DestinationFactory.Any(append(base, opts...)...)
}

func formatRequest(t *testing.T, provider *destwebhook.WebhookDestination, destination models.Destination) (*http.Request, string) {
	t.Helper()

	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
	)
	req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
	require.NoError(t, err)

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return req, string(body)
}

func TestPrimarySecretEncoding(t *testing.T) {
	t.Parallel()

	keyBytes := []byte("0123456789abcdef0123456789abcdef")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(keyBytes)

	provider := NewTestProvider(t, destwebhook.WithSecretEncoding(destwebhook.SecretEncodingBase64, "whsec_"))

	t.Run("signs with the decoded key", func(t *testing.T) {
		t.Parallel()
		req, body := formatRequest(t, provider, webhookDestination(secret))

		mac := hmac.New(sha256.New, keyBytes)
		mac.Write([]byte(body))
		assert.Equal(t, "v0="+hex.EncodeToString(mac.Sum(nil)), req.Header.Get("x-outpost-signature"))
	})

	t.Run("rejects a secret it can't decode", func(t *testing.T) {
		t.Parallel()
		destination := webhookDestination("not base64!")
		assertValidationError(t, provider.Validate(context.Background(), &destination), "credentials.secret", "invalid")
	})

	t.Run("rejects a signing secret template the encoding can't decode", func(t *testing.T) {
		t.Parallel()
		_, err := newTestProvider(
			destwebhook.WithSecretEncoding(destwebhook.SecretEncodingHex, ""),
			destwebhook.WithSigningSecretTemplate("whsec_{{.RandomHex}}"),
		)
		require.ErrorContains(t, err, "signing secret template output")
	})

	t.Run("rejects an alphanumeric template under hex encoding", func(t *testing.T) {
		t.Parallel()
		_, err := newTestProvider(
			destwebhook.WithSecretEncoding(destwebhook.SecretEncodingHex, ""),
			destwebhook.WithSigningSecretTemplate("{{.RandomAlphanumeric}}"),
		)
		require.ErrorContains(t, err, "signing secret template output")
	})

	t.Run("accepts a base64 template under base64 encoding", func(t *testing.T) {
		t.Parallel()
		_, err := newTestProvider(
			destwebhook.WithSecretEncoding(destwebhook.SecretEncodingBase64, "whsec_"),
			destwebhook.WithSigningSecretTemplate("whsec_{{.RandomBase64}}"),
		)
		require.NoError(t, err)
	})

	t.Run("accepts a signing secret template the encoding decodes", func(t *testing.T) {
		t.Parallel()
		_, err := newTestProvider(
			destwebhook.WithSecretEncoding(destwebhook.SecretEncodingHex, "whsec_"),
			destwebhook.WithSigningSecretTemplate("whsec_{{.RandomHex}}"),
		)
		require.NoError(t, err)
	})
}

func assertValidationError(t *testing.T, err error, field, errType string) {
	t.Helper()

	var validationErr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &validationErr)
	require.Len(t, validationErr.Errors, 1)
	assert.Equal(t, field, validationErr.Errors[0].Field)
	assert.Equal(t, errType, validationErr.Errors[0].Type)
}
