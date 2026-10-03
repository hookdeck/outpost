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
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	gcpNackTimeout = 10 * time.Second
	// Pub/Sub ends a half-closed stream about 2s after the half-close.
	gcpHalfCloseTimeout = 5 * time.Second

	gcpStreamingPullMethod     = "/google.pubsub.v1.Subscriber/StreamingPull"
	gcpModifyAckDeadlineMethod = "/google.pubsub.v1.Subscriber/ModifyAckDeadline"
)

type gcpSettleKey struct{}

// gcpLimitedSubscription is the StreamingPull subscription used with a byte
// limit. Pub/Sub holds the stream at MaxOutstandingMessages and
// MaxOutstandingBytes, so the backlog waits in the subscription.
//
// When the context passed to Subscribe ends, the subscription stops taking
// messages: it half-closes the stream, so Pub/Sub sends nothing more and ends
// it, while handlers still running ack or nack as usual. Messages not handed
// to Receive are nacked once the stream has ended.
type gcpLimitedSubscription struct {
	logger    *zap.Logger
	subName   string
	msgChan   chan *Message
	release   chan struct{} // closed when receiving stops: nack instead of handing over
	handOffMu sync.RWMutex  // held for reading by each hand-off in progress
	stopRecv  sync.Once
	stopOnce  sync.Once
	flushed   chan struct{} // closed once the stream has ended after receiving stopped and the nacks are sent
	cancel    context.CancelFunc
	done      chan struct{}
	client    *nativepubsub.Client
	recvErr   error // set before done is closed

	mu         sync.Mutex
	stopping   bool                 // receiving stopped: messages read from the stream are not passed on
	holdNacks  bool                 // from stopping until the stream has ended
	nacks      []gcpNack            // held until the stream has ended
	stream     *gcpTrackedStream    // latest stream; one at a time, see configureReceive
	unsettled  map[string]string    // ack ID -> message ID, received and not acked or nacked
	receivedAt map[string]time.Time // message ID -> when it came off the stream
	handedOver int
	heldMax    time.Duration
	heldOver1s int
	nackedMax  time.Duration // longest stream receipt -> nack sent by this subscription after receiving stopped
}

