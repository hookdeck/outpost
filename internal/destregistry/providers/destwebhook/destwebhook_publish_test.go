package destwebhook_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// WebhookConsumer implements testsuite.MessageConsumer
type WebhookConsumer struct {
	server       *httptest.Server
	messages     chan testsuite.Message
	headerPrefix string
	wg           sync.WaitGroup
}

func NewWebhookConsumer(headerPrefix string) *WebhookConsumer {
	consumer := &WebhookConsumer{
		messages:     make(chan testsuite.Message, 100),
		headerPrefix: headerPrefix,
	}

	consumer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		consumer.wg.Add(1)
		defer consumer.wg.Done()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Convert headers to metadata
		metadata := make(map[string]string)
		for k, v := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), strings.ToLower(headerPrefix)) {
				metadata[strings.TrimPrefix(strings.ToLower(k), strings.ToLower(headerPrefix))] = v[0]
			}
		}

		consumer.messages <- testsuite.Message{
			Data:     body,
			Metadata: metadata,
			Raw:      r, // Store the raw request for signature verification
		}

		w.WriteHeader(http.StatusOK)
	}))

	return consumer
}

func (c *WebhookConsumer) Consume() <-chan testsuite.Message {
	return c.messages
}

func (c *WebhookConsumer) Close() error {
	c.wg.Wait()
	c.server.Close()
	close(c.messages)
	return nil
}

// WebhookAsserter implements testsuite.MessageAsserter
type WebhookAsserter struct {
	headerPrefix       string
	expectedSignatures int
	secrets            []string
}

func (a *WebhookAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	req := msg.Raw.(*http.Request)

	// Verify basic HTTP properties
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "/webhook", req.URL.Path)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	// Verify default headers
	timestampHeader := req.Header.Get(a.headerPrefix + "timestamp")
	assert.NotEmpty(t, timestampHeader, "timestamp header should be present")

	// Verify timestamp is in Unix seconds (not milliseconds)
	testsuite.AssertTimestampIsUnixSeconds(t, timestampHeader)

	assert.Equal(t, event.ID, req.Header.Get(a.headerPrefix+"event-id"), "event-id header should match")
	assert.Equal(t, event.Topic, req.Header.Get(a.headerPrefix+"topic"), "topic header should match")

	// Verify request content and metadata
	for k, v := range event.Metadata {
		assert.Equal(t, v, msg.Metadata[k], "metadata key %s should match expected value", k)
	}

	// Verify signature if expected
	if a.expectedSignatures > 0 {
		signatureHeader := req.Header.Get(a.headerPrefix + "signature")
		assertSignatureFormat(t, signatureHeader, a.expectedSignatures)

		// Verify timestamp in signature header matches the timestamp header
		signatureParts := strings.SplitN(signatureHeader, ",", 2)
		if len(signatureParts) >= 2 {
			signatureTimestampStr := strings.TrimPrefix(signatureParts[0], "t=")
			assert.Equal(t, timestampHeader, signatureTimestampStr, "timestamp in signature header should match timestamp header")
		}

		// Verify each expected signature
		for _, secret := range a.secrets {
			assertValidSignature(t, secret, msg.Data, signatureHeader)
		}
	} else {
		// Verify no signature when not expected
		assert.Empty(t, req.Header.Get(a.headerPrefix+"signature"))
	}
}

// WebhookPublishSuite is the test suite for webhook publisher
type WebhookPublishSuite struct {
	testsuite.PublisherSuite
	consumer *WebhookConsumer
	setupFn  func(*WebhookPublishSuite)
}

func (s *WebhookPublishSuite) SetupSuite() {
	s.setupFn(s)
}

func (s *WebhookPublishSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
}

// Basic publish test configuration
func (s *WebhookPublishSuite) setupBasicSuite() {
	consumer := NewWebhookConsumer("x-outpost-")

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(s.T(), err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "test-secret",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix:       "x-outpost-",
			expectedSignatures: 1,
			secrets:            []string{"test-secret"},
		},
	})

	s.consumer = consumer
}

