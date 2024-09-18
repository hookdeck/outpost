package ingest

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
	ServerURL       string
	PublishExchange string
	PublishQueue    string
}

const (
	DefaultRabbitMQPublishExchange = "eventkit"
	DefaultRabbitMQPublishQueue    = "eventkit.publish"
)

func (c *IngestConfig) parseRabbitMQConfig(viper *viper.Viper) {
	if !viper.IsSet("RABBITMQ_SERVER_URL") {
		return
	}

	config := &RabbitMQConfig{}
	config.ServerURL = viper.GetString("RABBITMQ_SERVER_URL")

	if viper.IsSet("RABBITMQ_PUBLISH_EXCHANGE") {
		config.PublishExchange = viper.GetString("RABBITMQ_PUBLISH_EXCHANGE")
	} else {
		config.PublishExchange = DefaultRabbitMQPublishExchange
	}

	if viper.IsSet("RABBITMQ_PUBLISH_QUEUE") {
		config.PublishQueue = viper.GetString("RABBITMQ_PUBLISH_QUEUE")
	} else {
		config.PublishQueue = DefaultRabbitMQPublishQueue
	}

	c.RabbitMQ = config
}

func (c *IngestConfig) validateRabbitMQConfig() error {
	if c.RabbitMQ == nil {
		return nil
	}

	if c.RabbitMQ.ServerURL == "" {
		return errors.New("RabbitMQ Server URL is not set")
	}

	if c.RabbitMQ.PublishExchange == "" {
		return errors.New("RabbitMQ Publish Exchange is not set")
	}

	if c.RabbitMQ.PublishQueue == "" {
		return errors.New("RabbitMQ Publish Queue is not set")
	}

	return nil
}

// ============================== Queue ==============================

type RabbitMQQueue struct {
	conn   *amqp091.Connection
	config *RabbitMQConfig
	topic  *pubsub.Topic
}

var _ IngestQueue = &RabbitMQQueue{}

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
	q.topic = rabbitpubsub.OpenTopic(conn, q.config.PublishExchange, nil)
	return func() {
		conn.Close()
		q.topic.Shutdown(ctx)
	}, nil
}

func (q *RabbitMQQueue) Publish(ctx context.Context, event Event) error {
	msg, err := event.ToMessage()
	if err != nil {
		return err
	}
	return q.topic.Send(ctx, msg)
}

func (q *RabbitMQQueue) Subscribe(ctx context.Context) (Subscription, error) {
	subscription := rabbitpubsub.OpenSubscription(q.conn, q.config.PublishQueue, nil)
	return wrappedSubscription(subscription)
}

func (q *RabbitMQQueue) declareInfrastructure(_ context.Context, conn *amqp091.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	err = ch.ExchangeDeclare(
		q.config.PublishExchange, // name
		"topic",                  // type
		true,                     // durable
		false,                    // auto-deleted
		false,                    // internal
		false,                    // no-wait
		nil,                      // arguments
	)
	if err != nil {
		return err
	}
	queue, err := ch.QueueDeclare(
		q.config.PublishQueue, // name
		true,                  // durable
		false,                 // delete when unused
		false,                 // exclusive
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		return err
	}
	err = ch.QueueBind(
		queue.Name,               // queue name
		"",                       // routing key
		q.config.PublishExchange, // exchange
		false,
		nil,
	)
	return err
}

func NewRabbitMQQueue(config *RabbitMQConfig) *RabbitMQQueue {
	return &RabbitMQQueue{config: config}
}
