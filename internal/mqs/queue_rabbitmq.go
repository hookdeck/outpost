package mqs

import (
	"context"
	"errors"
	"sync"

	"github.com/rabbitmq/amqp091-go"
	"github.com/spf13/viper"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/rabbitpubsub"
)

// ============================== Config ==============================

type RabbitMQConfig struct {
	ServerURL string
	Exchange  string // optional
	Queue     string
}

func (c *QueueConfig) parseRabbitMQConfig(viper *viper.Viper, prefix string) {
	if !viper.IsSet(prefix + "_RABBITMQ_SERVER_URL") {
		return
	}

	config := &RabbitMQConfig{}
	config.ServerURL = viper.GetString(prefix + "_RABBITMQ_SERVER_URL")
	config.Exchange = viper.GetString(prefix + "_RABBITMQ_EXCHANGE")
	config.Queue = viper.GetString(prefix + "_RABBITMQ_QUEUE")

	c.RabbitMQ = config
}

func (c *QueueConfig) validateRabbitMQConfig() error {
	if c.RabbitMQ == nil {
		return nil
	}

	if c.RabbitMQ.ServerURL == "" {
		return errors.New("RabbitMQ Server URL is not set")
	}

	if c.RabbitMQ.Queue == "" {
		return errors.New("RabbitMQ Queue is not set")
	}

	return nil
}

// // ============================== Queue ==============================

type RabbitMQQueue struct {
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
	q.topic = rabbitpubsub.OpenTopic(q.conn, q.config.Exchange, nil)
	return func() {
		q.conn.Close()
		q.topic.Shutdown(ctx)
	}, nil
}

func (q *RabbitMQQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	msg, err := incomingMessage.ToMessage()
	if err != nil {
		return err
	}
	return q.topic.Send(ctx, &pubsub.Message{Body: msg.Body})
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
	return wrappedSubscription(subscription)
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
	return &RabbitMQQueue{config: config, once: &once}
}