// Single secret test configuration
func (s *WebhookPublishSuite) setupSingleSecretSuite() {
	consumer := NewWebhookConsumer("x-outpost-")

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(s.T(), err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "secret1",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix:       "x-outpost-",
			expectedSignatures: 1,
			secrets:            []string{"secret1"},
		},
	})

	s.consumer = consumer
}

// Multiple secrets test configuration
func (s *WebhookPublishSuite) setupMultipleSecretsSuite() {
	consumer := NewWebhookConsumer("x-outpost-")

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(s.T(), err)

	now := time.Now()
	invalidAt := now.Add(24 * time.Hour)
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret":                     "secret2",
			"previous_secret":            "secret1",
			"previous_secret_invalid_at": invalidAt.Format(time.RFC3339),
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix:       "x-outpost-",
			expectedSignatures: 2,
			secrets:            []string{"secret2", "secret1"},
		},
	})

	s.consumer = consumer
}

// Expired secrets test configuration
func (s *WebhookPublishSuite) setupExpiredSecretsSuite() {
	consumer := NewWebhookConsumer("x-outpost-")

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(s.T(), err)

	now := time.Now()
	invalidAt := now.Add(-1 * time.Hour) // Previous secret is already invalid
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret":                     "active_secret",
			"previous_secret":            "expired_secret",
			"previous_secret_invalid_at": invalidAt.Format(time.RFC3339),
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix:       "x-outpost-",
			expectedSignatures: 1, // Only expect signature from active secret
			secrets:            []string{"active_secret"},
		},
	})

	s.consumer = consumer
}

// Custom header prefix test configuration
func (s *WebhookPublishSuite) setupCustomHeaderSuite() {
	const customPrefix = "x-custom-"
	consumer := NewWebhookConsumer(customPrefix)

	provider, err := destwebhook.New(
		testutil.Registry.MetadataLoader(),
		nil,
		destwebhook.WithHeaderPrefix(customPrefix),
	)
	require.NoError(s.T(), err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "test-secret",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix:       customPrefix,
			expectedSignatures: 1,
			secrets:            []string{"test-secret"},
		},
	})

	s.consumer = consumer
}

func TestWebhookPublish(t *testing.T) {
	t.Parallel()
	testutil.CheckIntegrationTest(t)

	// Run basic publish tests
	t.Run("Basic", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &WebhookPublishSuite{
			setupFn: (*WebhookPublishSuite).setupBasicSuite,
		})
	})

	// Run single secret tests
	t.Run("SingleSecret", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &WebhookPublishSuite{
			setupFn: (*WebhookPublishSuite).setupSingleSecretSuite,
		})
	})

	// Run multiple secrets tests
	t.Run("MultipleSecrets", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &WebhookPublishSuite{
			setupFn: (*WebhookPublishSuite).setupMultipleSecretsSuite,
		})
	})

	// Run expired secrets tests
	t.Run("ExpiredSecrets", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &WebhookPublishSuite{
			setupFn: (*WebhookPublishSuite).setupExpiredSecretsSuite,
		})
	})

	// Run custom header prefix tests
	t.Run("CustomHeaderPrefix", func(t *testing.T) {
		t.Parallel()
		suite.Run(t, &WebhookPublishSuite{
			setupFn: (*WebhookPublishSuite).setupCustomHeaderSuite,
		})
	})
}

