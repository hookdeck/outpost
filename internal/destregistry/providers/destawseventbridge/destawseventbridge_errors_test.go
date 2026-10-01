package destawseventbridge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawseventbridge"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publishTo publishes one event to an EventBridge endpoint at the given URL.
func publishTo(t *testing.T, ctx context.Context, endpoint string) (*destregistry.Delivery, error) {
	t.Helper()
	provider, err := destawseventbridge.New(testutil.Registry.MetadataLoader(), nil, destawseventbridge.WithSource("outpost"))
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_eventbridge"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"endpoint":       endpoint,
			"event_bus_name": "my-bus",
			"region":         "us-east-1",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"key": "test", "secret": "test"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(testutil.EventFactory.WithTopic("user.created"))
	return publisher.Publish(ctx, &event)
}

func TestAWSEventBridgePublisher_Errors(t *testing.T) {
	t.Parallel()

	respondWith := func(t *testing.T, status int, body string) string {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-amz-json-1.1")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		return server.URL
	}

	assertFailed := func(t *testing.T, delivery *destregistry.Delivery, err error) {
		t.Helper()
		var publishErr *destregistry.ErrDestinationPublishAttempt
		require.ErrorAs(t, err, &publishErr)
		assert.Equal(t, "aws_eventbridge", publishErr.Provider)
		require.NotNil(t, delivery)
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, "ERR", delivery.Code)
	}

	// AWS spells the message key of an error body "message" or "Message"
	// depending on the error.
	for _, messageKey := range []string{"message", "Message"} {
		t.Run("API error carries the AWS code and "+messageKey, func(t *testing.T) {
			t.Parallel()
			message := "User: arn:aws:iam::123456789012:user/outpost is not authorized to perform: events:PutEvents on resource: arn:aws:events:us-east-1:123456789012:event-bus/my-bus"
			endpoint := respondWith(t, http.StatusBadRequest, `{"__type":"AccessDeniedException","`+messageKey+`":"`+message+`"}`)

			delivery, err := publishTo(t, context.Background(), endpoint)

			assertFailed(t, delivery, err)
			assert.Equal(t, "AccessDeniedException", delivery.Response["error_code"])
			assert.Contains(t, delivery.Response["error"], message)
		})
	}

	t.Run("rejected entry carries the entry code and message", func(t *testing.T) {
		t.Parallel()
		endpoint := respondWith(t, http.StatusOK, `{"FailedEntryCount":1,"Entries":[{"ErrorCode":"MalformedDetail","ErrorMessage":"Detail is malformed."}]}`)

		delivery, err := publishTo(t, context.Background(), endpoint)

		assertFailed(t, delivery, err)
		assert.Equal(t, "MalformedDetail", delivery.Response["error_code"])
		assert.Contains(t, delivery.Response["error"], "Detail is malformed.")
	})

	t.Run("rejected entry without a message carries the entry code", func(t *testing.T) {
		t.Parallel()
		endpoint := respondWith(t, http.StatusOK, `{"FailedEntryCount":1,"Entries":[{"ErrorCode":"InternalFailure"}]}`)

		delivery, err := publishTo(t, context.Background(), endpoint)

		assertFailed(t, delivery, err)
		assert.Equal(t, "InternalFailure", delivery.Response["error_code"])
		assert.Equal(t, "eventbridge: entry rejected: InternalFailure", delivery.Response["error"])
	})

	t.Run("failed entry count without entries is a failure", func(t *testing.T) {
		t.Parallel()
		endpoint := respondWith(t, http.StatusOK, `{"FailedEntryCount":1,"Entries":[]}`)

		delivery, err := publishTo(t, context.Background(), endpoint)

		assertFailed(t, delivery, err)
		assert.NotContains(t, delivery.Response, "error_code")
		assert.Contains(t, delivery.Response["error"], "entry rejected")
	})

	t.Run("unreachable endpoint reports the connection error", func(t *testing.T) {
		t.Parallel()
		delivery, err := publishTo(t, context.Background(), "http://127.0.0.1:1")

		assertFailed(t, delivery, err)
		assert.NotContains(t, delivery.Response, "error_code")
		assert.Contains(t, delivery.Response["error"], "connection refused")
	})

	t.Run("timeout reports the deadline", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-release
		}))
		t.Cleanup(server.Close)
		t.Cleanup(func() { close(release) })

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		delivery, err := publishTo(t, ctx, server.URL)

		assertFailed(t, delivery, err)
		assert.NotContains(t, delivery.Response, "error_code")
		assert.Contains(t, delivery.Response["error"], "context deadline exceeded")
	})
}
