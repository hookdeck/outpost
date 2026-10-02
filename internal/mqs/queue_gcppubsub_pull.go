package mqs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	vkit "cloud.google.com/go/pubsub/apiv1"
	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

const (
	// Most messages asked for and not yet received, over all pulls in flight.
	// Also the most one Pull request may ask for.
	gcpPullMaxRequested = 1000
	// Count limit when the subscriber sets none.
	gcpPullDefaultMaxCount = 1000
	// Most pulls in flight. They wait at the service while the subscription
	// is empty.
	gcpPullMaxParallel = 4
	// The largest body is kept for one to two periods.
	gcpPullSizePeriod = 30 * time.Second
	// Bytes one pull response carries at most. Pub/Sub does not document it;
	// measured responses stop at about 10 MB. A larger response, once seen,
	// takes its place.
	gcpPullResponseBytes = 10 << 20
	// Shortest time between the start of a pull that returned nothing and the
	// next pull, in case the service answers an empty subscription at once.
	gcpPullEmptyInterval = 250 * time.Millisecond
	gcpPullRetryDelay    = time.Second

	// Ack and nack requests.
	gcpAckBatchSize  = 1000
	gcpAckTimeout    = 5 * time.Second
	gcpAckAttempts   = 3
	gcpAckRetryDelay = 200 * time.Millisecond
)

// gcpSubscriberAPI is the part of the Pub/Sub subscriber client the pull
// subscription uses.
type gcpSubscriberAPI interface {
	Pull(context.Context, *pubsubpb.PullRequest, ...gax.CallOption) (*pubsubpb.PullResponse, error)
	Acknowledge(context.Context, *pubsubpb.AcknowledgeRequest, ...gax.CallOption) error
	ModifyAckDeadline(context.Context, *pubsubpb.ModifyAckDeadlineRequest, ...gax.CallOption) error
	Close() error
}

func newGCPSubscriberClient(ctx context.Context, opts ...option.ClientOption) (*vkit.SubscriberClient, error) {
	if addr := os.Getenv("PUBSUB_EMULATOR_HOST"); addr != "" {
		// The emulator takes no credentials.
		return vkit.NewSubscriberClient(ctx,
			option.WithEndpoint(addr),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			option.WithoutAuthentication(),
			option.WithTelemetryDisabled(),
		)
	}
	base := []option.ClientOption{
		option.WithGRPCDialOption(grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time: 5 * time.Minute,
		})),
	}
	return vkit.NewSubscriberClient(ctx, append(base, opts...)...)
}

// gcpPullSubscription receives with unary Pull requests sized to the room left
// under the count and byte limits. A message is asked for only when it can go
// to a handler, so the backlog stays in the subscription, with no ack deadline
// running and no delivery attempt counted.
//
// Pulls are kept waiting at the service for all the room there is, so a
// message is received as soon as it is published. Sizes are not known before
// a pull: it reserves what one response can carry, or less when the largest
// body seen recently says its messages cannot add up to that. Messages larger
// than their reservation still go to handlers and the next pull waits for
// their bytes, so the process holds at most the limit plus what the pulls in
// flight return beyond their reservations.
type gcpPullSubscription struct {
	api      gcpSubscriberAPI
	path     string
	maxCount int
	maxBytes int64

	ctx      context.Context
	cancel   context.CancelFunc
	msgs     chan *Message
	closed   chan struct{} // closed once pulling has stopped
	pulling  sync.WaitGroup
	haltOnce sync.Once

	sendWake chan struct{}
	sendStop chan struct{}
	sendDone chan struct{}
	stopOnce sync.Once

	mu        sync.Mutex
	stopped   bool
	err       error
	heldCount int // received and not settled
	heldBytes int64
	reqCount  int // asked for, response pending
	reqBytes  int64
	pulls     int
	sizes     gcpSizeWindow
	response  int64 // most bytes one pull response carries
	acks      []string
	nacks     []string
}

var _ Subscription = &gcpPullSubscription{}
var _ ConcurrentSubscription = &gcpPullSubscription{}

func newGCPPullSubscription(ctx context.Context, api gcpSubscriberAPI, path string, o SubscribeOptions) *gcpPullSubscription {
	maxCount := o.Concurrency
	if maxCount <= 0 {
		maxCount = gcpPullDefaultMaxCount
	}
	subCtx, cancel := context.WithCancel(ctx)
	s := &gcpPullSubscription{
		api:      api,
		path:     path,
		maxCount: maxCount,
		maxBytes: o.MaxBytes,
		ctx:      subCtx,
		cancel:   cancel,
		msgs:     make(chan *Message, maxCount),
		closed:   make(chan struct{}),
		sendWake: make(chan struct{}, 1),
		sendStop: make(chan struct{}),
		sendDone: make(chan struct{}),
		response: gcpPullResponseBytes,
	}
	go s.sendLoop()
	go func() {
		<-subCtx.Done()
		s.halt()
	}()
	s.schedule()
	return s
}

