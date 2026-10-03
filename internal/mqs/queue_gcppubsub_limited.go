package mqs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	nativepubsub "cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const gcpShutdownNackTimeout = 10 * time.Second

type gcpHeldNackKey struct{}

// gcpLimitedSubscription is the StreamingPull subscription used with a byte
// limit. Pub/Sub holds the stream at MaxOutstandingMessages and
// MaxOutstandingBytes, so the backlog waits in the subscription.
//
// Messages not handed to Receive by Shutdown are nacked after the stream has
// closed.
type gcpLimitedSubscription struct {
	msgChan  chan *Message
	release  chan struct{} // closed by Shutdown: nack instead of handing over
	handoff  sync.RWMutex  // held for reading by each hand-off in progress
	stopOnce sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
	client   *nativepubsub.Client
	recvErr  error // set before done is closed

	mu         sync.Mutex
	nacks      []*pubsubpb.ModifyAckDeadlineRequest // held until the stream has closed
	nackMD     metadata.MD
	stream     *grpc.ClientConn // connection of the latest stream
	streamDone chan struct{}    // closed once that stream has ended
}

var _ Subscription = &gcpLimitedSubscription{}
var _ ConcurrentSubscription = &gcpLimitedSubscription{}

func newGCPLimitedSubscription(ctx context.Context, projectID, subscriptionID string, settings nativepubsub.ReceiveSettings, clientOpts []option.ClientOption) (*gcpLimitedSubscription, error) {
	s := &gcpLimitedSubscription{
		msgChan: make(chan *Message),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	clientOpts = append(clientOpts,
		option.WithGRPCDialOption(grpc.WithChainUnaryInterceptor(s.holdNacks)),
		option.WithGRPCDialOption(grpc.WithChainStreamInterceptor(s.trackStream)),
	)
	client, err := nativepubsub.NewClient(ctx, projectID, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create pubsub client: %w", err)
	}
	s.client = client
	sub := client.Subscription(subscriptionID)
	sub.ReceiveSettings = settings

	// The stream outlives ctx: messages received while handlers finish are
	// nacked by Shutdown.
	recvCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	go func() {
		defer close(s.done)
		s.recvErr = sub.Receive(recvCtx, s.handOff)
	}()
	return s, nil
}

// holdNacks keeps nacks sent after Shutdown started until the stream has
// closed.
func (s *gcpLimitedSubscription) holdNacks(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	r, ok := req.(*pubsubpb.ModifyAckDeadlineRequest)
	if !ok || r.AckDeadlineSeconds != 0 || !s.stopping() || ctx.Value(gcpHeldNackKey{}) != nil {
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	s.mu.Lock()
	s.nacks = append(s.nacks, proto.Clone(r).(*pubsubpb.ModifyAckDeadlineRequest))
	s.nackMD = md
	s.mu.Unlock()
	return nil
}

// trackStream records the connection of each StreamingPull stream and when
// it ends. The stream is closed on that connection before the held nacks are
// sent on it, so Pub/Sub cannot send the nacked messages back to the stream.
func (s *gcpLimitedSubscription) trackStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	if method != "/google.pubsub.v1.Subscriber/StreamingPull" {
		return streamer(ctx, desc, cc, method, opts...)
	}
	done := make(chan struct{})
	var once sync.Once
	opts = append(opts, grpc.OnFinish(func(error) { once.Do(func() { close(done) }) }))
	cs, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.stream, s.streamDone = cc, done
	s.mu.Unlock()
	return cs, nil
}

func (s *gcpLimitedSubscription) sendHeldNacks(ctx context.Context) error {
	s.mu.Lock()
	nacks, md, cc, streamDone := s.nacks, s.nackMD, s.stream, s.streamDone
	s.nacks = nil
	s.mu.Unlock()
	if len(nacks) == 0 || cc == nil {
		return nil
	}
	select {
	case <-streamDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	ctx = context.WithValue(metadata.NewOutgoingContext(ctx, md), gcpHeldNackKey{}, true)
	var errs error
	for _, req := range nacks {
		if err := cc.Invoke(ctx, "/google.pubsub.v1.Subscriber/ModifyAckDeadline", req, &emptypb.Empty{}); err != nil {
			errs = errors.Join(errs, fmt.Errorf("nack: %w", err))
		}
	}
	return errs
}

func (s *gcpLimitedSubscription) stopping() bool {
	select {
	case <-s.release:
		return true
	default:
		return false
	}
}

func (s *gcpLimitedSubscription) handOff(_ context.Context, msg *nativepubsub.Message) {
	s.handoff.RLock()
	defer s.handoff.RUnlock()
	if s.stopping() {
		msg.Nack()
		return
	}
	m := &Message{
		QueueMessage: &gcpNativeAcker{msg: msg},
		LoggableID:   msg.ID,
		ID:           msg.ID,
		Body:         msg.Data,
	}
	select {
	case s.msgChan <- m:
	case <-s.release:
		msg.Nack()
	}
}

func (s *gcpLimitedSubscription) Receive(ctx context.Context) (*Message, error) {
	select {
	case msg := <-s.msgChan:
		return msg, nil
	case <-s.done:
		if s.recvErr != nil {
			return nil, fmt.Errorf("subscription closed: %w", s.recvErr)
		}
		return nil, fmt.Errorf("subscription closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Shutdown closes the stream, then nacks the messages not handed over.
func (s *gcpLimitedSubscription) Shutdown(ctx context.Context) error {
	var nackErr error
	s.stopOnce.Do(func() {
		close(s.release)
		s.handoff.Lock()
		s.handoff.Unlock()
		s.cancel()
		<-s.done

		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gcpShutdownNackTimeout)
		defer cancel()
		nackErr = s.sendHeldNacks(ctx)
	})
	<-s.done
	return errors.Join(nackErr, s.client.Close())
}

func (s *gcpLimitedSubscription) SupportsConcurrency() bool {
	return true
}
