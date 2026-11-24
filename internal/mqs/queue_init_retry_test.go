package mqs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSQueue_SubscribeRetriesFailedInit(t *testing.T) {
	t.Parallel()

	var getQueueURLCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") != "AmazonSQS.GetQueueUrl" {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		if getQueueURLCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"__type":  "com.amazonaws.sqs#QueueDoesNotExist",
				"message": "The specified queue does not exist.",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"QueueUrl": "http://sqs.local/000000000000/publish"})
	}))
	t.Cleanup(srv.Close)

	q := mqs.NewAWSQueue(&mqs.AWSSQSConfig{
		Endpoint:                  srv.URL,
		Region:                    "us-east-1",
		ServiceAccountCredentials: "test:test:",
		Topic:                     "publish",
	})

	_, err := q.Subscribe(t.Context())
	require.Error(t, err)

	sub, err := q.Subscribe(t.Context())
	require.NoError(t, err)
	require.NotNil(t, sub)
	assert.Equal(t, "http://sqs.local/000000000000/publish", mqs.AWSQueueURL(q))

	_, err = q.Subscribe(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(2), getQueueURLCalls.Load(), "successful init should be cached")
}

func TestAzureServiceBusQueue_SubscribeRetriesFailedInit(t *testing.T) {
	t.Parallel()

	config := &mqs.AzureServiceBusConfig{Topic: "publish", Subscription: "outpost"}
	q := mqs.NewAzureServiceBusQueue(config)

	_, err := q.Subscribe(t.Context())
	require.Error(t, err)

	config.ConnectionString = "Endpoint=sb://localhost/;SharedAccessKeyName=test;SharedAccessKey=test"

	require.NotPanics(t, func() {
		sub, err := q.Subscribe(t.Context())
		require.NoError(t, err)
		require.NotNil(t, sub)
	})
}

func TestGCPPubSubQueue_FailedInitReleasesConnections(t *testing.T) {
	t.Parallel()

	config := &mqs.GCPPubSubConfig{
		ProjectID:                 "test-project",
		ServiceAccountCredentials: `{"type":"service_account","client_email":"test@test-project.iam.gserviceaccount.com","private_key":"test","token_uri":"http://127.0.0.1:1/token"}`,
	}
	q := mqs.NewGCPPubSubQueue(config, 0)

	// An empty topic ID fails after the connection and client are opened.
	for range 2 {
		_, err := q.Init(t.Context())
		require.Error(t, err)
		assert.Zero(t, mqs.GCPPubSubCleanupFns(q))
	}

	config.TopicID = "publish"
	cleanup, err := q.Init(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, mqs.GCPPubSubCleanupFns(q))
	cleanup()
}