func (s *gcpPullSubscription) Receive(ctx context.Context) (*Message, error) {
	select {
	case msg := <-s.msgs:
		return msg, nil
	case <-s.closed:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("subscription closed: %w", err)
		}
		return nil, errors.New("subscription closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SupportsConcurrency returns true: the count limit is part of what a pull
// may ask for, so the consumer skips its own semaphore.
func (s *gcpPullSubscription) SupportsConcurrency() bool {
	return true
}

// Shutdown returns once every message that was received and not handed to
// the caller has been nacked, and every ack and nack made so far has been
// sent.
func (s *gcpPullSubscription) Shutdown(_ context.Context) error {
	s.halt()
	s.stopOnce.Do(func() { close(s.sendStop) })
	<-s.sendDone
	return s.api.Close()
}

// halt stops pulling and nacks the messages nobody has received yet.
func (s *gcpPullSubscription) halt() {
	s.haltOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		s.cancel()
		s.pulling.Wait()
		for {
			select {
			case msg := <-s.msgs:
				msg.Nack()
			default:
				close(s.closed)
				return
			}
		}
	})
}

func (s *gcpPullSubscription) schedule() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for !s.stopped && s.ctx.Err() == nil {
		n, reserved := s.nextPull(time.Now())
		if n == 0 {
			return
		}
		s.pulls++
		s.reqCount += n
		s.reqBytes += reserved
		s.pulling.Add(1)
		go s.pull(n, reserved)
	}
}

// nextPull returns how many messages to ask for now and the bytes they
// reserve, or 0 when there is no room.
func (s *gcpPullSubscription) nextPull(now time.Time) (int, int64) {
	if s.pulls >= gcpPullMaxParallel {
		return 0, 0
	}
	free := min(s.maxCount-s.heldCount-s.reqCount, gcpPullMaxRequested-s.reqCount)
	if free < 1 {
		return 0, 0
	}
	room := s.maxBytes - s.heldBytes - s.reqBytes
	// Largest body seen recently, 0 when none was.
	largest := s.sizes.largest(now)
	// reserve returns the most the response to a pull for n messages can
	// carry: a whole response, or n times the largest body if that is less.
	reserve := func(n int) int64 {
		if largest == 0 {
			return s.response
		}
		return min(int64(n)*largest, s.response)
	}

	// A pull takes about as long for one message as for many, so each asks
	// for a share of the count limit: a quarter, or more when the byte limit
	// has no room for four pulls of that size.
	most := min(s.maxCount, gcpPullMaxRequested)
	parallel := int(min(max(s.maxBytes/reserve(most), 1), gcpPullMaxParallel))
	share := (most + parallel - 1) / parallel
	n := min(free, share)
	reserved := reserve(n)
	// While others are out, the next pull waits until it can ask for a whole
	// share, so that handlers finishing one by one do not use up the pulls in
	// flight.
	enough := share
	if reserved > room {
		if reserved <= s.maxBytes-s.heldBytes {
			// It fits once the pulls in flight are back.
			return 0, 0
		}
		// The messages held leave less room than a response. Ask for what
		// fits at the largest size, a quarter of the limit or more at a time.
		if largest == 0 {
			// No size to go by: one message.
			if s.reqCount > 0 || room <= 0 {
				return 0, 0
			}
			return 1, 0
		}
		n = int(min(room/largest, int64(free)))
		if n < 1 {
			// Messages larger than the whole limit run one at a time.
			if s.heldCount == 0 && s.reqCount == 0 {
				return 1, largest
			}
			return 0, 0
		}
		reserved = int64(n) * largest
		enough = int(min(int64(share), max((s.maxBytes/largest+gcpPullMaxParallel-1)/gcpPullMaxParallel, 1)))
	}
	if n < enough && s.pulls > 0 {
		return 0, 0
	}
	return n, reserved
}

func (s *gcpPullSubscription) pull(n int, reserved int64) {
	defer s.pulling.Done()
	start := time.Now()
	resp, err := s.api.Pull(s.ctx, &pubsubpb.PullRequest{Subscription: s.path, MaxMessages: int32(n)})
	received := resp.GetReceivedMessages()

	var fatal error
	switch {
	case err == nil:
	case s.ctx.Err() != nil:
	case status.Code(err) == codes.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded):
		// The long poll ran out with nothing to return.
		err = nil
	case gcpPullRetryable(err):
		log.Printf("gcppubsub: pull from %s failed, retrying: %v", s.path, err)
		s.sleep(gcpPullRetryDelay)
	default:
		fatal = err
	}
	if err == nil && len(received) == 0 {
		s.sleep(gcpPullEmptyInterval - time.Since(start))
	}

	now := time.Now()
	nacked := false
	s.mu.Lock()
	s.pulls--
	s.reqCount -= n
	s.reqBytes -= reserved
	if fatal != nil && s.err == nil {
		s.err = fatal
		s.stopped = true
	}
	var total int64
	for _, rm := range received {
		total += int64(len(rm.GetMessage().GetData()))
	}
	s.response = max(s.response, total)
	for _, rm := range received {
		size := int64(len(rm.GetMessage().GetData()))
		s.sizes.add(now, size)
		msg := &Message{
			QueueMessage: &gcpPullMessage{sub: s, ackID: rm.GetAckId(), size: size},
			LoggableID:   rm.GetMessage().GetMessageId(),
			ID:           rm.GetMessage().GetMessageId(),
			Body:         rm.GetMessage().GetData(),
		}
		if !s.stopped {
			// There is room: at most maxCount messages are held or asked for.
			select {
			case s.msgs <- msg:
				s.heldCount++
				s.heldBytes += size
				continue
			default:
			}
		}
		s.nacks = append(s.nacks, rm.GetAckId())
		nacked = true
	}
	s.mu.Unlock()

	if nacked {
		s.wakeSender()
	}
	if fatal != nil {
		s.cancel()
		return
	}
	s.schedule()
}

func gcpPullRetryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.Internal, codes.Aborted, codes.Unknown, codes.ResourceExhausted, codes.Canceled:
		return true
	}
	return false
}

func (s *gcpPullSubscription) sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-s.ctx.Done():
	}
}

func (s *gcpPullSubscription) settle(ackID string, size int64, ack bool) {
	s.mu.Lock()
	s.heldCount--
	s.heldBytes -= size
	if ack {
		s.acks = append(s.acks, ackID)
	} else {
		s.nacks = append(s.nacks, ackID)
	}
	s.mu.Unlock()
	s.wakeSender()
	s.schedule()
}

func (s *gcpPullSubscription) wakeSender() {
	select {
	case s.sendWake <- struct{}{}:
	default:
	}
}

// sendLoop sends acks and nacks as soon as the previous request is done:
// whatever was settled in the meantime goes out in one request.
func (s *gcpPullSubscription) sendLoop() {
	defer close(s.sendDone)
	for {
		select {
		case <-s.sendWake:
			s.flush()
		case <-s.sendStop:
			s.flush()
			return
		}
	}
}

func (s *gcpPullSubscription) flush() {
	for {
		s.mu.Lock()
		acks, nacks := s.acks, s.nacks
		s.acks, s.nacks = nil, nil
		s.mu.Unlock()
		if len(acks) == 0 && len(nacks) == 0 {
			return
		}
		for ids := range slices.Chunk(nacks, gcpAckBatchSize) {
			s.send("nack", len(ids), func(ctx context.Context) error {
				return s.api.ModifyAckDeadline(ctx, &pubsubpb.ModifyAckDeadlineRequest{
					Subscription:       s.path,
					AckIds:             ids,
					AckDeadlineSeconds: 0,
				})
			})
		}
		for ids := range slices.Chunk(acks, gcpAckBatchSize) {
			s.send("ack", len(ids), func(ctx context.Context) error {
				return s.api.Acknowledge(ctx, &pubsubpb.AcknowledgeRequest{
					Subscription: s.path,
					AckIds:       ids,
				})
			})
		}
	}
}

func (s *gcpPullSubscription) send(kind string, count int, call func(context.Context) error) {
	var err error
	for attempt := range gcpAckAttempts {
		if attempt > 0 {
			time.Sleep(gcpAckRetryDelay)
		}
		ctx, cancel := context.WithTimeout(context.Background(), gcpAckTimeout)
		err = call(ctx)
		cancel()
		if err == nil {
			return
		}
	}
	log.Printf("gcppubsub: %s of %d messages on %s failed, they return after the ack deadline: %v", kind, count, s.path, err)
}

type gcpPullMessage struct {
	sub   *gcpPullSubscription
	ackID string
	size  int64
	once  sync.Once
}

func (m *gcpPullMessage) Ack() {
	m.once.Do(func() { m.sub.settle(m.ackID, m.size, true) })
}

func (m *gcpPullMessage) Nack() {
	m.once.Do(func() { m.sub.settle(m.ackID, m.size, false) })
}

// gcpSizeWindow keeps the largest body size seen in the current and the
// previous period.
type gcpSizeWindow struct {
	current, previous int64
	started           time.Time
}

func (w *gcpSizeWindow) roll(now time.Time) {
	switch age := now.Sub(w.started); {
	case age >= 2*gcpPullSizePeriod:
		w.current, w.previous, w.started = 0, 0, now
	case age >= gcpPullSizePeriod:
		w.current, w.previous, w.started = 0, w.current, w.started.Add(gcpPullSizePeriod)
	}
}

func (w *gcpSizeWindow) add(now time.Time, size int64) {
	w.roll(now)
	// An empty body still counts as seen.
	w.current = max(w.current, size, 1)
}

func (w *gcpSizeWindow) largest(now time.Time) int64 {
	w.roll(now)
	return max(w.current, w.previous)
}
