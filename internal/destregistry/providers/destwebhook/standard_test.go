package destwebhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Standard Webhooks preset, as config resolves "standard" mode: the
// provider gets the Standard* values and the event ID header pinned to
// "<prefix>id".
func standardOptions(prefix string) []destwebhook.Option {
	return []destwebhook.Option{
		destwebhook.WithHeaderPrefix(prefix),
		destwebhook.WithEventIDHeader(strings.TrimSpace(prefix)+destwebhook.StandardEventIDHeaderKey, false),
		destwebhook.WithTimestampFormat(destwebhook.StandardTimestampFormat),
		destwebhook.WithSignatureContentTemplate(destwebhook.StandardSignatureContentTmpl),
		destwebhook.WithSignatureHeaderTemplate(destwebhook.StandardSignatureHeaderTmpl),
		destwebhook.WithSignatureEncoding(destwebhook.StandardEncoding),
		destwebhook.WithSecretEncoding(destwebhook.StandardSecretEncoding, destwebhook.StandardSecretPrefix),
		destwebhook.WithSigningSecretTemplate(destwebhook.StandardSigningSecretTmpl),
	}
}

func newStandardProvider(t *testing.T, opts ...destwebhook.Option) *destwebhook.WebhookDestination {
	t.Helper()
	return NewTestProvider(t, append(standardOptions(destwebhook.StandardHeaderPrefix), opts...)...)
}

const (
	standardSecret         = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	standardPreviousSecret = "whsec_T2xkU2VjcmV0U3RyaW5nMTIz"
)

func standardDestination(credentials map[string]string, opts ...func(*models.Destination)) models.Destination {
	base := []func(*models.Destination){
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": "http://example.com/webhook"}),
		testutil.DestinationFactory.WithCredentials(credentials),
	}
	return testutil.DestinationFactory.Any(append(base, opts...)...)
}

func formatStandard(t *testing.T, provider *destwebhook.WebhookDestination, destination models.Destination, event models.Event) *http.Request {
	t.Helper()
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()
	req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
	require.NoError(t, err)
	return req
}

func requestBody(t *testing.T, req *http.Request) []byte {
	t.Helper()
	body, err := req.GetBody()
	require.NoError(t, err)
	data := make([]byte, 0, 256)
	buf := make([]byte, 256)
	for {
		n, err := body.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil {
			break
		}
	}
	return data
}

// assertStandardWebhooksSDK verifies the request with the official SDK, the
// way a receiver would.
func assertStandardWebhooksSDK(t *testing.T, secret string, req *http.Request) {
	t.Helper()
	wh, err := standardwebhooks.NewWebhook(secret)
	require.NoError(t, err)
	assert.NoError(t, wh.Verify(requestBody(t, req), req.Header), "Standard Webhooks SDK should verify with %s", secret)
}

