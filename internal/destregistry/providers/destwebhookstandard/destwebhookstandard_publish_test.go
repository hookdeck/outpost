package destwebhookstandard_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhookstandard"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// StandardWebhookConsumer implements testsuite.MessageConsumer
type StandardWebhookConsumer struct {
	server   *httptest.Server
	messages chan testsuite.Message
	wg       sync.WaitGroup
}

func NewStandardWebhookConsumer() *StandardWebhookConsumer {
	consumer := &StandardWebhookConsumer{
		messages: make(chan testsuite.Message, 100),
	}

	consumer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		consumer.wg.Add(1)
		defer consumer.wg.Done()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Extract all headers as metadata
		metadata := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headerKey := strings.ToLower(k)
				// Map Standard Webhooks headers to expected metadata keys
				// Standard Webhooks prefixes all headers with "webhook-", so we need to strip it
				if strings.HasPrefix(headerKey, "webhook-") {
					// Remove "webhook-" prefix to get the actual metadata key
					metadataKey := strings.TrimPrefix(headerKey, "webhook-")
					// Map to expected keys
					switch metadataKey {
					case "id":
						metadata["event-id"] = v[0]
					case "timestamp":
						metadata["timestamp"] = v[0]
					default:
						metadata[metadataKey] = v[0]
					}
				} else {
					// Keep all non-prefixed headers as-is (user-defined metadata)
					metadata[headerKey] = v[0]
				}
			}
		}

		consumer.messages <- testsuite.Message{
			Data:     body,
			Metadata: metadata,
			Raw:      r, // Store raw request for detailed assertions
		}

		w.WriteHeader(http.StatusOK)
	}))

	return consumer
}

func (c *StandardWebhookConsumer) Consume() <-chan testsuite.Message {
	return c.messages
}

func (c *StandardWebhookConsumer) Close() error {
	c.wg.Wait()
	c.server.Close()
	close(c.messages)
	return nil
}

// StandardWebhookAsserter implements testsuite.MessageAsserter
type StandardWebhookAsserter struct {
	secret             string
	expectedSignatures int
	headerPrefix       string // Defaults to "webhook-"
}

func (a *StandardWebhookAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	req := msg.Raw.(*http.Request)

	// Verify HTTP properties
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	// Use configured prefix or default to "webhook-"
	prefix := a.headerPrefix
	if prefix == "" {
		prefix = "webhook-"
	}

	// Verify Standard Webhooks headers with configured prefix
	webhookID := req.Header.Get(prefix + "id")
	assert.NotEmpty(t, webhookID, prefix+"id should be present")
	// Note: webhook-id format depends on event.ID format (user-provided)

	webhookTimestamp := req.Header.Get(prefix + "timestamp")
	assert.NotEmpty(t, webhookTimestamp, prefix+"timestamp should be present")
	testsuite.AssertTimestampIsUnixSeconds(t, webhookTimestamp)

	webhookSignature := req.Header.Get(prefix + "signature")
	assert.NotEmpty(t, webhookSignature, prefix+"signature should be present")

	// Verify signature format and count
	assertSignatureFormat(t, webhookSignature, a.expectedSignatures)

	// Verify signature with known secret (if provided)
	if a.secret != "" {
		assertValidStandardWebhookSignature(t, a.secret, webhookID, webhookTimestamp, msg.Data, webhookSignature)
	}
}

// StandardWebhookPublishSuite is the test suite
type StandardWebhookPublishSuite struct {
	testsuite.PublisherSuite
	consumer *StandardWebhookConsumer
	setupFn  func(*StandardWebhookPublishSuite)
}

func (s *StandardWebhookPublishSuite) SetupSuite() {
	s.setupFn(s)
}

func (s *StandardWebhookPublishSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
}

// Basic publish test configuration
func (s *StandardWebhookPublishSuite) setupBasicSuite() {
	consumer := NewStandardWebhookConsumer()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(s.T(), err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &StandardWebhookAsserter{
			secret:             "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			expectedSignatures: 1,
		},
	})

	s.consumer = consumer
}

