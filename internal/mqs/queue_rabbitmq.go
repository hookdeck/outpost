package mqs

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/rabbitmq/amqp091-go"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/rabbitpubsub"
)

const rabbitmqRedialCooldown = 5 * time.Second

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
}

var _ Queue = &RabbitMQQueue{}

func (q *RabbitMQQueue) Init(ctx context.Context) (func(), error) {
	if _, _, err := q.ensureConnected(); err != nil {
		return nil, err
	}
	return func() {
		q.mu.Lock()
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
	subscription := rabbitpubsub.OpenSubscription(conn, q.config.Queue, nil)
	return q.base.Subscribe(ctx, subscription)
}

func (q *RabbitMQQueue) ensureConnected() (*pubsub.Topic, *amqp091.Connection, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
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
		Locale: "en_US",
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