func TestStandardPreset_VerifiesWithSDK(t *testing.T) {
	t.Parallel()

	provider := newStandardProvider(t)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("msg_2KWPBgLlAfxdpx2AI54pPJ85f4W"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithDataMap(map[string]interface{}{"hello": "world"}),
	)

	t.Run("single secret", func(t *testing.T) {
		before := time.Now()
		req := formatStandard(t, provider, standardDestination(map[string]string{"secret": standardSecret}), event)

		assert.Equal(t, "msg_2KWPBgLlAfxdpx2AI54pPJ85f4W", req.Header.Get("webhook-id"))
		assert.Empty(t, req.Header.Get("webhook-event-id"))
		seconds, err := strconv.ParseInt(req.Header.Get("webhook-timestamp"), 10, 64)
		require.NoError(t, err, "webhook-timestamp must be unix seconds")
		assert.InDelta(t, before.Unix(), seconds, 2)
		assert.Equal(t, "user.created", req.Header.Get("webhook-topic"))

		signature := req.Header.Get("webhook-signature")
		require.True(t, strings.HasPrefix(signature, "v1,"), "signature %q", signature)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(signature, "v1,"))
		require.NoError(t, err)
		assert.Len(t, raw, sha256.Size)

		assertStandardWebhooksSDK(t, standardSecret, req)
	})

	t.Run("rotation signs with both secrets", func(t *testing.T) {
		req := formatStandard(t, provider, standardDestination(map[string]string{
			"secret":                     standardSecret,
			"previous_secret":            standardPreviousSecret,
			"previous_secret_invalid_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		}), event)

		signatures := strings.Split(req.Header.Get("webhook-signature"), " ")
		assert.Len(t, signatures, 2)
		for _, sig := range signatures {
			assert.True(t, strings.HasPrefix(sig, "v1,"), "signature %q", sig)
		}
		assertStandardWebhooksSDK(t, standardSecret, req)
		assertStandardWebhooksSDK(t, standardPreviousSecret, req)
	})

	t.Run("expired previous secret is dropped", func(t *testing.T) {
		req := formatStandard(t, provider, standardDestination(map[string]string{
			"secret":                     standardSecret,
			"previous_secret":            standardPreviousSecret,
			"previous_secret_invalid_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		}), event)

		assert.Len(t, strings.Split(req.Header.Get("webhook-signature"), " "), 1)
		assertStandardWebhooksSDK(t, standardSecret, req)
	})
}

func TestStandardPreset_HeaderPrefix(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		prefix string
		want   string
	}{
		{"default prefix", "webhook-", "webhook-"},
		{"custom prefix", "x-custom-", "x-custom-"},
		{"whitespace disables the prefix", "  ", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewTestProvider(t, standardOptions(tt.prefix)...)
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithID("msg_test123"),
				testutil.EventFactory.WithTopic("user.created"),
				testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
			)
			req := formatStandard(t, provider, standardDestination(map[string]string{"secret": standardSecret}), event)

			assert.Equal(t, "msg_test123", req.Header.Get(tt.want+"id"))
			assert.NotEmpty(t, req.Header.Get(tt.want+"timestamp"))
			assert.NotEmpty(t, req.Header.Get(tt.want+"signature"))
			assert.Equal(t, "user.created", req.Header.Get(tt.want+"topic"))
			if tt.want != "webhook-" {
				assert.Empty(t, req.Header.Get("webhook-id"))
				assert.Empty(t, req.Header.Get("webhook-signature"))
			}
		})
	}
}

func TestStandardPreset_MetadataHeadersArePrefixed(t *testing.T) {
	t.Parallel()

	provider := newStandardProvider(t)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithMetadata(map[string]string{"source": "crm"}),
		testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
	)
	req := formatStandard(t, provider, standardDestination(map[string]string{"secret": standardSecret}), event)

	assert.Equal(t, "crm", req.Header.Get("webhook-source"))
	assert.Empty(t, req.Header.Get("source"), "event metadata is sent under the prefix only")
}

func TestStandardPreset_SecretValidation(t *testing.T) {
	t.Parallel()

	provider := newStandardProvider(t)

	assertInvalid := func(t *testing.T, credentials map[string]string, field string) {
		t.Helper()
		destination := standardDestination(credentials)
		err := provider.Validate(context.Background(), &destination)
		var validationErr *destregistry.ErrDestinationValidation
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, field, validationErr.Errors[0].Field)
		assert.Equal(t, "invalid", validationErr.Errors[0].Type)
	}

	t.Run("secret that is not base64", func(t *testing.T) {
		assertInvalid(t, map[string]string{"secret": "whsec_not-valid-base64!!!"}, "credentials.secret")
	})

	t.Run("previous secret that is not base64", func(t *testing.T) {
		assertInvalid(t, map[string]string{
			"secret":                     standardSecret,
			"previous_secret":            "not-a-whsec-secret",
			"previous_secret_invalid_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		}, "credentials.previous_secret")
	})

	t.Run("valid secrets", func(t *testing.T) {
		destination := standardDestination(map[string]string{
			"secret":                     standardSecret,
			"previous_secret":            standardPreviousSecret,
			"previous_secret_invalid_at": "2024-01-02T00:00:00Z",
		})
		assert.NoError(t, provider.Validate(context.Background(), &destination))
	})
}

func TestStandardPreset_GeneratedSecret(t *testing.T) {
	t.Parallel()

	provider := newStandardProvider(t)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": "https://example.com"}),
	)
	require.NoError(t, provider.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"}))

	secret := destination.Credentials["secret"]
	require.True(t, strings.HasPrefix(secret, "whsec_"), "secret %q", secret)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	require.NoError(t, err)
	assert.Len(t, raw, 32)
	assert.NoError(t, provider.Validate(context.Background(), &destination))

	event := testutil.EventFactory.Any(testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}))
	assertStandardWebhooksSDK(t, secret, formatStandard(t, provider, destination, event))
}

// The compat signature is a second header set, so it works in standard mode
// the same way: here the previous scheme is Outpost's default one.
func TestStandardPreset_CompatSignature(t *testing.T) {
	t.Parallel()

	provider := newStandardProvider(t, destwebhook.WithCompatSignature(&destwebhook.CompatSignatureConfig{
		SignatureHeaderName:      "x-outpost-signature",
		SignatureContentTemplate: destwebhook.DefaultSignatureContentTmpl,
		SignatureHeaderTemplate:  destwebhook.DefaultSignatureHeaderTmpl,
		SignatureEncoding:        destwebhook.DefaultEncoding,
		SignatureAlgorithm:       destwebhook.DefaultAlgorithm,
	}))
	event := testutil.EventFactory.Any(testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}))
	req := formatStandard(t, provider, standardDestination(map[string]string{"secret": standardSecret}), event)

	assertStandardWebhooksSDK(t, standardSecret, req)

	mac := hmac.New(sha256.New, []byte(standardSecret)) // raw secret encoding: the stored string is the key
	mac.Write(requestBody(t, req))
	assert.Equal(t, "v0="+hex.EncodeToString(mac.Sum(nil)), req.Header.Get("x-outpost-signature"))
}
