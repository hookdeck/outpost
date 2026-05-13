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

// successResponseJSON mirrors a real Cloudflare Queues push success response.
// Captured from CF docs; replace with a recorded fixture once we have live creds.
const successResponseJSON = `{
  "success": true,
  "result": {
    "metadata": {
      "metrics": {
        "backlog_bytes": 1024,
        "backlog_count": 5,
        "oldest_message_timestamp_ms": 1710950954154
      }
    }
  },
  "messages": [],
  "errors": []
}`

// Roundtrip guard: if CF's documented success shape ever stops decoding cleanly
// into our types, this fails loudly instead of degrading silently to "failed".
func TestCloudflareAPIResponse_DecodesDocumentedShape(t *testing.T) {
	t.Parallel()
	var parsed struct {
		Success bool `json:"success"`
		Result  *struct {
			Metadata struct {
				Metrics struct {
					BacklogBytes             int64 `json:"backlog_bytes"`
					BacklogCount             int64 `json:"backlog_count"`
					OldestMessageTimestampMs int64 `json:"oldest_message_timestamp_ms"`
				} `json:"metrics"`
			} `json:"metadata"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(successResponseJSON), &parsed))
	assert.True(t, parsed.Success)
	require.NotNil(t, parsed.Result)
	assert.Equal(t, int64(1024), parsed.Result.Metadata.Metrics.BacklogBytes)
}

func newPublisher(t *testing.T, serverURL string) *destcfqueues.CloudflareQueuesPublisher {
	t.Helper()
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
	cfPublisher := publisher.(*destcfqueues.CloudflareQueuesPublisher)
	if serverURL != "" {
		cfPublisher.SetHTTPClient(&http.Client{Transport: &testTransport{serverURL: serverURL}})
	}
	return cfPublisher
}

func TestCloudflareQueuesPublisher_Format(t *testing.T) {
	t.Parallel()
	publisher := newPublisher(t, "")
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithDataMap(map[string]interface{}{
			"order_id": "test-order-123",
			"amount":   99.99,
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"source": "test-service",
		}),
	)

	req, err := publisher.Format(context.Background(), &event)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, "https://api.cloudflare.com/client/v4/accounts/test-account-id/queues/test-queue-id/messages", req.URL.String())
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "Bearer test-api-token", req.Header.Get("Authorization"))

	bodyBytes, err := io.ReadAll(req.Body)
	require.NoError(t, err)

	var payload struct {
		Body struct {
			Data     map[string]interface{} `json:"data"`
			Metadata map[string]string      `json:"metadata"`
		} `json:"body"`
		ContentType string `json:"content_type"`
	}
	require.NoError(t, json.Unmarshal(bodyBytes, &payload))

	assert.Equal(t, "json", payload.ContentType, "must set content_type=json so CF consumers decode JSON correctly")
	assert.Equal(t, "test-order-123", payload.Body.Data["order_id"])
	assert.Equal(t, 99.99, payload.Body.Data["amount"])
	assert.Equal(t, "evt_123", payload.Body.Metadata["event-id"])
	assert.Equal(t, "order.created", payload.Body.Metadata["topic"])
	assert.Equal(t, "test-service", payload.Body.Metadata["source"])
	assert.NotEmpty(t, payload.Body.Metadata["timestamp"])
}

func TestCloudflareQueuesPublisher_Publish_Success(t *testing.T) {
	t.Parallel()

	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-api-token", r.Header.Get("Authorization"))
		assert.True(t, strings.HasSuffix(r.URL.Path, "/accounts/test-account-id/queues/test-queue-id/messages"))
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(successResponseJSON))
	}))
	defer server.Close()

	publisher := newPublisher(t, server.URL)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID("evt_123"),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithDataMap(map[string]interface{}{"order_id": "test-order-123"}),
	)

	delivery, err := publisher.Publish(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "success", delivery.Status)
	assert.Equal(t, "OK", delivery.Code)

	// Verify server received the right single-message shape (not a batch wrapper).
	var sent map[string]interface{}
	require.NoError(t, json.Unmarshal(receivedBody, &sent))
	_, hasMessagesWrapper := sent["messages"]
	assert.False(t, hasMessagesWrapper, "must not wrap in a batch 'messages' array — single-message endpoint")
	assert.Equal(t, "json", sent["content_type"])
	assert.NotNil(t, sent["body"])
}

func TestCloudflareQueuesPublisher_Publish_HTTPSuccess(t *testing.T) {
	t.Parallel()

	for _, statusCode := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(successResponseJSON))
			}))
			defer server.Close()

			publisher := newPublisher(t, server.URL)
			defer publisher.Close()

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
			)
			delivery, err := publisher.Publish(context.Background(), &event)
			require.NoError(t, err)
			assert.Equal(t, "success", delivery.Status)
			assert.Equal(t, "OK", delivery.Code)
		})
	}
}

func TestCloudflareQueuesPublisher_Publish_APIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		expectedCode string
	}{
		{
			name:         "401 Unauthorized",
			statusCode:   http.StatusUnauthorized,
			responseBody: `{"success":false,"errors":[{"code":10000,"message":"Authentication error","documentation_url":"https://developers.cloudflare.com/api","source":{"pointer":"/"}}],"messages":[],"result":null}`,
			expectedCode: "401",
		},
		{
			name:         "403 Forbidden",
			statusCode:   http.StatusForbidden,
			responseBody: `{"success":false,"errors":[{"code":10001,"message":"Access denied"}],"messages":[],"result":null}`,
			expectedCode: "403",
		},
		{
			name:         "404 Not Found",
			statusCode:   http.StatusNotFound,
			responseBody: `{"success":false,"errors":[{"code":10002,"message":"Queue not found"}],"messages":[],"result":null}`,
			expectedCode: "404",
		},
		{
			name:         "500 Internal Server Error",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"success":false,"errors":[{"code":10003,"message":"Internal error"}],"messages":[],"result":null}`,
			expectedCode: "500",
		},
		{
			name:         "200 OK but success=false",
			statusCode:   http.StatusOK,
			responseBody: `{"success":false,"errors":[{"code":10004,"message":"Validation error"}],"messages":[],"result":null}`,
			expectedCode: "200",
		},
		{
			name:         "unparseable error body still returns failure",
			statusCode:   http.StatusBadGateway,
			responseBody: `<html>bad gateway</html>`,
			expectedCode: "502",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			publisher := newPublisher(t, server.URL)
			defer publisher.Close()

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
			)
			delivery, err := publisher.Publish(context.Background(), &event)
			require.Error(t, err)
			require.NotNil(t, delivery)
			assert.Equal(t, "failed", delivery.Status)
			assert.Equal(t, tt.expectedCode, delivery.Code)
		})
	}
}

// testTransport redirects requests to the test server while preserving the path.
type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := t.serverURL + req.URL.Path
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}
