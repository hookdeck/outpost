package destwebhook_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type webhookDestinationSuite struct {
	server        *httptest.Server
	request       *http.Request // Capture the request for verification
	requestBody   []byte        // Capture the request body
	responseCode  int           // Configurable response code
	responseDelay time.Duration // Configurable response delay
	webhookURL    string
}

func (suite *webhookDestinationSuite) SetupTest(t *testing.T) {
	// Default response code if not set
	if suite.responseCode == 0 {
		suite.responseCode = http.StatusOK
	}

	suite.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture request for verification
		suite.request = r
		var err error
		suite.requestBody, err = io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Apply configured delay
		if suite.responseDelay > 0 {
			time.Sleep(suite.responseDelay)
		}

		w.WriteHeader(suite.responseCode)
	}))
	suite.webhookURL = suite.server.URL + "/webhook"
}

func (suite *webhookDestinationSuite) TearDownTest(t *testing.T) {
	suite.server.Close()
}

func TestWebhookDestination_Publish(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithData(map[string]interface{}{
			"foo": "bar",
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"key1": "value1",
		}),
	)

	t.Run("should send webhook request without secret", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))
		assert.Empty(t, suite.request.Header.Get("x-outpost-signature"))
		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should send webhook request with one secret", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": `[{"key":"secret1","created_at":"2024-01-01T00:00:00Z"}]`,
			}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))

		// Verify signature
		signature := suite.request.Header.Get("x-outpost-signature")
		require.NotEmpty(t, signature)
		assertValidSignature(t, "secret1", suite.requestBody, signature)

		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should send webhook request with multiple active secrets", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		now := time.Now()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": fmt.Sprintf(`[
					{"key":"secret1","created_at":"%s"},
					{"key":"secret2","created_at":"%s"}
				]`,
					now.Add(-12*time.Hour).Format(time.RFC3339), // Active secret
					now.Add(-6*time.Hour).Format(time.RFC3339),  // Active secret
				),
			}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))

		// Verify signatures
		signatureHeader := suite.request.Header.Get("x-outpost-signature")
		require.NotEmpty(t, signatureHeader)

		fmt.Println("signatureHeader", signatureHeader)

		// Parse "t={timestamp},v0={signature1,signature2}" format
		parts := strings.SplitN(signatureHeader, ",", 2)
		require.True(t, len(parts) >= 2, "signature header should have timestamp and signature parts")

		// First part should be timestamp
		assert.True(t, strings.HasPrefix(parts[0], "t="))

		// Second part should start with v0= and contain all signatures
		assert.True(t, strings.HasPrefix(parts[1], "v0="))
		signatures := strings.Split(strings.TrimPrefix(parts[1], "v0="), ",")
		fmt.Println("signatures", signatures)
		require.Len(t, signatures, 2, "should have signatures from all secrets")

		assertValidSignature(t, "secret1", suite.requestBody, signatureHeader)
		assertValidSignature(t, "secret2", suite.requestBody, signatureHeader)

		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should handle secret rotation", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		now := time.Now()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": fmt.Sprintf(`[
					{"key":"current_secret","created_at":"%s"},
					{"key":"old_secret","created_at":"%s"}
				]`,
					now.Format(time.RFC3339),
					now.Add(-25*time.Hour).Format(time.RFC3339),
				),
			}),
		)

		err := webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))

		// Verify only current secret's signature is present
		signatureHeader := suite.request.Header.Get("x-outpost-signature")
		require.NotEmpty(t, signatureHeader)
		signatures := strings.Split(signatureHeader, " ")
		require.Len(t, signatures, 1)

		assertValidSignature(t, "current_secret", suite.requestBody, signatures[0])

		assert.Equal(t, "value1", suite.request.Header.Get("x-outpost-key1"))
		assert.JSONEq(t, `{"foo":"bar"}`, string(suite.requestBody))
	})

	t.Run("should handle timeout", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{
			responseDelay: 2 * time.Second, // Delay longer than our timeout
		}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		webhookDestination, err := destwebhook.New(
			testutil.Registry.MetadataLoader(),
			destwebhook.WithTimeout(1), // 1 second timeout
		)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
		)

		err = webhookDestination.Publish(context.Background(), &destination, &event)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})
}

func TestWebhookDestination_HeaderPrefix(t *testing.T) {
	t.Parallel()

	t.Run("should use custom header prefix", func(t *testing.T) {
		t.Parallel()

		suite := &webhookDestinationSuite{}
		suite.SetupTest(t)
		defer suite.TearDownTest(t)

		webhookDestination, err := destwebhook.New(
			testutil.Registry.MetadataLoader(),
			destwebhook.WithHeaderPrefix("x-custom-"),
		)
		require.NoError(t, err)

		now := time.Now()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": fmt.Sprintf(`[{"key":"secret1","created_at":"%s"}]`, now.Format(time.RFC3339)),
			}),
		)

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithMetadata(map[string]string{
				"Key1": "Value1",
			}),
		)

		err = webhookDestination.Publish(context.Background(), &destination, &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.NotEmpty(t, suite.request.Header.Get("x-custom-signature"))
		assert.Equal(t, "Value1", suite.request.Header.Get("x-custom-key1"))
	})
}
