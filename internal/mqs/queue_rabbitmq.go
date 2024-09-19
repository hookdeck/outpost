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
	ServerURL        string
	DeliveryExchange string
	DeliveryQueue    string
}

const (
	DefaultRabbitMQDeliveryExchange = "eventkit"
	DefaultRabbitMQDeliveryQueue    = "eventkit.delivery"
)

func (c *QueueConfig) parseRabbitMQConfig(viper *viper.Viper) {
	if !viper.IsSet("DELIVERY_RABBITMQ_SERVER_URL") {
		return
	}

	config := &RabbitMQConfig{}
	config.ServerURL = viper.GetString("DELIVERY_RABBITMQ_SERVER_URL")

	if viper.IsSet("DELIVERY_RABBITMQ_EXCHANGE") {
		config.DeliveryExchange = viper.GetString("DELIVERY_RABBITMQ_EXCHANGE")
	} else {
		config.DeliveryExchange = DefaultRabbitMQDeliveryExchange
	}

	if viper.IsSet("DELIVERY_RABBITMQ_QUEUE") {
		config.DeliveryQueue = viper.GetString("DELIVERY_RABBITMQ_QUEUE")
	} else {
		config.DeliveryQueue = DefaultRabbitMQDeliveryQueue
	}

	c.RabbitMQ = config
}

func (c *QueueConfig) validateRabbitMQConfig() error {
	if c.RabbitMQ == nil {
		return nil
	}

	if c.RabbitMQ.ServerURL == "" {
		return errors.New("RabbitMQ Server URL is not set")
	}

	if c.RabbitMQ.DeliveryExchange == "" {
		return errors.New("RabbitMQ Delivery Exchange is not set")
	}

	if c.RabbitMQ.DeliveryQueue == "" {
		return errors.New("RabbitMQ Delivery Queue is not set")
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
	q.topic = rabbitpubsub.OpenTopic(conn, q.config.DeliveryExchange, nil)
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
	subscription := rabbitpubsub.OpenSubscription(q.conn, q.config.DeliveryQueue, nil)
	return wrappedSubscription(subscription)
}

func (q *RabbitMQQueue) declareInfrastructure(_ context.Context, conn *amqp091.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	err = ch.ExchangeDeclare(
		q.config.DeliveryExchange, // name
		"topic",                   // type
		true,                      // durable
		false,                     // auto-deleted
		false,                     // internal
		false,                     // no-wait
		nil,                       // arguments
	)
	if err != nil {
		return err
	}
	queue, err := ch.QueueDeclare(
		q.config.DeliveryQueue, // name
		true,                   // durable
		false,                  // delete when unused
		false,                  // exclusive
		false,                  // no-wait
		nil,                    // arguments
	)
	if err != nil {
		return err
	}
	err = ch.QueueBind(
		queue.Name,                // queue name
		"",                        // routing key
		q.config.DeliveryExchange, // exchange
		false,
		nil,
	)
	return err
}

func NewRabbitMQQueue(config *RabbitMQConfig) *RabbitMQQueue {
	return &RabbitMQQueue{config: config}
}
