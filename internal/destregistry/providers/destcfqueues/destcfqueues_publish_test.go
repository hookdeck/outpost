package destcfqueues_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destcfqueues"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloudflareAPIResponse mirrors the response structure from Cloudflare API
type cloudflareAPIResponse struct {
	Success  bool                     `json:"success"`
	Errors   []cloudflareAPIError     `json:"errors"`
	Messages []string                 `json:"messages"`
	Result   []map[string]interface{} `json:"result"`
}

type cloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// cloudflareMessagesRequest mirrors the request structure for Cloudflare Queues API
type cloudflareMessagesRequest struct {
	Messages []cloudflareMessage `json:"messages"`
}

type cloudflareMessage struct {
	Body messageBody `json:"body"`
}

type messageBody struct {
	Data     interface{}       `json:"data"`
	Metadata map[string]string `json:"metadata"`
}

func TestCloudflareQueuesPublisher_Format(t *testing.T) {
	t.Parallel()

	provider, err := destcfqueues.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("cloudflare_queues"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"account_id": "test-account-id",
			"queue_id":   "test-queue-id",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"api_token": "test-api-token",
		}),
	)

	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithData(map[string]interface{}{
			"order_id": "test-order-123",
			"amount":   99.99,
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"source": "test-service",
		}),
	)

	t.Run("should produce correct HTTP request structure", func(t *testing.T) {
		t.Parallel()
		req, err := publisher.(*destcfqueues.CloudflareQueuesPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		// Verify HTTP method
		assert.Equal(t, http.MethodPost, req.Method)

		// Verify URL structure
		assert.Equal(t, "https://api.cloudflare.com/client/v4/accounts/test-account-id/queues/test-queue-id/messages", req.URL.String())

		// Verify Content-Type header
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	})

	t.Run("should contain bearer token in Authorization header", func(t *testing.T) {
		t.Parallel()
		req, err := publisher.(*destcfqueues.CloudflareQueuesPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		authHeader := req.Header.Get("Authorization")
		assert.Equal(t, "Bearer test-api-token", authHeader)
	})

	t.Run("should contain event data and metadata in request body", func(t *testing.T) {
		t.Parallel()
		req, err := publisher.(*destcfqueues.CloudflareQueuesPublisher).Format(context.Background(), &event)
		require.NoError(t, err)

		// Read and parse the request body
		bodyBytes, err := io.ReadAll(req.Body)
		require.NoError(t, err)

		var reqPayload cloudflareMessagesRequest
		err = json.Unmarshal(bodyBytes, &reqPayload)
		require.NoError(t, err)

		// Verify the message structure
		require.Len(t, reqPayload.Messages, 1)

		// Verify event data is in the body
		dataMap, ok := reqPayload.Messages[0].Body.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "test-order-123", dataMap["order_id"])
		assert.Equal(t, 99.99, dataMap["amount"])

		// Verify metadata is present
		metadata := reqPayload.Messages[0].Body.Metadata
		assert.Equal(t, "evt_123", metadata["event-id"])
		assert.Equal(t, "order.created", metadata["topic"])
		assert.Equal(t, "test-service", metadata["source"])
		assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present in metadata")
	})
}

func TestCloudflareQueuesPublisher_Publish_Success(t *testing.T) {
	t.Parallel()

	// Create a mock server that simulates successful Cloudflare API response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-api-token", r.Header.Get("Authorization"))
		assert.True(t, strings.HasSuffix(r.URL.Path, "/accounts/test-account-id/queues/test-queue-id/messages"))

		// Return success response
		response := cloudflareAPIResponse{
			Success:  true,
			Errors:   []cloudflareAPIError{},
			Messages: []string{},
			Result: []map[string]interface{}{
				{"messageId": "msg-123"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with custom HTTP client that routes to test server
	provider, err := destcfqueues.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("cloudflare_queues"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"account_id": "test-account-id",
			"queue_id":   "test-queue-id",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"api_token": "test-api-token",
		}),
	)

	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	// Replace the HTTP client with one that routes to our test server
	cfPublisher := publisher.(*destcfqueues.CloudflareQueuesPublisher)
	cfPublisher.SetHTTPClient(&http.Client{
		Transport: &testTransport{serverURL: server.URL},
	})

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithData(map[string]interface{}{
			"order_id": "test-order-123",
		}),
	)

	delivery, err := publisher.Publish(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "success", delivery.Status)
	assert.Equal(t, "OK", delivery.Code)
}