func TestWebhookPublisher_DisableDefaultHeaders(t *testing.T) {
	tests := []struct {
		name           string
		options        []destwebhook.Option
		expectedHeader string
		shouldExist    bool
	}{
		{
			name:           "disable event id header",
			options:        []destwebhook.Option{destwebhook.WithDisableDefaultEventIDHeader(true)},
			expectedHeader: "x-outpost-event-id",
			shouldExist:    false,
		},
		{
			name:           "disable signature header",
			options:        []destwebhook.Option{destwebhook.WithDisableDefaultSignatureHeader(true)},
			expectedHeader: "x-outpost-signature",
			shouldExist:    false,
		},
		{
			name:           "disable timestamp header",
			options:        []destwebhook.Option{destwebhook.WithDisableDefaultTimestampHeader(true)},
			expectedHeader: "x-outpost-timestamp",
			shouldExist:    false,
		},
		{
			name:           "disable topic header",
			options:        []destwebhook.Option{destwebhook.WithDisableDefaultTopicHeader(true)},
			expectedHeader: "x-outpost-topic",
			shouldExist:    false,
		},
		{
			name:           "default headers enabled",
			options:        []destwebhook.Option{},
			expectedHeader: "x-outpost-event-id",
			shouldExist:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil, tt.options...)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("webhook"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"url": "http://example.com",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"secret": "test-secret",
				}),
			)

			publisher, err := dest.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
			)

			req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
			require.NoError(t, err)

			if tt.shouldExist {
				assert.NotEmpty(t, req.Header.Get(tt.expectedHeader))
			} else {
				assert.Empty(t, req.Header.Get(tt.expectedHeader))
			}
		})
	}
}

func TestWebhookPublisher_DeliveryMetadata(t *testing.T) {
	t.Parallel()

	consumer := NewWebhookConsumer("x-outpost-")
	defer consumer.Close()

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "test-secret",
		}),
		testutil.DestinationFactory.WithDeliveryMetadata(map[string]string{
			"app-id":    "my-app",
			"source":    "outpost-delivery",
			"x-api-key": "secret-api-key",
			"timestamp": "999", // Should override system timestamp
		}),
	)

	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithMetadata(map[string]string{
			"user-id": "usr_456",
			"source":  "user-service", // Should override delivery_metadata source
		}),
		testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
	)

	_, err = publisher.Publish(context.Background(), &event)
	require.NoError(t, err)

	select {
	case msg := <-consumer.Consume():
		req := msg.Raw.(*http.Request)

		// Verify delivery_metadata headers are present
		assert.Equal(t, "my-app", req.Header.Get("x-outpost-app-id"), "app-id from delivery_metadata should be present")
		assert.Equal(t, "secret-api-key", req.Header.Get("x-outpost-x-api-key"), "x-api-key from delivery_metadata should be present")

		// Verify merge priority: delivery_metadata overrides system timestamp
		assert.Equal(t, "999", req.Header.Get("x-outpost-timestamp"), "delivery_metadata timestamp should override system timestamp")

		// Verify merge priority: event metadata overrides delivery_metadata source
		assert.Equal(t, "user-service", req.Header.Get("x-outpost-source"), "event metadata source should override delivery_metadata source")

		// Verify system metadata still present
		assert.Equal(t, "evt_123", req.Header.Get("x-outpost-event-id"))
		assert.Equal(t, "user.created", req.Header.Get("x-outpost-topic"))

		// Verify event metadata present
		assert.Equal(t, "usr_456", req.Header.Get("x-outpost-user-id"))

	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for webhook delivery")
	}
}

