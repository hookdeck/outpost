package desthookdeck_test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/desthookdeck"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// HookdeckConsumer implements testsuite.MessageConsumer
type HookdeckConsumer struct {
	server   *httptest.Server
	messages chan testsuite.Message
	wg       sync.WaitGroup
}

func NewHookdeckConsumer() *HookdeckConsumer {
	consumer := &HookdeckConsumer{
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

		// Convert headers to metadata
		metadata := make(map[string]string)
		for k, v := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-outpost-") {
				metadata[strings.TrimPrefix(strings.ToLower(k), "x-outpost-")] = v[0]
			}
		}

		consumer.messages <- testsuite.Message{
			Data:     body,
			Metadata: metadata,
			Raw:      r, // Store the raw request for signature verification
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))

	return consumer
}

func (c *HookdeckConsumer) Consume() <-chan testsuite.Message {
	return c.messages
}

func (c *HookdeckConsumer) Close() error {
	c.wg.Wait()
	c.server.Close()
	close(c.messages)
	return nil
}

// HookdeckAsserter implements testsuite.MessageAsserter
type HookdeckAsserter struct {
	signingKey string
}

func (a *HookdeckAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	req := msg.Raw.(*http.Request)

	// Verify basic HTTP properties
	assert.Equal(t, "POST", req.Method)

	// Check the original URL from the header to ensure it would have gone to Hookdeck
	originalURL := req.Header.Get("X-Original-URL")
	assert.NotEmpty(t, originalURL, "Original URL header should be present")
	assert.True(t, strings.HasPrefix(originalURL, "https://hkdk.events/"),
		"Original URL should point to Hookdeck")
	assert.True(t, strings.Contains(originalURL, "123"),
		"Original URL should contain the token ID")

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	// Verify standard metadata headers
	assert.Equal(t, event.ID, req.Header.Get("X-Outpost-Event-ID"), "event-id header should match")
	assert.Equal(t, event.Topic, req.Header.Get("X-Outpost-Topic"), "topic header should match")
	assert.NotEmpty(t, req.Header.Get("X-Outpost-Timestamp"), "timestamp header should be present")

	// Verify custom metadata headers
	for k, v := range event.Metadata {
		assert.Equal(t, v, msg.Metadata[k], "metadata key %s should match expected value", k)
	}

	// Create a new request to verify the signature since the body may have been consumed
	// and restore the body using the message data
	verifyReq, _ := http.NewRequest("POST", req.URL.String(), strings.NewReader(string(msg.Data)))
	verifyReq.Header = req.Header

	// Use the existing verifySignature function
	// We need to convert the testsuite.TestingT to a testing.T for the helper function
	tester := t.(*testing.T)
	body, isValid := verifySignature(tester, verifyReq, a.signingKey)
	assert.True(t, isValid, "signature should be a valid HMAC SHA256 signature")

	// Verify request body matches event data
	var bodyData map[string]interface{}
	err := json.Unmarshal(body, &bodyData)
	require.NoError(t, err)

	// Compare body data with event data
	eventDataJSON, err := json.Marshal(event.Data)
	require.NoError(t, err)
	var eventData map[string]interface{}
	err = json.Unmarshal(eventDataJSON, &eventData)
	require.NoError(t, err)

	for k, v := range eventData {
		assert.Equal(t, v, bodyData[k], "body data should match event data for key %s", k)
	}
}

// CustomTransport is used to redirect requests to our test server
type CustomTransport struct {
	testServerURL string
}

func (t *CustomTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Save the original URL for debugging
	originalURL := req.URL.String()

	// Create a new URL pointing to our test server
	// Append the original host as a path segment to maintain uniqueness
	// and for debugging purposes
	newPath := "/" + strings.Replace(req.URL.Host, ".", "_", -1)
	if req.URL.Path != "/" {
		newPath += req.URL.Path
	}

	// Parse the test server URL and update it with our custom path
	newURL, err := url.Parse(t.testServerURL + newPath)
	if err != nil {
		return nil, err
	}

	// Update the request to point to our test server
	req.URL = newURL
	req.Host = newURL.Host

	// Add the original URL as a header for debugging/verification
	req.Header.Set("X-Original-URL", originalURL)

	// Create a direct connection to our test server
	client := &http.Client{
		Transport: &http.Transport{
			// Disable HTTP/2
			TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
		},
	}

	// Make the request directly to our test server
	return client.Transport.RoundTrip(req)
}