func TestCloudflareQueuesPublisher_Publish_APIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		statusCode     int
		response       cloudflareAPIResponse
		expectedStatus string
		expectedCode   string
	}{
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			response: cloudflareAPIResponse{
				Success: false,
				Errors: []cloudflareAPIError{
					{Code: 10000, Message: "Authentication error"},
				},
			},
			expectedStatus: "failed",
			expectedCode:   "401",
		},
		{
			name:       "403 Forbidden",
			statusCode: http.StatusForbidden,
			response: cloudflareAPIResponse{
				Success: false,
				Errors: []cloudflareAPIError{
					{Code: 10001, Message: "Access denied"},
				},
			},
			expectedStatus: "failed",
			expectedCode:   "403",
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			response: cloudflareAPIResponse{
				Success: false,
				Errors: []cloudflareAPIError{
					{Code: 10002, Message: "Queue not found"},
				},
			},
			expectedStatus: "failed",
			expectedCode:   "404",
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			response: cloudflareAPIResponse{
				Success: false,
				Errors: []cloudflareAPIError{
					{Code: 10003, Message: "Internal error"},
				},
			},
			expectedStatus: "failed",
			expectedCode:   "500",
		},
		{
			name:       "API success false with errors",
			statusCode: http.StatusOK,
			response: cloudflareAPIResponse{
				Success: false,
				Errors: []cloudflareAPIError{
					{Code: 10004, Message: "Validation error"},
				},
			},
			expectedStatus: "failed",
			expectedCode:   "200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			provider, err := destcfqueues.New(testutil.Registry.MetadataLoader(), nil)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("cloudflare_queues"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"account_id": "test-account-id",
					"queue_id":   "test-queue-id",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"api_token": "test-api-token",
				}),
			)

			publisher, err := provider.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)
			defer publisher.Close()

			cfPublisher := publisher.(*destcfqueues.CloudflareQueuesPublisher)
			cfPublisher.SetHTTPClient(&http.Client{
				Transport: &testTransport{serverURL: server.URL},
			})

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
			)

			delivery, err := publisher.Publish(context.Background(), &event)
			require.Error(t, err)
			require.NotNil(t, delivery, "delivery should not be nil for API errors")
			assert.Equal(t, tt.expectedStatus, delivery.Status)
			assert.Equal(t, tt.expectedCode, delivery.Code)
		})
	}
}

func TestCloudflareQueuesPublisher_Publish_HTTPSuccess(t *testing.T) {
	t.Parallel()

	successCodes := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
	}

	for _, statusCode := range successCodes {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := cloudflareAPIResponse{
					Success:  true,
					Errors:   []cloudflareAPIError{},
					Messages: []string{},
					Result: []map[string]interface{}{
						{"messageId": "msg-123"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			provider, err := destcfqueues.New(testutil.Registry.MetadataLoader(), nil)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("cloudflare_queues"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"account_id": "test-account-id",
					"queue_id":   "test-queue-id",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"api_token": "test-api-token",
				}),
			)

			publisher, err := provider.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)
			defer publisher.Close()

			cfPublisher := publisher.(*destcfqueues.CloudflareQueuesPublisher)
			cfPublisher.SetHTTPClient(&http.Client{
				Transport: &testTransport{serverURL: server.URL},
			})

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
			)

			delivery, err := publisher.Publish(context.Background(), &event)
			require.NoError(t, err)
			assert.Equal(t, "success", delivery.Status)
			assert.Equal(t, "OK", delivery.Code)
		})
	}
}

// testTransport redirects requests to the test server
type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the URL with test server URL while keeping the path
	newURL := t.serverURL + req.URL.Path
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}