func TestWebhookPublisher_CustomHeaders(t *testing.T) {
	t.Parallel()

	t.Run("should include custom headers in request", func(t *testing.T) {
		t.Parallel()

		provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "http://example.com/webhook",
				"custom_headers": `{"x-api-key":"secret123","x-tenant-id":"tenant-abc"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		assert.Equal(t, "secret123", req.Header.Get("x-api-key"))
		assert.Equal(t, "tenant-abc", req.Header.Get("x-tenant-id"))
	})

	t.Run("should allow metadata to override custom headers", func(t *testing.T) {
		t.Parallel()

		provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "http://example.com/webhook",
				"custom_headers": `{"x-outpost-source":"custom-value"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
			testutil.DestinationFactory.WithDeliveryMetadata(map[string]string{
				"source": "delivery-metadata-value",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		// Metadata should override custom headers (metadata adds prefix x-outpost-)
		assert.Equal(t, "delivery-metadata-value", req.Header.Get("x-outpost-source"))
	})

	t.Run("should work with empty custom_headers", func(t *testing.T) {
		t.Parallel()

		provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "http://example.com/webhook",
				"custom_headers": `{}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		// Should still have standard headers
		assert.NotEmpty(t, req.Header.Get("x-outpost-event-id"))
		assert.NotEmpty(t, req.Header.Get("x-outpost-timestamp"))
	})

	t.Run("should work without custom_headers field", func(t *testing.T) {
		t.Parallel()

		provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "http://example.com/webhook",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		// Should still have standard headers
		assert.NotEmpty(t, req.Header.Get("x-outpost-event-id"))
		assert.NotEmpty(t, req.Header.Get("x-outpost-timestamp"))
	})

	t.Run("should work with disabled system headers", func(t *testing.T) {
		t.Parallel()

		provider, err := destwebhook.New(
			testutil.Registry.MetadataLoader(),
			nil,
			destwebhook.WithDisableDefaultTimestampHeader(true),
			destwebhook.WithDisableDefaultTopicHeader(true),
		)
		require.NoError(t, err)

		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "http://example.com/webhook",
				"custom_headers": `{"x-api-key":"secret123"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		publisher, err := provider.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
		)

		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		// Custom header should be present
		assert.Equal(t, "secret123", req.Header.Get("x-api-key"))
		// Disabled headers should be absent
		assert.Empty(t, req.Header.Get("x-outpost-timestamp"))
		assert.Empty(t, req.Header.Get("x-outpost-topic"))
		// Other system headers should still work
		assert.NotEmpty(t, req.Header.Get("x-outpost-event-id"))
	})
}

// TestWebhookPublisher_ConnectionErrors tests that connection errors (connection refused, DNS failures)
// return a Delivery object alongside the error, NOT nil.
//
// This is important because the messagehandler uses the presence of a Delivery object to distinguish
// between "pre-delivery errors" (system issues) and "delivery errors" (destination issues):
// - nil delivery + error → PreDeliveryError → nack → DLQ
// - delivery + error → DeliveryError → ack + retry
//
// Connection errors are destination-level failures and should trigger retries, not go to DLQ.
// See: https://github.com/hookdeck/outpost/issues/571
func TestWebhookPublisher_ConnectionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		url          string
		description  string
		expectedCode string
	}{
		{
			name:         "connection refused",
			url:          "http://127.0.0.1:1/webhook", // Port 1 is typically not listening
			description:  "simulates a server that is not running",
			expectedCode: "connection_refused",
		},
		{
			name:         "DNS failure",
			url:          "http://this-domain-does-not-exist-abc123xyz.invalid/webhook",
			description:  "simulates an invalid/non-existent domain",
			expectedCode: "dns_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("webhook"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"url": tt.url,
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"secret": "test-secret",
				}),
			)

			publisher, err := provider.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)
			defer publisher.Close()

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
			)

			// Attempt to publish to unreachable endpoint
			delivery, err := publisher.Publish(context.Background(), &event)

			// Should return an error
			require.Error(t, err, "should return error for %s", tt.description)

			// CRITICAL: Should return a Delivery object, NOT nil
			// This ensures the error is treated as a DeliveryError (retryable)
			// rather than a PreDeliveryError (goes to DLQ)
			require.NotNil(t, delivery, "delivery should NOT be nil for connection errors - "+
				"returning nil causes messagehandler to treat this as PreDeliveryError (nack → DLQ) "+
				"instead of DeliveryError (ack + retry)")

			// Verify the delivery has appropriate status and code
			assert.Equal(t, "failed", delivery.Status, "delivery status should be 'failed'")
			assert.Equal(t, tt.expectedCode, delivery.Code, "delivery code should indicate error type")
		})
	}
}

// TestWebhookPublisher_HTTPErrors tests that HTTP error responses (4xx, 5xx) return
// a Delivery object alongside the error. This is the current correct behavior that
// connection errors should also follow.
func TestWebhookPublisher_HTTPErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"400 Bad Request", 400},
		{"401 Unauthorized", 401},
		{"403 Forbidden", 403},
		{"404 Not Found", 404},
		{"429 Too Many Requests", 429},
		{"500 Internal Server Error", 500},
		{"502 Bad Gateway", 502},
		{"503 Service Unavailable", 503},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a server that returns the specified status code
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(`{"error": "test error"}`))
			}))
			defer server.Close()

			provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("webhook"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"url": server.URL + "/webhook",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"secret": "test-secret",
				}),
			)

			publisher, err := provider.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)
			defer publisher.Close()

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
			)

			delivery, err := publisher.Publish(context.Background(), &event)

			// Should return an error
			require.Error(t, err)

			// Should return a Delivery object (this already works correctly for HTTP errors)
			require.NotNil(t, delivery, "delivery should NOT be nil for HTTP errors")
			assert.Equal(t, "failed", delivery.Status)
			assert.Equal(t, fmt.Sprintf("%d", tt.statusCode), delivery.Code)
		})
	}
}

