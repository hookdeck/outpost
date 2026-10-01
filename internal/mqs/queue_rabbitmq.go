package mqs

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/rabbitmq/amqp091-go"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/rabbitpubsub"
)

const rabbitmqRedialCooldown = 5 * time.Second

var errRabbitMQQueueClosed = errors.New("rabbitmq queue closed")

type RabbitMQConfig struct {
	ServerURL string
	Exchange  string // optional
	Queue     string
	// Dial, when set, opens the broker connection in place of a direct TCP
	// dial, e.g. through a proxy chain.
	Dial proxychain.DialFunc
}

type RabbitMQQueue struct {
	base            *wrappedBaseQueue
	conn            *amqp091.Connection
	config          *RabbitMQConfig
	topic           *pubsub.Topic
	mu              sync.Mutex
	lastDialFailure time.Time
	lastDialErr     error
	closed          bool
}

var _ Queue = &RabbitMQQueue{}

func (q *RabbitMQQueue) Init(ctx context.Context) (func(), error) {
	if _, _, err := q.ensureConnected(); err != nil {
		return nil, err
	}
	return func() {
		q.mu.Lock()
		q.closed = true
		conn, topic := q.conn, q.topic
		q.mu.Unlock()
		if conn != nil {
			conn.Close()
		}
		if topic != nil {
			topic.Shutdown(ctx)
		}
	}, nil
}

func (q *RabbitMQQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	topic, _, err := q.ensureConnected()
	if err != nil {
		return err
	}
	metadata := map[string]string{"Queue": q.config.Queue}
	err = q.base.Publish(ctx, topic, incomingMessage, metadata)
	if err == nil || !q.connectionLost() {
		return err
	}
	topic, _, rerr := q.ensureConnected()
	if rerr != nil {
		return err
	}
	return q.base.Publish(ctx, topic, incomingMessage, metadata)
}

func (q *RabbitMQQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	_, conn, err := q.ensureConnected()
	if err != nil {
		return nil, err
	}
	return &rabbitMQSubscription{
		queue:        q,
		conn:         conn,
		subscription: rabbitpubsub.OpenSubscription(conn, q.config.Queue, nil),
	}, nil
}

func (q *RabbitMQQueue) ensureConnected() (*pubsub.Topic, *amqp091.Connection, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil, nil, errRabbitMQQueueClosed
	}
	if q.conn != nil && !q.conn.IsClosed() {
		return q.topic, q.conn, nil
	}
	if q.lastDialErr != nil && time.Since(q.lastDialFailure) < rabbitmqRedialCooldown {
		return nil, nil, q.lastDialErr
	}
	conn, err := q.dial()
	if err != nil {
		q.lastDialFailure = time.Now()
		q.lastDialErr = err
		return nil, nil, err
	}
	q.lastDialErr = nil
	if oldTopic := q.topic; oldTopic != nil {
		go oldTopic.Shutdown(context.Background())
	}
	var opts *rabbitpubsub.TopicOptions
	if q.config.Queue != "" {
		opts = &rabbitpubsub.TopicOptions{
			KeyName: "Queue",
		}
	}
	q.conn = conn
	q.topic = rabbitpubsub.OpenTopic(conn, q.config.Exchange, opts)
	return q.topic, q.conn, nil
}

func (q *RabbitMQQueue) connectionLost() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.conn == nil || q.conn.IsClosed()
}

// rabbitMQSubscription reopens the underlying subscription after its connection
// closes, since a gocloud subscription stays in a permanent error state once its
// connection is gone.
type rabbitMQSubscription struct {
	queue        *RabbitMQQueue
	mu           sync.Mutex
	conn         *amqp091.Connection
	subscription *pubsub.Subscription
	shutdown     bool
}

var _ Subscription = &rabbitMQSubscription{}

// Receive returns the error that revealed a dropped connection after
// resubscribing, so the caller's backoff still applies to the reconnect.
func (s *rabbitMQSubscription) Receive(ctx context.Context) (*Message, error) {
	s.mu.Lock()
	conn, subscription := s.conn, s.subscription
	s.mu.Unlock()

	msg, err := subscription.Receive(ctx)
	if err != nil {
		if conn.IsClosed() {
			s.resubscribe(subscription)
		}
		return nil, err
	}
	// LoggableID falls back to the delivery tag, which changes on every
	// redelivery, so ID reads the message ID from the delivery.
	var delivery amqp091.Delivery
	msg.As(&delivery)
	return &Message{
		QueueMessage: &rabbitMQMessage{Message: msg},
		LoggableID:   msg.LoggableID,
		ID:           delivery.MessageId,
		Body:         msg.Body,
	}, nil
}

// rabbitMQMessage adds Reject, since gocloud's Nack always requeues.
type rabbitMQMessage struct {
	*pubsub.Message
}

var _ Rejecter = &rabbitMQMessage{}

func (m *rabbitMQMessage) Reject() {
	var delivery amqp091.Delivery
	if !m.As(&delivery) {
		m.Nack()
		return
	}
	// Settled outside gocloud, so drop its "never acked" finalizer.
	runtime.SetFinalizer(m.Message, nil)
	// A failed nack means the channel is gone, and the broker requeues the
	// message on its own.
	_ = delivery.Nack(false, false)
}

func (s *rabbitMQSubscription) resubscribe(dead *pubsub.Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Another Receive may have already replaced it.
	if s.shutdown || s.subscription != dead {
		return
	}
	_, conn, err := s.queue.ensureConnected()
	if err != nil {
		return
	}
	s.conn = conn
	s.subscription = rabbitpubsub.OpenSubscription(conn, s.queue.config.Queue, nil)
	go dead.Shutdown(context.Background())
}

func (s *rabbitMQSubscription) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.shutdown = true
	subscription := s.subscription
	s.mu.Unlock()
	return subscription.Shutdown(ctx)
}

func NewRabbitMQQueue(config *RabbitMQConfig) *RabbitMQQueue {
	return &RabbitMQQueue{config: config, base: newWrappedBaseQueue()}
}

func (q *RabbitMQQueue) dial() (*amqp091.Connection, error) {
	if q.config.Dial == nil {
		return amqp091.Dial(q.config.ServerURL)
	}
	uri, err := amqp091.ParseURI(q.config.ServerURL)
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if uri.ConnectionTimeout != 0 {
		timeout = time.Duration(uri.ConnectionTimeout) * time.Millisecond
	}
	return amqp091.DialConfig(q.config.ServerURL, amqp091.Config{
		Dial: func(network, addr string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			conn, err := q.config.Dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			// Same handshake deadline as amqp091's default dialer; the
			// library clears it once the connection is open.
			if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
				conn.Close()
				return nil, err
			}
			return conn, nil
		},
	})
}
