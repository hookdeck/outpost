package mqs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	nativepubsub "cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"go.uber.org/zap"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	gcpShutdownNackTimeout = 10 * time.Second

	gcpStreamingPullMethod     = "/google.pubsub.v1.Subscriber/StreamingPull"
	gcpModifyAckDeadlineMethod = "/google.pubsub.v1.Subscriber/ModifyAckDeadline"
)

type gcpSettleKey struct{}

// gcpLimitedSubscription is the StreamingPull subscription used with a byte
// limit. Pub/Sub holds the stream at MaxOutstandingMessages and
// MaxOutstandingBytes, so the backlog waits in the subscription.
//
// Messages not handed to Receive by Shutdown are nacked after the stream has
// closed.
type gcpLimitedSubscription struct {
	logger    *zap.Logger
	subName   string
	msgChan   chan *Message
	release   chan struct{} // closed by Shutdown: nack instead of handing over, hold nacks
	handOffMu sync.RWMutex  // held for reading by each hand-off in progress
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
	client    *nativepubsub.Client
	recvErr   error // set before done is closed

	mu         sync.Mutex
	nacks      []string // held until the stream has closed
	streamConn *grpc.ClientConn
	streamDone chan struct{}        // closed once the latest stream has ended
	unsettled  map[string]string    // ack ID -> message ID, received and not acked or nacked
	receivedAt map[string]time.Time // message ID -> when it came off the stream
	handedOver int
	heldMax    time.Duration
	heldOver1s int
}

var _ Subscription = &gcpLimitedSubscription{}
var _ ConcurrentSubscription = &gcpLimitedSubscription{}