// HookdeckPublishSuite is the test suite for Hookdeck publisher
type HookdeckPublishSuite struct {
	testsuite.PublisherSuite
	consumer    *HookdeckConsumer
	tokenString string
}

func createCustomClient(serverURL string) *http.Client {
	return &http.Client{
		Transport: &CustomTransport{
			testServerURL: serverURL,
		},
	}
}

func (s *HookdeckPublishSuite) SetupSuite() {
	consumer := NewHookdeckConsumer()

	// Create a test token
	tokenData := "123:test-source:signing-key"
	s.tokenString = base64.StdEncoding.EncodeToString([]byte(tokenData))

	// Create a custom client that routes requests to our test server
	customClient := createCustomClient(consumer.server.URL)

	// Create provider with the custom HTTP client
	provider, err := desthookdeck.New(
		testutil.Registry.MetadataLoader(),
		desthookdeck.WithHTTPClient(customClient),
	)
	require.NoError(s.T(), err)

	// Create destination with the token
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("hookdeck"),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"token": s.tokenString,
		}),
	)

	// Create custom asserter to verify Hookdeck-specific behaviors
	asserter := &HookdeckAsserter{
		signingKey: s.tokenString,
	}

	// Initialize the suite using the provider with the custom client
	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: asserter,
	})

	s.consumer = consumer
}

func (s *HookdeckPublishSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
}

func TestHookdeckPublish(t *testing.T) {
	suite.Run(t, new(HookdeckPublishSuite))
}

func TestHookdeckProvider_WithClientOption(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	// Create a custom client
	customClient := createCustomClient(server.URL)

	// Create a token
	tokenData := "123:test-source:signing-key"
	tokenString := base64.StdEncoding.EncodeToString([]byte(tokenData))

	// Create a provider with the custom client
	provider, err := desthookdeck.New(
		testutil.Registry.MetadataLoader(),
		desthookdeck.WithHTTPClient(customClient),
	)
	require.NoError(t, err)

	// Create a destination
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("hookdeck"),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"token": tokenString,
		}),
	)

	// Create a publisher through the provider
	publisher, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err)
	defer publisher.Close()

	// Create an event
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("event123"),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithData(map[string]interface{}{
			"order_id": "test123",
		}),
	)

	// Publish the event - should go to our test server via the custom client
	delivery, err := publisher.Publish(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "success", delivery.Status)
	assert.Equal(t, "200", delivery.Code)
}

func TestHookdeckPublisher_DirectClientOption(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"custom_client_ok"}`))
	}))
	defer server.Close()

	// Create a custom client
	customClient := createCustomClient(server.URL)

	// Create a token
	tokenData := "123:test-source:signing-key"
	tokenString := base64.StdEncoding.EncodeToString([]byte(tokenData))

	// Create publisher with the custom client
	publisher, err := desthookdeck.NewPublisher(
		tokenString,
		desthookdeck.PublisherWithClient(customClient),
	)
	require.NoError(t, err)
	defer publisher.Close()

	// Create an event
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("event123"),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithData(map[string]interface{}{
			"order_id": "test123",
		}),
	)

	// Publish the event - should go to our test server via the custom client
	delivery, err := publisher.Publish(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "success", delivery.Status)
	assert.Equal(t, "200", delivery.Code)
}

func TestHookdeckPublisher_TokenInvalidation(t *testing.T) {
	// Test with invalid token
	_, err := desthookdeck.NewPublisher("invalid-token")
	assert.Error(t, err, "Creating publisher with invalid token should fail")

	// Test with valid token
	validToken := base64.StdEncoding.EncodeToString([]byte("123:test-source:signing-key"))
	publisher, err := desthookdeck.NewPublisher(validToken)
	assert.NoError(t, err, "Creating publisher with valid token should succeed")
	assert.NotNil(t, publisher, "Publisher should not be nil")

	// Clean up
	publisher.Close()
}
