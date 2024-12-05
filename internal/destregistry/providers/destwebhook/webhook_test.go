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
