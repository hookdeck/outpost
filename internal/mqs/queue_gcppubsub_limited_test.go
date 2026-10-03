package mqs_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeSubscriberServer records what a Pub/Sub client sends. Each stream gets
// the messages in send, then stays open until the client closes it.
type fakeSubscriberServer struct {
	pubsubpb.UnimplementedSubscriberServer
	send []*pubsubpb.ReceivedMessage

	mu     sync.Mutex
	first  []*pubsubpb.StreamingPullRequest // first request of each stream
	acked  []string
	nacked []string
}

func (f *fakeSubscriberServer) StreamingPull(stream pubsubpb.Subscriber_StreamingPullServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.first = append(f.first, req)
	f.mu.Unlock()
	if len(f.send) > 0 {
		if err := stream.Send(&pubsubpb.StreamingPullResponse{ReceivedMessages: f.send}); err != nil {
			return err
		}
	}
	for {
		if _, err := stream.Recv(); err != nil {
			return nil
		}
	}
}

func (f *fakeSubscriberServer) ModifyAckDeadline(_ context.Context, req *pubsubpb.ModifyAckDeadlineRequest) (*emptypb.Empty, error) {
	if req.AckDeadlineSeconds == 0 {
		f.mu.Lock()
		f.nacked = append(f.nacked, req.AckIds...)
		f.mu.Unlock()
	}
	return &emptypb.Empty{}, nil
}

func (f *fakeSubscriberServer) Acknowledge(_ context.Context, req *pubsubpb.AcknowledgeRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	f.acked = append(f.acked, req.AckIds...)
	f.mu.Unlock()
	return &emptypb.Empty{}, nil
}

func (f *fakeSubscriberServer) firstRequests() []*pubsubpb.StreamingPullRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*pubsubpb.StreamingPullRequest(nil), f.first...)
}

func (f *fakeSubscriberServer) settled() (acked, nacked []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acked...), append([]string(nil), f.nacked...)
}

// startFakeSubscriber serves f on a local port and returns client options
// that connect to it.
func startFakeSubscriber(t *testing.T, f *fakeSubscriberServer) []option.ClientOption {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	pubsubpb.RegisterSubscriberServer(srv, f)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	return []option.ClientOption{option.WithGRPCConn(conn)}
}

func newFakeGCPQueue() mqs.Queue {
	return mqs.NewQueue(&mqs.QueueConfig{
		GCPPubSub:         &mqs.GCPPubSubConfig{ProjectID: "test-project", SubscriptionID: "test-sub"},
		VisibilityTimeout: 30 * time.Second,
	})
}

// The limits reach Pub/Sub on the first StreamingPull request, which is
// the only one that may carry them. Without a byte limit the request is the
// same as before the option existed: the SDK default of 1e9 bytes.
func TestGCPPubSubQueue_StreamingPullRequestLimits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		opts      []mqs.SubscribeOption
		wantCount int64
		wantBytes int64
		limited   bool
	}{
		{name: "no limit", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5)}, wantCount: 5, wantBytes: 1e9},
		{name: "zero", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5), mqs.WithMaxBytes(0)}, wantCount: 5, wantBytes: 1e9},
		{name: "16 MiB", opts: []mqs.SubscribeOption{mqs.WithConcurrency(1100), mqs.WithMaxBytes(16 << 20)}, wantCount: 1100, wantBytes: 16 << 20, limited: true},
		{name: "over 2 GiB", opts: []mqs.SubscribeOption{mqs.WithConcurrency(1100), mqs.WithMaxBytes(5 << 30)}, wantCount: 1100, wantBytes: 5 << 30, limited: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeSubscriberServer{}
			clientOpts := startFakeSubscriber(t, f)
			ctx := context.Background()
			sub, err := mqs.GCPSubscribe(ctx, newFakeGCPQueue(), clientOpts, tc.opts...)
			require.NoError(t, err)
			defer sub.Shutdown(ctx)
			assert.Equal(t, tc.limited, mqs.IsGCPLimited(sub))

			require.Eventually(t, func() bool { return len(f.firstRequests()) > 0 }, 10*time.Second, 10*time.Millisecond)
			req := f.firstRequests()[0]
			assert.Equal(t, "projects/test-project/subscriptions/test-sub", req.Subscription)
			assert.Equal(t, tc.wantCount, req.MaxOutstandingMessages)
			assert.Equal(t, tc.wantBytes, req.MaxOutstandingBytes)
		})
	}
}

func fakeReceivedMessages(n int) []*pubsubpb.ReceivedMessage {
	msgs := make([]*pubsubpb.ReceivedMessage, n)
	for i := range msgs {
		msgs[i] = &pubsubpb.ReceivedMessage{
			AckId:   fmt.Sprintf("ack-%d", i),
			Message: &pubsubpb.PubsubMessage{MessageId: fmt.Sprintf("m-%d", i), Data: []byte("x")},
		}
	}
	return msgs
}

// Messages received from the stream and not handed to Receive go back to
// Pub/Sub with a nack when the subscription shuts down, also after its
// context has ended. After 6s the client no longer tracks them (lease
// extension is off) and drops a nack that comes after the stream closes.
func TestGCPPubSubQueue_LimitedStopNacksUnreceived(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		stopBy string
		wait   time.Duration
	}{
		{"shutdown", 200 * time.Millisecond},
		{"context", 200 * time.Millisecond},
		{"shutdown", 6 * time.Second},
		{"context", 6 * time.Second},
	} {
		stopBy := tc.stopBy
		t.Run(fmt.Sprintf("%s after %s", stopBy, tc.wait), func(t *testing.T) {
			t.Parallel()
			f := &fakeSubscriberServer{send: fakeReceivedMessages(3)}
			clientOpts := startFakeSubscriber(t, f)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sub, err := mqs.GCPSubscribe(ctx, newFakeGCPQueue(), clientOpts, mqs.WithConcurrency(10), mqs.WithMaxBytes(1<<20))
			require.NoError(t, err)

			msg, err := sub.Receive(ctx)
			require.NoError(t, err)
			msg.Ack()
			// The other two wait to be handed over.
			time.Sleep(tc.wait)

			if stopBy == "context" {
				// Handlers still run after their context ends: the stream stays
				// open, and a nack now would bring the message straight back.
				cancel()
				time.Sleep(300 * time.Millisecond)
				_, nacked := f.settled()
				require.Empty(t, nacked)
			}
			stopping := time.Now()
			require.NoError(t, sub.Shutdown(context.Background()))
			assert.Less(t, time.Since(stopping), 2*time.Second)

			acked, nacked := f.settled()
			assert.Len(t, acked, 1)
			assert.Len(t, nacked, 2)
			assert.ElementsMatch(t, []string{"ack-0", "ack-1", "ack-2"}, append(acked, nacked...))
		})
	}
}