// Multiple secrets test configuration
func (s *StandardWebhookPublishSuite) setupMultipleSecretsSuite() {
	consumer := NewStandardWebhookConsumer()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(s.T(), err)

	now := time.Now()
	invalidAt := now.Add(24 * time.Hour)
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret":                     "whsec_TmV3U2VjcmV0QmFzZTY0RW5jb2RlZFN0cmluZzEyMw==",
			"previous_secret":            "whsec_T2xkU2VjcmV0QmFzZTY0RW5jb2RlZFN0cmluZzEyMw==",
			"previous_secret_invalid_at": invalidAt.Format(time.RFC3339),
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &StandardWebhookAsserter{
			secret:             "whsec_TmV3U2VjcmV0QmFzZTY0RW5jb2RlZFN0cmluZzEyMw==",
			expectedSignatures: 2,
		},
	})

	s.consumer = consumer
}

// Expired secrets test configuration
func (s *StandardWebhookPublishSuite) setupExpiredSecretsSuite() {
	consumer := NewStandardWebhookConsumer()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(s.T(), err)

	now := time.Now()
	invalidAt := now.Add(-1 * time.Hour) // Previous secret is already invalid
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret":                     "whsec_QWN0aXZlU2VjcmV0QmFzZTY0RW5jb2RlZFN0cmluZzEyMw==",
			"previous_secret":            "whsec_RXhwaXJlZFNlY3JldEJhc2U2NEVuY29kZWRTdHJpbmcxMjM=",
			"previous_secret_invalid_at": invalidAt.Format(time.RFC3339),
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &StandardWebhookAsserter{
			secret:             "whsec_QWN0aXZlU2VjcmV0QmFzZTY0RW5jb2RlZFN0cmluZzEyMw==",
			expectedSignatures: 1, // Only expect signature from active secret
		},
	})

	s.consumer = consumer
}

func TestStandardWebhookPublish(t *testing.T) {
	t.Parallel()

	// Run basic publish tests
	t.Run("Basic", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &StandardWebhookPublishSuite{
			setupFn: (*StandardWebhookPublishSuite).setupBasicSuite,
		})
	})

	// Run multiple secrets tests
	t.Run("MultipleSecrets", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &StandardWebhookPublishSuite{
			setupFn: (*StandardWebhookPublishSuite).setupMultipleSecretsSuite,
		})
	})

	// Run expired secrets tests
	t.Run("ExpiredSecrets", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &StandardWebhookPublishSuite{
			setupFn: (*StandardWebhookPublishSuite).setupExpiredSecretsSuite,
		})
	})
}

