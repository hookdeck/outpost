package destawskinesis_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSKinesisPublisher_ResponseWithoutShardID(t *testing.T) {
	t.Parallel()

	// A Kinesis-compatible endpoint can answer 200 without ShardId or
	// SequenceNumber.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	provider, err := destawskinesis.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_kinesis"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"stream_name": "my-stream",
			"region":      "us-east-1",
			"endpoint":    server.URL,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"key": "test", "secret": "test"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(testutil.EventFactory.WithTopic("user.created"))
	delivery, err := publisher.Publish(context.Background(), &event)

	require.NoError(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "success", delivery.Status)
	assert.Equal(t, "", delivery.Response["shard_id"])
	assert.Equal(t, "", delivery.Response["sequence_number"])
}
