package destgcppubsub

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type cloudTestPublisher struct {
	pubsubpb.UnimplementedPublisherServer
	hits atomic.Int32
}

func (s *cloudTestPublisher) Publish(_ context.Context, _ *pubsubpb.PublishRequest) (*pubsubpb.PublishResponse, error) {
	s.hits.Add(1)
	return &pubsubpb.PublishResponse{MessageIds: []string{"test-id"}}, nil
}

func TestPubSubEndpointConnectionPolicy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	receiver := &cloudTestPublisher{}
	pubsubpb.RegisterPublisherServer(server, receiver)
	go server.Serve(listener)
	defer server.Stop()
	d := &models.Destination{
		Type:        "gcp_pubsub",
		Config:      models.Config{"project_id": "test-project", "topic": "test-topic", "endpoint": strings.Replace(listener.Addr().String(), "127.0.0.1", "localhost", 1)},
		Credentials: models.Credentials{"service_account_json": "{}"},
	}
	for _, cidrs := range []string{"", "127.0.0.1/32,::1/128"} {
		policy, err := destregistry.NewCloudEndpointPolicy(cidrs)
		require.NoError(t, err)
		provider, err := New(metadata.NewMetadataLoader(""), nil, WithEndpointPolicy(policy))
		require.NoError(t, err)
		pub, err := provider.CreatePublisher(context.Background(), d)
		require.NoError(t, err)
		// Pub/Sub batches retry independently of the Publish caller's context.
		// Keep the SDK's background retry deadline bounded in this local test.
		pub.(*GCPPubSubPublisher).topic.PublishSettings.Timeout = time.Second
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = pub.Publish(ctx, &models.Event{ID: "test", Data: models.Data(`{"test":true}`)})
		cancel()
		require.NoError(t, pub.Close())
		if cidrs == "" {
			require.Error(t, err)
			require.Zero(t, receiver.hits.Load())
		} else {
			require.NoError(t, err)
			require.EqualValues(t, 1, receiver.hits.Load())
		}
	}
}
