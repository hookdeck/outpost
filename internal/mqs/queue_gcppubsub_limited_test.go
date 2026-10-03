package mqs_test

import (
	"context"
	"fmt"
	"io"
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
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeSubscriberServer records what a Pub/Sub client sends. Each stream gets
// the messages in send, then stays open until the client closes it.
type fakeSubscriberServer struct {
	pubsubpb.UnimplementedSubscriberServer
	send []*pubsubpb.ReceivedMessage

	mu         sync.Mutex
	first      []*pubsubpb.StreamingPullRequest // first request of each stream
	open       []context.Context                // context of each stream
	acked      []string
	nacked     []string
	nackedOpen []string // nacked while a stream was open
}

func (f *fakeSubscriberServer) streamOpen() bool {
	for _, ctx := range f.open {
		if ctx.Err() == nil {
			return true
		}
	}
	return false
}

func (f *fakeSubscriberServer) StreamingPull(stream pubsubpb.Subscriber_StreamingPullServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.first = append(f.first, req)
	f.open = append(f.open, stream.Context())
	f.mu.Unlock()
	if len(f.send) > 0 {
		if err := stream.Send(&pubsubpb.StreamingPullResponse{ReceivedMessages: f.send}); err != nil {
			return err
		}
	}

	// A half-close does not end the stream: it stays open until the client
	// cancels it.
	for {
		if _, err := stream.Recv(); err != nil {
			<-stream.Context().Done()
			return nil
		}
	}
}

func (f *fakeSubscriberServer) ModifyAckDeadline(_ context.Context, req *pubsubpb.ModifyAckDeadlineRequest) (*emptypb.Empty, error) {
	if req.AckDeadlineSeconds == 0 {
		f.mu.Lock()
		f.nacked = append(f.nacked, req.AckIds...)
		if f.streamOpen() {
			f.nackedOpen = append(f.nackedOpen, req.AckIds...)
		}
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

func (f *fakeSubscriberServer) settled() (acked, nacked, nackedOpen []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acked...), append([]string(nil), f.nacked...), append([]string(nil), f.nackedOpen...)
}

// startFakeSubscriber serves f on a local port and returns client options
// that connect to it.
func startFakeSubscriber(t *testing.T, f *fakeSubscriberServer, dialOpts ...grpc.DialOption) []option.ClientOption {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	pubsubpb.RegisterSubscriberServer(srv, f)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	opts := []option.ClientOption{
		option.WithEndpoint(lis.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		option.WithTelemetryDisabled(),
	}
	for _, o := range dialOpts {
		opts = append(opts, option.WithGRPCDialOption(o))
	}
	return opts
}

// slowStreamClose ends each stream for the client as soon as the client
// cancels it, and on the wire 300ms later. Settling messages before the
// stream has ended on the wire then shows up as a nack while the stream is
// still open on the server.
func slowStreamClose() grpc.DialOption {
	return grpc.WithChainStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		inner, cancel := context.WithCancel(context.WithoutCancel(ctx))
		go func() {
			<-ctx.Done()
			time.Sleep(300 * time.Millisecond)
			cancel()
		}()
		cs, err := streamer(inner, desc, cc, method, opts...)
		if err != nil || method != "/google.pubsub.v1.Subscriber/StreamingPull" {
			return cs, err
		}
		s := &earlyEndStream{ClientStream: cs, ctx: ctx, recv: make(chan *pubsubpb.StreamingPullResponse)}
		go s.pump()
		return s, nil
	})
}

type earlyEndStream struct {
	grpc.ClientStream
	ctx  context.Context
	recv chan *pubsubpb.StreamingPullResponse
}

func (s *earlyEndStream) pump() {
	defer close(s.recv)
	for {
		res := &pubsubpb.StreamingPullResponse{}
		if err := s.ClientStream.RecvMsg(res); err != nil {
			return
		}
		select {
		case s.recv <- res:
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *earlyEndStream) RecvMsg(m any) error {
	select {
	case res, ok := <-s.recv:
		if !ok {
			return io.EOF
		}
		proto.Merge(m.(*pubsubpb.StreamingPullResponse), res)
		return nil
	case <-s.ctx.Done():
		return status.FromContextError(s.ctx.Err()).Err()
	}
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
// context has ended and after the client stopped tracking them (6s, lease
// extension off). The nacks reach Pub/Sub only after the stream has closed:
// on an open stream, Pub/Sub could send the message straight back to it.
func TestGCPPubSubQueue_LimitedStopNacksUnreceived(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		stopBy    string
		wait      time.Duration
		slowClose bool
	}{
		{"shutdown", 200 * time.Millisecond, false},
		{"context", 200 * time.Millisecond, false},
		{"shutdown", 6 * time.Second, false},
		{"context", 6 * time.Second, false},
		{"shutdown", 200 * time.Millisecond, true},
		{"context", 200 * time.Millisecond, true},
	} {
		stopBy := tc.stopBy
		t.Run(fmt.Sprintf("%s after %s slow close %v", stopBy, tc.wait, tc.slowClose), func(t *testing.T) {
			t.Parallel()
			f := &fakeSubscriberServer{send: fakeReceivedMessages(3)}
			var dialOpts []grpc.DialOption
			if tc.slowClose {
				dialOpts = append(dialOpts, slowStreamClose())
			}
			clientOpts := startFakeSubscriber(t, f, dialOpts...)
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
				_, nacked, _ := f.settled()
				require.Empty(t, nacked)
			}
			stopping := time.Now()
			require.NoError(t, sub.Shutdown(context.Background()))
			assert.Less(t, time.Since(stopping), 2*time.Second)

			acked, nacked, nackedOpen := f.settled()
			assert.Len(t, acked, 1)
			assert.Len(t, nacked, 2)
			assert.Empty(t, nackedOpen)
			assert.ElementsMatch(t, []string{"ack-0", "ack-1", "ack-2"}, append(acked, nacked...))
		})
	}
}

// The SDK drops the nack of a message it reads from the stream after its
// last ack/nack flush, so the message would come back only at the end of its
// ack deadline. Shutdown nacks every message received and not acked or
// nacked, after the stream has ended.
func TestGCPPubSubQueue_LimitedShutdownNacksUnsettled(t *testing.T) {
	t.Parallel()
	f := &fakeSubscriberServer{send: fakeReceivedMessages(1)}
	clientOpts := startFakeSubscriber(t, f, slowStreamClose())
	ctx := context.Background()
	sub, err := mqs.GCPSubscribe(ctx, newFakeGCPQueue(), clientOpts, mqs.WithConcurrency(10), mqs.WithMaxBytes(1<<20))
	require.NoError(t, err)

	msg, err := sub.Receive(ctx)
	require.NoError(t, err)
	msg.Ack()
	require.Eventually(t, func() bool { acked, _, _ := f.settled(); return len(acked) == 1 }, 5*time.Second, 10*time.Millisecond)
	// Read from the stream, never seen by the SDK's ack/nack sender.
	mqs.GCPReceived(sub, "ack-dropped", "m-dropped")
	require.NoError(t, sub.Shutdown(ctx))

	acked, nacked, nackedOpen := f.settled()
	assert.Equal(t, []string{"ack-0"}, acked)
	assert.Equal(t, []string{"ack-dropped"}, nacked)
	assert.Empty(t, nackedOpen)
}
