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
// Received messages are handed to Receive one at a time. Messages not handed
// over when Shutdown is called are nacked while the stream is still open: the
// client sends the nacks it has recorded before it closes the stream, and
// drops those recorded after. Until then they stay in the process: a nack on
// an open stream would come straight back to it.
type gcpLimitedSubscription struct {
	msgChan  chan *Message
	release  chan struct{} // closed by Shutdown: nack instead of handing over
	handoff  sync.RWMutex  // held for reading by each hand-off in progress
	stopOnce sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
	client   *nativepubsub.Client
	recvErr  error // set before done is closed
}

var _ Subscription = &gcpLimitedSubscription{}
var _ ConcurrentSubscription = &gcpLimitedSubscription{}

func newGCPLimitedSubscription(ctx context.Context, client *nativepubsub.Client, sub *nativepubsub.Subscription) *gcpLimitedSubscription {
	// The stream outlives ctx so that messages received while handlers finish
	// can be nacked by Shutdown before it closes.
	recvCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &gcpLimitedSubscription{
		msgChan: make(chan *Message),
		release: make(chan struct{}),
		cancel:  cancel,
		done:    make(chan struct{}),
		client:  client,
	}
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
	case <-s.release:
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

// Shutdown nacks the messages not handed over, then closes the stream. It
// returns once the nacks are sent.
func (s *gcpLimitedSubscription) Shutdown(_ context.Context) error {
	s.stopOnce.Do(func() {
		close(s.release)
		// Wait for hand-offs in progress to nack.
		s.handoff.Lock()
		s.handoff.Unlock()
		s.cancel()
	})
	<-s.done
	return s.client.Close()
}

func (s *gcpLimitedSubscription) SupportsConcurrency() bool {
	return true
}
