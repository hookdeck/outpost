package mqs

import (
	"context"
	"sync"

	"github.com/rabbitmq/amqp091-go"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/rabbitpubsub"
)

type RabbitMQConfig struct {
	ServerURL string
	Exchange  string // optional
	Queue     string
}

type RabbitMQQueue struct {
	base   *wrappedBaseQueue
	once   *sync.Once
	conn   *amqp091.Connection
	config *RabbitMQConfig
	topic  *pubsub.Topic
}

var _ Queue = &RabbitMQQueue{}

func (q *RabbitMQQueue) Init(ctx context.Context) (func(), error) {
	var err error
	q.once.Do(func() {
		err = q.InitConn()
	})
	if err != nil {
		return nil, err
	}

	var opts *rabbitpubsub.TopicOptions
	if q.config.Queue != "" {
		opts = &rabbitpubsub.TopicOptions{
			KeyName: "Queue",
		}
	}

	q.topic = rabbitpubsub.OpenTopic(q.conn, q.config.Exchange, opts)
	return func() {
		q.conn.Close()
		q.topic.Shutdown(ctx)
	}, nil
}

func (q *RabbitMQQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	return q.base.Publish(ctx, q.topic, incomingMessage, map[string]string{
		"Queue": q.config.Queue,
	})
}

func (q *RabbitMQQueue) Subscribe(ctx context.Context) (Subscription, error) {
	var err error
	q.once.Do(func() {
		err = q.InitConn()
	})
	if err != nil {
		return nil, err
	}
	subscription := rabbitpubsub.OpenSubscription(q.conn, q.config.Queue, nil)
	return q.base.Subscribe(ctx, subscription)
}

func (q *RabbitMQQueue) InitConn() error {
	conn, err := amqp091.Dial(q.config.ServerURL)
	if err != nil {
		return err
	}
	q.conn = conn
	return nil
}

func NewRabbitMQQueue(config *RabbitMQConfig) *RabbitMQQueue {
	var once sync.Once
	return &RabbitMQQueue{config: config, once: &once, base: newWrappedBaseQueue()}
}
