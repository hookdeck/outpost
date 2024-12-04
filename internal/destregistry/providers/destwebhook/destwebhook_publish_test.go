package destwebhook_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	request       *http.Request
	requestBody   []byte
	responseCode  int
	responseDelay time.Duration
	webhookURL    string
}

func (suite *webhookDestinationSuite) SetupTest(t *testing.T) {
	if suite.responseCode == 0 {
		suite.responseCode = http.StatusOK
	}

	suite.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		suite.request = r
		var err error
		suite.requestBody, err = io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

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

func TestWebhookDestination_DefaultBehavior(t *testing.T) {
	t.Parallel()

	suite := &webhookDestinationSuite{}
	suite.SetupTest(t)
	defer suite.TearDownTest(t)

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithData(map[string]interface{}{
			"test_key": "test_value",
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"meta_key": "meta_value",
		}),
	)

	t.Run("should send data and metadata correctly", func(t *testing.T) {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
		)

		expectedData := map[string]interface{}{
			"test_key": "test_value",
			"nested": map[string]interface{}{
				"field": "value",
			},
		}
		expectedMetadata := map[string]string{
			"meta_key1": "meta_value1",
			"meta_key2": "meta_value2",
		}

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(expectedData),
			testutil.EventFactory.WithMetadata(expectedMetadata),
		)

		publisher, err := webhookDestination.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Equal(t, "POST", suite.request.Method)
		assert.Equal(t, "/webhook", suite.request.URL.Path)
		assert.Equal(t, "application/json", suite.request.Header.Get("Content-Type"))
		assertRequestContent(t, suite.requestBody, expectedData, expectedMetadata, "x-outpost-", suite.request)
	})

	t.Run("should send webhook request without secret", func(t *testing.T) {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
		)

		publisher, err := webhookDestination.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		assert.Empty(t, suite.request.Header.Get("x-outpost-signature"))
	})

	t.Run("should send webhook request with one secret", func(t *testing.T) {
		now := time.Now()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": fmt.Sprintf(`[{"key":"secret1","created_at":"%s"}]`,
					now.Format(time.RFC3339)),
			}),
		)

		publisher, err := webhookDestination.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		signatureHeader := suite.request.Header.Get("x-outpost-signature")
		assertSignatureFormat(t, signatureHeader, 1)
		assertValidSignature(t, "secret1", suite.requestBody, signatureHeader)
	})

	t.Run("should send webhook request with multiple active secrets", func(t *testing.T) {
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

		publisher, err := webhookDestination.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		signatureHeader := suite.request.Header.Get("x-outpost-signature")
		assertSignatureFormat(t, signatureHeader, 2)

		// Verify both signatures are valid
		assertValidSignature(t, "secret1", suite.requestBody, signatureHeader)
		assertValidSignature(t, "secret2", suite.requestBody, signatureHeader)
	})

	t.Run("should handle multiple secrets with expiration", func(t *testing.T) {
		now := time.Now()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": suite.webhookURL,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secrets": fmt.Sprintf(`[
                    {"key":"expired_secret","created_at":"%s"},
                    {"key":"active_secret1","created_at":"%s"},
                    {"key":"active_secret2","created_at":"%s"}
                ]`,
					now.Add(-48*time.Hour).Format(time.RFC3339), // Expired secret (> 24h old)
					now.Add(-12*time.Hour).Format(time.RFC3339), // Active secret
					now.Add(-6*time.Hour).Format(time.RFC3339),  // Active secret
				),
			}),
		)

		publisher, err := webhookDestination.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		require.NotNil(t, suite.request)
		signatureHeader := suite.request.Header.Get("x-outpost-signature")
		assertSignatureFormat(t, signatureHeader, 2)

		// Verify only active signatures are present
		assertValidSignature(t, "active_secret1", suite.requestBody, signatureHeader)
		assertValidSignature(t, "active_secret2", suite.requestBody, signatureHeader)
	})
}

func TestWebhookDestination_CustomHeaderPrefix(t *testing.T) {
	t.Parallel()

	suite := &webhookDestinationSuite{}
	suite.SetupTest(t)
	defer suite.TearDownTest(t)

	webhookDestination, err := destwebhook.New(
		testutil.Registry.MetadataLoader(),
		destwebhook.WithHeaderPrefix("x-custom-"),
	)
	require.NoError(t, err)

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithMetadata(map[string]string{
			"meta_key": "meta_value",
		}),
	)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": suite.webhookURL,
		}),
	)

	publisher, err := webhookDestination.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	err = publisher.Publish(context.Background(), &event)
	require.NoError(t, err)

	require.NotNil(t, suite.request)
	assert.Equal(t, "meta_value", suite.request.Header.Get("x-custom-meta_key"))
}

func TestWebhookDestination_Timeout(t *testing.T) {
	t.Parallel()

	suite := &webhookDestinationSuite{
		responseDelay: 2 * time.Second,
	}
	suite.SetupTest(t)
	defer suite.TearDownTest(t)

	webhookDestination, err := destwebhook.New(
		testutil.Registry.MetadataLoader(),
		destwebhook.WithTimeout(1), // 1 second timeout
	)
	require.NoError(t, err)

	event := testutil.EventFactory.Any()
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": suite.webhookURL,
		}),
	)

	publisher, err := webhookDestination.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	err = publisher.Publish(context.Background(), &event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

// Helper function to assert webhook request content
func assertRequestContent(t *testing.T, rawBody []byte, expectedData map[string]interface{}, expectedMetadata map[string]string, headerPrefix string, request *http.Request) {
	t.Helper()

	// Verify body content
	var actualBody map[string]interface{}
	err := json.Unmarshal(rawBody, &actualBody)
	require.NoError(t, err, "body should be valid JSON")
	assert.Equal(t, expectedData, actualBody, "request body should match expected data")

	// Verify metadata in headers
	for key, value := range expectedMetadata {
		assert.Equal(t, value, request.Header.Get(headerPrefix+key),
			"metadata header %s should match expected value", key)
	}
}

// Helper function to assert signature format
func assertSignatureFormat(t *testing.T, signatureHeader string, expectedSignatureCount int) {
	t.Helper()

	parts := strings.SplitN(signatureHeader, ",", 2)
	require.True(t, len(parts) >= 2, "signature header should have timestamp and signature parts")

	// Verify timestamp format
	assert.True(t, strings.HasPrefix(parts[0], "t="), "should start with t=")
	timestampStr := strings.TrimPrefix(parts[0], "t=")
	_, err := strconv.ParseInt(timestampStr, 10, 64)
	require.NoError(t, err, "timestamp should be a valid integer")

	// Verify signature format and count
	assert.True(t, strings.HasPrefix(parts[1], "v0="), "should start with v0=")
	signatures := strings.Split(strings.TrimPrefix(parts[1], "v0="), ",")
	assert.Len(t, signatures, expectedSignatureCount, "should have exact number of signatures")
}