func newGCPLimitedSubscription(ctx context.Context, projectID, subscriptionID string, settings nativepubsub.ReceiveSettings, clientOpts []option.ClientOption, logger *zap.Logger) (*gcpLimitedSubscription, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &gcpLimitedSubscription{
		logger:     logger.With(zap.String("subscription", subscriptionID)),
		msgChan:    make(chan *Message),
		release:    make(chan struct{}),
		done:       make(chan struct{}),
		unsettled:  map[string]string{},
		receivedAt: map[string]time.Time{},
	}
	// Ours first, so interceptors in clientOpts (tests) run inside them.
	clientOpts = append([]option.ClientOption{
		option.WithGRPCDialOption(grpc.WithChainUnaryInterceptor(s.holdSettlements)),
		option.WithGRPCDialOption(grpc.WithChainStreamInterceptor(s.trackStream)),
	}, clientOpts...)
	client, err := nativepubsub.NewClient(ctx, projectID, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create pubsub client: %w", err)
	}
	s.client = client
	sub := client.Subscription(subscriptionID)
	sub.ReceiveSettings = settings
	s.subName = sub.String()

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

func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// holdSettlements tracks acks and nacks, and holds nacks sent after
// Shutdown started until the stream has ended.
func (s *gcpLimitedSubscription) holdSettlements(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if ctx.Value(gcpSettleKey{}) != nil {
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	var ackIDs []string
	var hold, ack bool
	switch r := req.(type) {
	case *pubsubpb.AcknowledgeRequest:
		ackIDs, ack = r.AckIds, true
	case *pubsubpb.ModifyAckDeadlineRequest:
		if r.AckDeadlineSeconds != 0 {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		ackIDs, hold = r.AckIds, closed(s.release)
	default:
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	ids := s.settled(ackIDs)
	if hold {
		s.mu.Lock()
		s.nacks = append(s.nacks, ackIDs...)
		s.mu.Unlock()
	}
	if ack {
		s.logger.Debug("gcp pubsub ack", zap.Strings("message_ids", ids))
	} else {
		s.logger.Debug("gcp pubsub nack", zap.Strings("message_ids", ids), zap.Bool("held", hold))
	}
	if hold {
		return nil
	}
	err := invoker(ctx, method, req, reply, cc, opts...)
	if err != nil {
		s.logger.Debug("gcp pubsub settle failed", zap.Bool("ack", ack), zap.Int("count", len(ackIDs)), zap.Error(err))
	}
	return err
}

// trackStream records the connection of each StreamingPull stream, when it
// ends, and the messages it receives.
func (s *gcpLimitedSubscription) trackStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	if method != gcpStreamingPullMethod {
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
	s.streamConn, s.streamDone = cc, done
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
	s.logger.Debug("gcp pubsub stream received", zap.Int("count", len(msgs)), zap.Strings("message_ids", ids), zap.Bool("stopping", closed(s.release)))
}

// settled forgets the given ack IDs and returns their message IDs.
func (s *gcpLimitedSubscription) settled(ackIDs []string) []string {
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
	return ids
}

func (s *gcpLimitedSubscription) handOff(_ context.Context, msg *nativepubsub.Message) {
	s.handOffMu.RLock()
	defer s.handOffMu.RUnlock()
	if closed(s.release) {
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
		s.handedOverAt(msg.ID)
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

// handedOverAt logs how long a message was held between coming off the
// stream and being handed to the handler.
func (s *gcpLimitedSubscription) handedOverAt(id string) {
	s.mu.Lock()
	at, ok := s.receivedAt[id]
	delete(s.receivedAt, id)
	var held time.Duration
	if ok {
		held = time.Since(at)
		s.handedOver++
		s.heldMax = max(s.heldMax, held)
		if held > time.Second {
			s.heldOver1s++
		}
	}
	s.mu.Unlock()
	if ok {
		s.logger.Debug("gcp pubsub message handed over", zap.String("message_id", id), zap.Float64("held_ms", float64(held.Microseconds())/1000))
	}
}

// nackHeld sends the held nacks and a nack for every message received and
// not acked or nacked (the SDK drops nacks of messages it reads after its
// last flush), on the stream's connection once the stream has ended: Pub/Sub
// sees the stream close before the nacks.
func (s *gcpLimitedSubscription) nackHeld(ctx context.Context) (held, unsettled int, err error) {
	s.mu.Lock()
	nackIDs, cc, streamDone := s.nacks, s.streamConn, s.streamDone
	s.nacks = nil
	held = len(nackIDs)
	for a := range s.unsettled {
		nackIDs = append(nackIDs, a)
	}
	unsettled = len(s.unsettled)
	clear(s.unsettled)
	s.mu.Unlock()
	if len(nackIDs) == 0 || cc == nil {
		return held, unsettled, nil
	}
	select {
	case <-streamDone:
	case <-ctx.Done():
		return held, unsettled, fmt.Errorf("wait for the stream to end: %w", ctx.Err())
	}
	ctx = context.WithValue(ctx, gcpSettleKey{}, true)
	ctx = metadata.AppendToOutgoingContext(ctx, "x-goog-request-params", "subscription="+url.QueryEscape(s.subName))
	var errs error
	for ids := range chunk(nackIDs) {
		req := &pubsubpb.ModifyAckDeadlineRequest{Subscription: s.subName, AckIds: ids}
		if err := cc.Invoke(ctx, gcpModifyAckDeadlineMethod, req, &emptypb.Empty{}); err != nil {
			errs = errors.Join(errs, fmt.Errorf("nack: %w", err))
		}
	}
	return held, unsettled, errs
}

// chunk splits ack IDs into batches Pub/Sub accepts in one request.
func chunk(ids []string) func(func([]string) bool) {
	return func(yield func([]string) bool) {
		for len(ids) > 0 {
			n := min(len(ids), 2500)
			if !yield(ids[:n]) {
				return
			}
			ids = ids[n:]
		}
	}
}

// Shutdown closes the stream, then nacks the messages not handed over.
func (s *gcpLimitedSubscription) Shutdown(ctx context.Context) error {
	var nackErr error
	s.stopOnce.Do(func() {
		start := time.Now()
		close(s.release)
		s.handOffMu.Lock()
		s.handOffMu.Unlock()
		s.cancel()
		<-s.done

		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gcpShutdownNackTimeout)
		defer cancel()
		held, unsettled, err := s.nackHeld(ctx)
		nackErr = err
		s.mu.Lock()
		handed, heldMax, over := s.handedOver, s.heldMax, s.heldOver1s
		s.mu.Unlock()
		fields := []zap.Field{
			zap.Int("nacks_held", held), zap.Int("nacks_unsettled", unsettled),
			zap.Duration("took", time.Since(start)),
			zap.Int("handed_over", handed), zap.Duration("held_max", heldMax), zap.Int("held_over_1s", over),
		}
		if err != nil {
			s.logger.Warn("gcp pubsub shutdown: nack failed, the messages return at the end of their ack deadline", append(fields, zap.Error(err))...)
		} else {
			s.logger.Debug("gcp pubsub subscription shut down", fields...)
		}
	})
	<-s.done
	return errors.Join(nackErr, s.client.Close())
}

func (s *gcpLimitedSubscription) SupportsConcurrency() bool {
	return true
}