type gcpNack struct {
	ackID, id  string
	receivedAt time.Time
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
		flushed:    make(chan struct{}),
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

	// The SDK outlives ctx so handlers still running can ack; the stream is
	// half-closed when ctx ends.
	recvCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	go func() {
		defer close(s.done)
		s.recvErr = sub.Receive(recvCtx, s.handOff)
	}()
	go func() {
		select {
		case <-ctx.Done():
			s.stopReceiving()
		case <-s.done:
		}
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
// receiving stopped until the stream has ended.
func (s *gcpLimitedSubscription) holdSettlements(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if ctx.Value(gcpSettleKey{}) != nil {
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	var ackIDs []string
	var ack bool
	switch r := req.(type) {
	case *pubsubpb.AcknowledgeRequest:
		ackIDs, ack = r.AckIds, true
	case *pubsubpb.ModifyAckDeadlineRequest:
		if r.AckDeadlineSeconds != 0 {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		ackIDs = r.AckIds
	default:
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	s.mu.Lock()
	settled := s.settledLocked(ackIDs)
	hold := !ack && s.holdNacks
	if hold {
		s.nacks = append(s.nacks, settled...)
	}
	s.mu.Unlock()
	msg := "gcp pubsub nack"
	if ack {
		msg = "gcp pubsub ack"
	}
	if ce := s.logger.Check(zap.DebugLevel, msg); ce != nil {
		fields := []zap.Field{zap.Strings("message_ids", nackIDs(settled))}
		if !ack {
			fields = append(fields, zap.Bool("held", hold))
		}
		ce.Write(fields...)
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

// trackStream records each StreamingPull stream, when it ends, and the
// messages it receives. Once receiving has stopped, new streams are not
// opened.
func (s *gcpLimitedSubscription) trackStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	if method != gcpStreamingPullMethod {
		return streamer(ctx, desc, cc, method, opts...)
	}
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if stopping {
		return gcpIdleStream{ctx: ctx}, nil
	}
	streamCtx, cancel := context.WithCancel(ctx)
	t := &gcpTrackedStream{s: s, ctx: ctx, cancel: cancel, conn: cc, done: make(chan struct{})}
	var once sync.Once
	opts = append(opts, grpc.OnFinish(func(error) { once.Do(func() { cancel(); close(t.done) }) }))
	cs, err := streamer(streamCtx, desc, cc, method, opts...)
	if err != nil {
		cancel()
		return nil, err
	}
	t.ClientStream = cs
	s.mu.Lock()
	s.stream = t
	stopping = s.stopping
	s.mu.Unlock()
	s.logger.Debug("gcp pubsub stream opened")
	if stopping {
		t.stop()
	}
	return t, nil
}

// gcpTrackedStream passes the SDK what the stream receives until receiving
// stops. Then it half-closes the stream and passes nothing more on; the SDK
// sees a stream that stays open until the SDK itself stops, so it keeps
// sending acks and nacks and opens no new stream.
type gcpTrackedStream struct {
	grpc.ClientStream
	s      *gcpLimitedSubscription
	ctx    context.Context // the SDK's
	cancel context.CancelFunc
	conn   *grpc.ClientConn
	done   chan struct{} // closed once the stream has ended

	sendMu     sync.Mutex
	halfClosed bool

	ended bool // read by the SDK's receive goroutine only
}

func (t *gcpTrackedStream) SendMsg(m any) error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	if t.halfClosed {
		return nil
	}
	return t.ClientStream.SendMsg(m)
}

func (t *gcpTrackedStream) CloseSend() error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	if t.halfClosed {
		return nil
	}
	t.halfClosed = true
	return t.ClientStream.CloseSend()
}

// stop half-closes the stream. The SDK keeps reading it: hand-offs return
// at once once receiving has stopped.
func (t *gcpTrackedStream) stop() {
	if err := t.CloseSend(); err != nil {
		t.s.logger.Debug("gcp pubsub stream half-close failed", zap.Error(err))
		t.cancel()
		return
	}
	t.s.logger.Debug("gcp pubsub stream half-closed")
}

func (t *gcpTrackedStream) RecvMsg(m any) error {
	for !t.ended {
		err := t.ClientStream.RecvMsg(m)
		if err != nil {
			t.s.logger.Debug("gcp pubsub stream ended", zap.Error(err))
			if !t.s.isStopping() {
				return err
			}
			t.ended = true
			break
		}
		res, _ := m.(*pubsubpb.StreamingPullResponse)
		if t.s.received(res.GetReceivedMessages()) {
			return nil
		}
	}
	// Ended after receiving stopped: wait for the SDK to stop.
	<-t.ctx.Done()
	return status.FromContextError(t.ctx.Err()).Err()
}

// gcpIdleStream stands in for a stream the SDK opens after receiving
// stopped: it receives nothing until the SDK stops.
type gcpIdleStream struct {
	ctx context.Context
}

func (gcpIdleStream) Header() (metadata.MD, error) { return nil, nil }
func (gcpIdleStream) Trailer() metadata.MD         { return nil }
func (gcpIdleStream) CloseSend() error             { return nil }
func (i gcpIdleStream) Context() context.Context   { return i.ctx }
func (gcpIdleStream) SendMsg(any) error            { return nil }
func (i gcpIdleStream) RecvMsg(any) error {
	<-i.ctx.Done()
	return status.FromContextError(i.ctx.Err()).Err()
}

func (s *gcpLimitedSubscription) isStopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

// received records messages read from the stream. Once receiving has
// stopped they are not passed on: it returns false and holds their nacks.
func (s *gcpLimitedSubscription) received(msgs []*pubsubpb.ReceivedMessage) bool {
	if len(msgs) == 0 {
		return true
	}
	now := time.Now()
	s.mu.Lock()
	stopping := s.stopping
	for _, m := range msgs {
		id := m.GetMessage().GetMessageId()
		if stopping {
			s.nacks = append(s.nacks, gcpNack{ackID: m.AckId, id: id, receivedAt: now})
			continue
		}
		s.unsettled[m.AckId] = id
		s.receivedAt[id] = now
	}
	s.mu.Unlock()
	if ce := s.logger.Check(zap.DebugLevel, "gcp pubsub stream received"); ce != nil {
		ids := make([]string, len(msgs))
		for i, m := range msgs {
			ids[i] = m.GetMessage().GetMessageId()
		}
		ce.Write(zap.Int("count", len(msgs)), zap.Strings("message_ids", ids), zap.Bool("stopping", stopping))
	}
	return !stopping
}

// settledLocked forgets the given ack IDs and returns their messages.
func (s *gcpLimitedSubscription) settledLocked(ackIDs []string) []gcpNack {
	out := make([]gcpNack, 0, len(ackIDs))
	for _, a := range ackIDs {
		if id, ok := s.unsettled[a]; ok {
			out = append(out, gcpNack{ackID: a, id: id, receivedAt: s.receivedAt[id]})
			delete(s.receivedAt, id)
			delete(s.unsettled, a)
		}
	}
	return out
}

// stopReceiving stops taking messages: hand-offs nack, the stream is
// half-closed, and once it has ended the held nacks are sent.
func (s *gcpLimitedSubscription) stopReceiving() {
	s.stopRecv.Do(func() {
		start := time.Now()
		s.mu.Lock()
		s.stopping, s.holdNacks = true, true
		t := s.stream
		s.mu.Unlock()
		close(s.release)
		if t != nil {
			t.stop()
		}
		go s.flushAfterStreamEnd(t, start)
	})
}

func (s *gcpLimitedSubscription) flushAfterStreamEnd(t *gcpTrackedStream, start time.Time) {
	defer close(s.flushed)
	if t == nil {
		// No stream was open, so nothing was received. A stream opened
		// later is half-closed before its first request.
		s.mu.Lock()
		s.holdNacks = false
		s.mu.Unlock()
		return
	}
	select {
	case <-t.done:
	case <-time.After(gcpHalfCloseTimeout):
		s.logger.Debug("gcp pubsub stream did not end after the half-close, cancelling it")
		t.cancel()
		<-t.done
	}
	s.mu.Lock()
	nacks := s.nacks
	s.nacks, s.holdNacks = nil, false
	s.mu.Unlock()
	if len(nacks) == 0 {
		s.logger.Debug("gcp pubsub stream ended after receiving stopped", zap.Duration("took", time.Since(start)), zap.Int("nacks", 0))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), gcpNackTimeout)
	defer cancel()
	err := s.sendNacks(ctx, t.conn, nacks)
	fields := []zap.Field{zap.Duration("took", time.Since(start)), zap.Int("nacks", len(nacks)), zap.Strings("message_ids", nackIDs(nacks))}
	if err != nil {
		s.logger.Warn("gcp pubsub: nack after the stream ended failed, the messages return at the end of their ack deadline", append(fields, zap.Error(err))...)
		return
	}
	s.logger.Debug("gcp pubsub stream ended after receiving stopped", fields...)
}

func nackIDs(nacks []gcpNack) []string {
	ids := make([]string, len(nacks))
	for i, n := range nacks {
		ids[i] = n.id
	}
	return ids
}

// sendNacks nacks on the stream's connection, after the stream has ended.
func (s *gcpLimitedSubscription) sendNacks(ctx context.Context, cc *grpc.ClientConn, nacks []gcpNack) error {
	ctx = context.WithValue(ctx, gcpSettleKey{}, true)
	ctx = metadata.AppendToOutgoingContext(ctx, "x-goog-request-params", "subscription="+url.QueryEscape(s.subName))
	ackIDs := make([]string, len(nacks))
	for i, n := range nacks {
		ackIDs[i] = n.ackID
	}
	var errs error
	for ids := range chunk(ackIDs) {
		req := &pubsubpb.ModifyAckDeadlineRequest{Subscription: s.subName, AckIds: ids}
		if err := cc.Invoke(ctx, gcpModifyAckDeadlineMethod, req, &emptypb.Empty{}); err != nil {
			errs = errors.Join(errs, fmt.Errorf("nack: %w", err))
		}
	}
	if errs == nil {
		now := time.Now()
		s.mu.Lock()
		for _, n := range nacks {
			if !n.receivedAt.IsZero() {
				s.nackedMax = max(s.nackedMax, now.Sub(n.receivedAt))
			}
		}
		s.mu.Unlock()
	}
	return errs
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
	if !ok {
		return
	}
	if ce := s.logger.Check(zap.DebugLevel, "gcp pubsub message handed over"); ce != nil {
		ce.Write(zap.String("message_id", id), zap.Float64("held_ms", float64(held.Microseconds())/1000))
	}
}

// nackHeld sends the held nacks and a nack for every message received and
// not acked or nacked (the SDK drops nacks of messages it reads after its
// last flush), on the stream's connection once the stream has ended: Pub/Sub
// sees the stream close before the nacks.
func (s *gcpLimitedSubscription) nackHeld(ctx context.Context) (held, unsettled int, err error) {
	s.mu.Lock()
	nacks, t := s.nacks, s.stream
	s.nacks = nil
	held = len(nacks)
	for a, id := range s.unsettled {
		nacks = append(nacks, gcpNack{ackID: a, id: id, receivedAt: s.receivedAt[id]})
	}
	unsettled = len(s.unsettled)
	clear(s.unsettled)
	s.mu.Unlock()
	if len(nacks) == 0 || t == nil {
		return held, unsettled, nil
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return held, unsettled, fmt.Errorf("wait for the stream to end: %w", ctx.Err())
	}
	return held, unsettled, s.sendNacks(ctx, t.conn, nacks)
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

// Shutdown stops receiving if the context passed to Subscribe has not
// ended, waits for the stream to end, stops the SDK, then nacks the messages
// not handed over and not settled.
func (s *gcpLimitedSubscription) Shutdown(ctx context.Context) error {
	var nackErr error
	s.stopOnce.Do(func() {
		start := time.Now()
		s.stopReceiving()
		<-s.flushed
		s.handOffMu.Lock()
		s.handOffMu.Unlock()
		s.cancel()
		<-s.done

		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gcpNackTimeout)
		defer cancel()
		held, unsettled, err := s.nackHeld(ctx)
		nackErr = err
		s.mu.Lock()
		handed, heldMax, over, nackedMax := s.handedOver, s.heldMax, s.heldOver1s, s.nackedMax
		s.mu.Unlock()
		fields := []zap.Field{
			zap.Int("nacks_held", held), zap.Int("nacks_unsettled", unsettled),
			zap.Duration("took", time.Since(start)),
			zap.Int("handed_over", handed), zap.Duration("held_max", heldMax), zap.Int("held_over_1s", over),
			zap.Duration("nacked_held_max", nackedMax),
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
