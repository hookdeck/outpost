package mqs

import (
	"context"
	"errors"
	"sync"
)

var errSubscriptionShutdown = errors.New("subscription shut down")

// limitBytes bounds the total body size of messages received from sub and not
// yet settled (Ack, Nack or Reject). A message larger than maxBytes is let
// through when nothing else is in flight. maxBytes <= 0 returns sub unchanged.
func limitBytes(sub Subscription, maxBytes int64) Subscription {
	if maxBytes <= 0 {
		return sub
	}
	return &byteLimitSubscription{
		inner: sub,
		limit: maxBytes,
		done:  make(chan struct{}),
	}
}

type byteLimitSubscription struct {
	inner Subscription
	limit int64

	mu       sync.Mutex
	bytes    int64
	count    int
	released chan struct{} // closed on the next release; nil when nobody waits
	shutdown bool
	done     chan struct{}
	waiting  sync.WaitGroup
}

var _ Subscription = &byteLimitSubscription{}

// Receive returns the next message once its body fits in the limit. The
// message is already received while it waits. If ctx ends or the subscription
// shuts down first it is not returned: it is nacked, and the broker
// redelivers it, at the latest once its visibility timeout or ack deadline
// passes.
func (s *byteLimitSubscription) Receive(ctx context.Context) (*Message, error) {
	msg, err := s.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	size := int64(len(msg.Body))

	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		msg.Nack()
		return nil, errSubscriptionShutdown
	}
	s.waiting.Add(1)
	s.mu.Unlock()
	defer s.waiting.Done()

	if err := s.acquire(ctx, size); err != nil {
		msg.Nack()
		return nil, err
	}

	settle := &byteLimitMessage{QueueMessage: msg.QueueMessage, sub: s, size: size}
	if _, ok := msg.QueueMessage.(Rejecter); ok {
		msg.QueueMessage = byteLimitRejectableMessage{settle}
	} else {
		msg.QueueMessage = settle
	}
	return msg, nil
}

func (s *byteLimitSubscription) acquire(ctx context.Context, size int64) error {
	for {
		s.mu.Lock()
		if s.count == 0 || s.bytes+size <= s.limit {
			s.bytes += size
			s.count++
			s.mu.Unlock()
			return nil
		}
		if s.released == nil {
			s.released = make(chan struct{})
		}
		released := s.released
		s.mu.Unlock()

		select {
		case <-released:
		case <-s.done:
			return errSubscriptionShutdown
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *byteLimitSubscription) release(size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytes -= size
	s.count--
	if s.released != nil {
		close(s.released)
		s.released = nil
	}
}

func (s *byteLimitSubscription) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.shutdown {
		s.shutdown = true
		close(s.done)
	}
	s.mu.Unlock()
	// Waiting receives nack their message before the inner subscription
	// shuts down. Whether that nack reaches the broker is up to the inner
	// Shutdown.
	s.waiting.Wait()
	return s.inner.Shutdown(ctx)
}

type byteLimitMessage struct {
	QueueMessage
	sub  *byteLimitSubscription
	size int64
	once sync.Once
}

func (m *byteLimitMessage) Ack() {
	m.QueueMessage.Ack()
	m.release()
}

func (m *byteLimitMessage) Nack() {
	m.QueueMessage.Nack()
	m.release()
}

func (m *byteLimitMessage) release() {
	m.once.Do(func() { m.sub.release(m.size) })
}

// byteLimitRejectableMessage wraps messages that implement Rejecter, so
// Message.Rejectable reports the same with or without the limit.
type byteLimitRejectableMessage struct {
	*byteLimitMessage
}

var _ Rejecter = byteLimitRejectableMessage{}

func (m byteLimitRejectableMessage) Reject() {
	m.QueueMessage.(Rejecter).Reject()
	m.release()
}
