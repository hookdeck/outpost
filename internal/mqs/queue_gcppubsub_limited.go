package mqs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	nativepubsub "cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"go.uber.org/zap"
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
	logger   *zap.Logger
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
	stream     *grpc.ClientConn     // connection of the latest stream
	streamDone chan struct{}        // closed once that stream has ended
	unsettled  map[string]string    // ack ID -> message ID, received and not acked or nacked
	receivedAt map[string]time.Time // message ID -> when it came off the stream
}

var _ Subscription = &gcpLimitedSubscription{}
var _ ConcurrentSubscription = &gcpLimitedSubscription{}

func newGCPLimitedSubscription(ctx context.Context, projectID, subscriptionID string, settings nativepubsub.ReceiveSettings, clientOpts []option.ClientOption, logger *zap.Logger) (*gcpLimitedSubscription, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &gcpLimitedSubscription{
		logger:     logger.With(zap.String("subscription", subscriptionID)),
		unsettled:  map[string]string{},
		receivedAt: map[string]time.Time{},
		msgChan:    make(chan *Message),
		release:    make(chan struct{}),
		done:       make(chan struct{}),
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
	if a, ok := req.(*pubsubpb.AcknowledgeRequest); ok {
		s.settled(a.AckIds, true)
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			s.logger.Debug("gcp pubsub ack failed", zap.Int("count", len(a.AckIds)), zap.Error(err))
		}
		return err
	}
	r, ok := req.(*pubsubpb.ModifyAckDeadlineRequest)
	if ok && r.AckDeadlineSeconds == 0 && ctx.Value(gcpHeldNackKey{}) == nil {
		s.settled(r.AckIds, false)
	}
	if !ok || r.AckDeadlineSeconds != 0 || !s.stopping() || ctx.Value(gcpHeldNackKey{}) != nil {
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil && ok {
			s.logger.Debug("gcp pubsub modify ack deadline failed", zap.Int32("deadline", r.AckDeadlineSeconds), zap.Int("count", len(r.AckIds)), zap.Error(err))
		}
		return err
	}
	s.logger.Debug("gcp pubsub nack held until the stream has closed", zap.Int("count", len(r.AckIds)))
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
	s.logger.Debug("gcp pubsub stream opened")
	return &gcpTrackedStream{ClientStream: cs, s: s}, nil
}

type gcpTrackedStream struct {
	grpc.ClientStream
	s *gcpLimitedSubscription
}

func (t *gcpTrackedStream) RecvMsg(m any) error {
	err := t.ClientStream.RecvMsg(m)
	if err != nil {
		t.s.logger.Debug("gcp pubsub stream ended", zap.Error(err))
		return err
	}
	if res, ok := m.(*pubsubpb.StreamingPullResponse); ok && len(res.ReceivedMessages) > 0 {
		t.s.received(res.ReceivedMessages)
	}
	return nil
}

func (s *gcpLimitedSubscription) received(msgs []*pubsubpb.ReceivedMessage) {
	now := time.Now()
	ids := make([]string, len(msgs))
	s.mu.Lock()
	for i, m := range msgs {
		id := m.GetMessage().GetMessageId()
		ids[i] = id
		s.unsettled[m.AckId] = id
		s.receivedAt[id] = now
	}
	s.mu.Unlock()
	s.logger.Debug("gcp pubsub stream received", zap.Int("count", len(msgs)), zap.Strings("message_ids", ids), zap.Bool("stopping", s.stopping()))
}

func (s *gcpLimitedSubscription) settled(ackIDs []string, ack bool) {
	ids := make([]string, 0, len(ackIDs))
	s.mu.Lock()
	for _, a := range ackIDs {
		if id, ok := s.unsettled[a]; ok {
			ids = append(ids, id)
			delete(s.receivedAt, id)
			delete(s.unsettled, a)
		}
	}
	s.mu.Unlock()
	if ack {
		s.logger.Debug("gcp pubsub ack", zap.Strings("message_ids", ids))
	} else {
		s.logger.Debug("gcp pubsub nack", zap.Strings("message_ids", ids), zap.Bool("stopping", s.stopping()))
	}
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
		s.mu.Lock()
		at, ok := s.receivedAt[msg.ID]
		delete(s.receivedAt, msg.ID)
		s.mu.Unlock()
		if ok {
			s.logger.Debug("gcp pubsub message handed over", zap.String("message_id", msg.ID), zap.Float64("held_ms", float64(time.Since(at).Microseconds())/1000))
		}
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
		s.mu.Lock()
		held := 0
		for _, r := range s.nacks {
			held += len(r.AckIds)
		}
		s.mu.Unlock()
		nackErr = s.sendHeldNacks(ctx)
		s.mu.Lock()
		left := make([]string, 0, len(s.unsettled))
		for _, id := range s.unsettled {
			left = append(left, id)
		}
		s.mu.Unlock()
		s.logger.Debug("gcp pubsub subscription shut down", zap.Int("nacks_held", held), zap.Strings("unsettled_message_ids", left), zap.Error(nackErr))
	})
	<-s.done
	return errors.Join(nackErr, s.client.Close())
}

func (s *gcpLimitedSubscription) SupportsConcurrency() bool {
	return true
}
