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
	assert.NotEmpty(t, req.Header.Get(a.headerPrefix+"timestamp"), "timestamp header should be present")
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

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(s.T(), err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix: "x-outpost-",
		},
	})

	s.consumer = consumer
}

// Single secret test configuration
func (s *WebhookPublishSuite) setupSingleSecretSuite() {
	consumer := NewWebhookConsumer("x-outpost-")

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(s.T(), err)

	now := time.Now()
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secrets": fmt.Sprintf(`[{"key":"secret1","created_at":"%s"}]`,
				now.Format(time.RFC3339)),
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

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(s.T(), err)

	now := time.Now()
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secrets": fmt.Sprintf(`[
				{"key":"secret1","created_at":"%s"},
				{"key":"secret2","created_at":"%s"}
			]`,
				now.Add(-12*time.Hour).Format(time.RFC3339),
				now.Add(-6*time.Hour).Format(time.RFC3339),
			),
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix:       "x-outpost-",
			expectedSignatures: 2,
			secrets:            []string{"secret1", "secret2"},
		},
	})

	s.consumer = consumer
}

// Expired secrets test configuration
func (s *WebhookPublishSuite) setupExpiredSecretsSuite() {
	consumer := NewWebhookConsumer("x-outpost-")

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader())
	require.NoError(s.T(), err)

	now := time.Now()
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
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

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix:       "x-outpost-",
			expectedSignatures: 2, // Only expect signatures from active secrets
			secrets:            []string{"active_secret1", "active_secret2"},
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
		destwebhook.WithHeaderPrefix(customPrefix),
	)
	require.NoError(s.T(), err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": consumer.server.URL + "/webhook",
		}),
	)

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &WebhookAsserter{
			headerPrefix: customPrefix,
		},
	})

	s.consumer = consumer
}

func TestWebhookTimeout(t *testing.T) {
	t.Parallel()

	// Setup test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Delay longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create webhook destination with short timeout
	provider, err := destwebhook.New(
		testutil.Registry.MetadataLoader(),
		destwebhook.WithTimeout(1), // 1 second timeout
	)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": server.URL + "/webhook",
		}),
	)

	publisher, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err)
	defer publisher.Close()

	// Attempt publish which should timeout
	event := testutil.EventFactory.Any()
	err = publisher.Publish(context.Background(), &event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
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
			dest, err := destwebhook.New(testutil.Registry.MetadataLoader(), tt.options...)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("webhook"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"url": "http://example.com",
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

func TestWebhookPublisher_SignatureTemplates(t *testing.T) {
	now := time.Now()
	secret := destwebhook.WebhookSecret{
		Key:       "test-secret",
		CreatedAt: now,
	}

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
				destwebhook.WithSignatureContentTemplate(tt.contentTemplate),
				destwebhook.WithSignatureHeaderTemplate(tt.headerTemplate),
			)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("webhook"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"url": "http://example.com",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"secrets": fmt.Sprintf(`[{"key":"%s","created_at":"%s"}]`,
						secret.Key,
						secret.CreatedAt.Format(time.RFC3339)),
				}),
			)

			publisher, err := provider.CreatePublisher(context.Background(), &destination)
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
			sm := destwebhook.NewSignatureManager(
				[]destwebhook.WebhookSecret{secret},
				destwebhook.WithSignatureFormatter(destwebhook.NewSignatureFormatter(tt.contentTemplate)),
			)

			// Verify signature matches expected content
			assert.True(t, sm.VerifySignature(
				signature,
				secret.Key,
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