func TestWebhookPublisher_SignatureTemplates(t *testing.T) {
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "http://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "test-secret",
		}),
	)

	tests := []struct {
		name             string
		contentTemplate  string
		headerTemplate   string
		validateHeader   func(string) bool
		extractSignature func(string) (string, error)
	}{
		{
			name:            "default templates",
			contentTemplate: "",
			headerTemplate:  "",
			validateHeader: func(header string) bool {
				return strings.HasPrefix(header, "t=") && strings.Contains(header, ",v0=")
			},
			extractSignature: func(header string) (string, error) {
				parts := strings.Split(header, "v0=")
				if len(parts) != 2 {
					return "", fmt.Errorf("invalid signature header format")
				}
				return strings.Split(parts[1], ",")[0], nil
			},
		},
		{
			name:            "custom templates",
			contentTemplate: `ts={{.Timestamp.Unix}};data={{.Body}}`,
			headerTemplate:  `time={{.Timestamp.Unix}};sigs={{.Signatures | join ","}}`,
			validateHeader: func(header string) bool {
				return strings.HasPrefix(header, "time=") && strings.Contains(header, ";sigs=")
			},
			extractSignature: func(header string) (string, error) {
				parts := strings.Split(header, "sigs=")
				if len(parts) != 2 {
					return "", fmt.Errorf("invalid signature header format")
				}
				return strings.Split(parts[1], ",")[0], nil
			},
		},
		{
			name:            "custom templates with event data",
			contentTemplate: `ts={{.Timestamp.Unix}};id={{.EventID}};topic={{.Topic}};data={{.Body}}`,
			headerTemplate:  `time={{.Timestamp.Unix}};sigs={{.Signatures | join ","}}`,
			validateHeader: func(header string) bool {
				return strings.HasPrefix(header, "time=") && strings.Contains(header, ";sigs=")
			},
			extractSignature: func(header string) (string, error) {
				parts := strings.Split(header, "sigs=")
				if len(parts) != 2 {
					return "", fmt.Errorf("invalid signature header format")
				}
				return strings.Split(parts[1], ",")[0], nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := destwebhook.New(
				testutil.Registry.MetadataLoader(),
				nil,
				destwebhook.WithSignatureContentTemplate(tt.contentTemplate),
				destwebhook.WithSignatureHeaderTemplate(tt.headerTemplate),
			)
			require.NoError(t, err)

			publisher, err := provider.CreatePublisher(context.Background(), &dest)
			require.NoError(t, err)

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{"hello": "world"}),
			)

			req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
			require.NoError(t, err)

			// Verify header format
			signatureHeader := req.Header.Get("x-outpost-signature")
			assert.True(t, tt.validateHeader(signatureHeader), "header format should match expected pattern")

			// Extract signature using test case's extraction function
			signature, err := tt.extractSignature(signatureHeader)
			require.NoError(t, err)

			// Create a new signature manager to verify
			now := time.Now()
			secrets := []destwebhook.WebhookSecret{
				{
					Key:       "test-secret",
					CreatedAt: now,
				},
			}
			sm := destwebhook.NewSignatureManager(
				secrets,
				destwebhook.WithSignatureFormatter(destwebhook.NewSignatureFormatter(tt.contentTemplate)),
			)

			// Verify signature matches expected content
			assert.True(t, sm.VerifySignature(
				signature,
				"test-secret",
				destwebhook.SignaturePayload{
					Timestamp: now,
					Body:      `{"hello":"world"}`,
					EventID:   event.ID,
					Topic:     event.Topic,
				},
			), "signature should verify with expected content")
		})
	}
}