func TestStandardWebhookPublisher_SignatureFormat(t *testing.T) {
	t.Parallel()

	consumer := NewStandardWebhookConsumer()
	defer consumer.Close()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
		}),
	)

	publisher, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("msg_2KWPBgLlAfxdpx2AI54pPJ85f4W"),
		testutil.EventFactory.WithData(map[string]interface{}{"hello": "world"}),
	)

	_, err = publisher.Publish(context.Background(), &event)
	require.NoError(t, err)

	// Get the message
	select {
	case msg := <-consumer.Consume():
		req := msg.Raw.(*http.Request)

		// Verify signature format is "v1,<base64>"
		signatureHeader := req.Header.Get("webhook-signature")
		assert.True(t, strings.HasPrefix(signatureHeader, "v1,"))

		// Verify base64
		sigPart := strings.TrimPrefix(signatureHeader, "v1,")
		decoded, err := base64.StdEncoding.DecodeString(sigPart)
		assert.NoError(t, err)
		assert.Equal(t, 32, len(decoded)) // HMAC-SHA256 produces 32 bytes

	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestStandardWebhookPublisher_MessageIDFormat(t *testing.T) {
	t.Parallel()

	consumer := NewStandardWebhookConsumer()
	defer consumer.Close()

	provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
		}),
	)

	publisher, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("msg_2KWPBgLlAfxdpx2AI54pPJ85f4W"),
		testutil.EventFactory.WithData(map[string]interface{}{"test": "data"}),
	)

	_, err = publisher.Publish(context.Background(), &event)
	require.NoError(t, err)

	// Get the message
	select {
	case msg := <-consumer.Consume():
		req := msg.Raw.(*http.Request)

		// Verify webhook-id uses event ID directly and has msg_ prefix
		webhookID := req.Header.Get("webhook-id")
		assert.NotEmpty(t, webhookID)
		assert.Equal(t, event.ID, webhookID)
		assert.True(t, strings.HasPrefix(webhookID, "msg_"), "webhook-id should have msg_ prefix, got: %s", webhookID)

	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestStandardWebhookPublisher_CustomHeaderPrefix(t *testing.T) {
	t.Parallel()

	consumer := NewStandardWebhookConsumer()
	defer consumer.Close()

	// Create provider with custom header prefix
	provider, err := destwebhookstandard.New(
		testutil.Registry.MetadataLoader(),
		nil,
		destwebhookstandard.WithHeaderPrefix("x-custom-"),
	)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
		}),
	)

	publisher, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("msg_2KWPBgLlAfxdpx2AI54pPJ85f4W"),
		testutil.EventFactory.WithData(map[string]interface{}{"test": "data"}),
		testutil.EventFactory.WithTopic("user.created"),
	)

	_, err = publisher.Publish(context.Background(), &event)
	require.NoError(t, err)

	// Get the message
	select {
	case msg := <-consumer.Consume():
		req := msg.Raw.(*http.Request)

		// Verify ALL headers use custom prefix (including Standard Webhooks headers)
		assert.NotEmpty(t, req.Header.Get("x-custom-id"), "should have x-custom-id header")
		assert.NotEmpty(t, req.Header.Get("x-custom-timestamp"), "should have x-custom-timestamp header")
		assert.NotEmpty(t, req.Header.Get("x-custom-signature"), "should have x-custom-signature header")
		assert.NotEmpty(t, req.Header.Get("x-custom-topic"), "should have x-custom-topic header")
		assert.Equal(t, "user.created", req.Header.Get("x-custom-topic"))

		// Verify default prefix is NOT used
		assert.Empty(t, req.Header.Get("webhook-id"), "should not have webhook-id header")
		assert.Empty(t, req.Header.Get("webhook-timestamp"), "should not have webhook-timestamp header")
		assert.Empty(t, req.Header.Get("webhook-signature"), "should not have webhook-signature header")
		assert.Empty(t, req.Header.Get("webhook-topic"), "should not have webhook-topic header")

	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestStandardWebhookPublisher_CustomHeaders(t *testing.T) {
	t.Parallel()

	t.Run("should include custom headers in request", func(t *testing.T) {
		t.Parallel()

		consumer := NewStandardWebhookConsumer()
		defer consumer.Close()

		provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            consumer.server.URL + "/webhook",
				"custom_headers": `{"x-api-key":"secret123","x-tenant-id":"tenant-abc"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &dest)
		require.NoError(t, err)
		defer publisher.Close()

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		_, err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		select {
		case msg := <-consumer.Consume():
			req := msg.Raw.(*http.Request)
			assert.Equal(t, "secret123", req.Header.Get("x-api-key"))
			assert.Equal(t, "tenant-abc", req.Header.Get("x-tenant-id"))
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run("should allow metadata to override custom headers", func(t *testing.T) {
		t.Parallel()

		consumer := NewStandardWebhookConsumer()
		defer consumer.Close()

		provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            consumer.server.URL + "/webhook",
				"custom_headers": `{"webhook-source":"custom-value"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
			testutil.DestinationFactory.WithDeliveryMetadata(map[string]string{
				"source": "delivery-metadata-value",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &dest)
		require.NoError(t, err)
		defer publisher.Close()

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		_, err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		select {
		case msg := <-consumer.Consume():
			req := msg.Raw.(*http.Request)
			// Metadata should override custom headers (metadata adds prefix webhook-)
			assert.Equal(t, "delivery-metadata-value", req.Header.Get("webhook-source"))
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run("should fail CreatePublisher when custom_headers is empty object", func(t *testing.T) {
		t.Parallel()

		provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "http://example.com/webhook",
				"custom_headers": `{}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		_, err = provider.CreatePublisher(context.Background(), &dest)
		require.Error(t, err)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.custom_headers", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should work without custom_headers field", func(t *testing.T) {
		t.Parallel()

		consumer := NewStandardWebhookConsumer()
		defer consumer.Close()

		provider, err := destwebhookstandard.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": consumer.server.URL + "/webhook",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &dest)
		require.NoError(t, err)
		defer publisher.Close()

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		_, err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		select {
		case msg := <-consumer.Consume():
			req := msg.Raw.(*http.Request)
			// Should still have standard headers
			assert.NotEmpty(t, req.Header.Get("webhook-id"))
			assert.NotEmpty(t, req.Header.Get("webhook-timestamp"))
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})
}
