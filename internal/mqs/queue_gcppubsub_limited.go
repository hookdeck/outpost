package mqs

import (
	"context"
	"fmt"
	"sync"

	nativepubsub "cloud.google.com/go/pubsub"
)

// gcpLimitedSubscription is the StreamingPull subscription used with a byte
// limit. Pub/Sub holds the stream at MaxOutstandingMessages and
// MaxOutstandingBytes, so the backlog waits in the subscription.
//
// Received messages are handed to Receive one at a time. When ctx ends or
// Shutdown is called, messages not handed over yet are nacked while the
// stream is still open: the client sends nacks recorded before it stops,
// and drops those recorded after.
type gcpLimitedSubscription struct {
	msgChan   chan *Message
	stopping  chan struct{}
	handoff   sync.RWMutex // held for reading by each hand-off in progress
	stopOnce  sync.Once
	stopAfter func() bool
	cancel    context.CancelFunc
	done      chan struct{}
	client    *nativepubsub.Client
	recvErr   error // set before done is closed
}

var _ Subscription = &gcpLimitedSubscription{}
var _ ConcurrentSubscription = &gcpLimitedSubscription{}

func newGCPLimitedSubscription(ctx context.Context, client *nativepubsub.Client, sub *nativepubsub.Subscription) *gcpLimitedSubscription {
	recvCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &gcpLimitedSubscription{
		msgChan:  make(chan *Message),
		stopping: make(chan struct{}),
		cancel:   cancel,
		done:     make(chan struct{}),
		client:   client,
	}
	s.stopAfter = context.AfterFunc(ctx, s.stop)
	go func() {
		defer close(s.done)
		s.recvErr = sub.Receive(recvCtx, s.handOff)
	}()
	return s
}

func (s *gcpLimitedSubscription) handOff(_ context.Context, msg *nativepubsub.Message) {
	s.handoff.RLock()
	defer s.handoff.RUnlock()
	select {
	case <-s.stopping:
		msg.Nack()
		return
	default:
	}
	m := &Message{
		QueueMessage: &gcpNativeAcker{msg: msg},
		LoggableID:   msg.ID,
		ID:           msg.ID,
		Body:         msg.Data,
	}
	select {
	case s.msgChan <- m:
	case <-s.stopping:
		msg.Nack()
	}
}

// stop nacks the messages not handed over, then closes the stream.
func (s *gcpLimitedSubscription) stop() {
	s.stopOnce.Do(func() {
		close(s.stopping)
		// Wait for hand-offs in progress to finish or nack.
		s.handoff.Lock()
		s.handoff.Unlock()
		s.cancel()
	})
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

// Shutdown returns once the stream is closed and the nacks are sent.
func (s *gcpLimitedSubscription) Shutdown(_ context.Context) error {
	s.stopAfter()
	s.stop()
	<-s.done
	return s.client.Close()
}

func (s *gcpLimitedSubscription) SupportsConcurrency() bool {
	return true
}
