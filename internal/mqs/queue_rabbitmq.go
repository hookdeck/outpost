package mqs

import (
	"context"
	"errors"

	"github.com/rabbitmq/amqp091-go"
	"github.com/spf13/viper"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/rabbitpubsub"
)

// ============================== Config ==============================

type RabbitMQConfig struct {
	ServerURL string
	Exchange  string
	Queue     string
}

// const (
// 	DefaultRabbitMQDeliveryExchange = "eventkit"
// 	DefaultRabbitMQDeliveryQueue    = "eventkit.delivery"
// )

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

	if c.RabbitMQ.Exchange == "" {
		return errors.New("RabbitMQ Exchange is not set")
	}

	if c.RabbitMQ.Queue == "" {
		return errors.New("RabbitMQ Queue is not set")
	}

	return nil
}

// // ============================== Queue ==============================

type RabbitMQQueue struct {
	conn   *amqp091.Connection
	config *RabbitMQConfig
	topic  *pubsub.Topic
}

var _ Queue = &RabbitMQQueue{}

func (q *RabbitMQQueue) Init(ctx context.Context) (func(), error) {
	conn, err := amqp091.Dial(q.config.ServerURL)
	if err != nil {
		return nil, err
	}
	err = q.declareInfrastructure(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	q.conn = conn
	q.topic = rabbitpubsub.OpenTopic(conn, q.config.Exchange, nil)
	return func() {
		conn.Close()
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
	subscription := rabbitpubsub.OpenSubscription(q.conn, q.config.Queue, nil)
	return wrappedSubscription(subscription)
}

func (q *RabbitMQQueue) declareInfrastructure(_ context.Context, conn *amqp091.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	err = ch.ExchangeDeclare(
		q.config.Exchange, // name
		"topic",           // type
		true,              // durable
		false,             // auto-deleted
		false,             // internal
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		return err
	}
	queue, err := ch.QueueDeclare(
		q.config.Queue, // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		return err
	}
	err = ch.QueueBind(
		queue.Name,        // queue name
		"",                // routing key
		q.config.Exchange, // exchange
		false,
		nil,
	)
	return err
}

func NewRabbitMQQueue(config *RabbitMQConfig) *RabbitMQQueue {
	return &RabbitMQQueue{config: config}
}
