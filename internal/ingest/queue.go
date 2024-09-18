package ingest

import (
	"context"
	"errors"

	"github.com/rabbitmq/amqp091-go"
	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/mempubsub"
	"gocloud.dev/pubsub/rabbitpubsub"
)

type IngestQueue interface {
	Init(ctx context.Context) (func(), error)
	Publish(ctx context.Context, event Event) error
	Subscribe(ctx context.Context) (Subscription, error)
}

type Subscription interface {
	Receive(ctx context.Context) (*Message, error)
	Shutdown(ctx context.Context) error
}

type QueueMessage interface {
	Ack()
	Nack()
}

type Message struct {
	QueueMessage
	Event Event
}

func NewQueue(config *IngestConfig) (IngestQueue, error) {
	if config.AWSSQS != nil {
		return NewAWSQueue(config.AWSSQS), nil
	} else if config.AzureServiceBus != nil {
		return nil, errors.New("Azure Service Bus queue is not implemented")
	} else if config.GCPPubSub != nil {
		return nil, errors.New("GCP PubSub queue is not implemented")
	} else if config.RabbitMQ != nil {
		return NewRabbitMQQueue(config.RabbitMQ), nil
	} else {
		return NewInMemoryQueue(config.InMemory), nil
	}
}

// ============================== In-memory Queue ==============================

type InMemoryQueue struct {
	topicName string
	topic     *pubsub.Topic
}

var _ IngestQueue = &InMemoryQueue{}

func (q *InMemoryQueue) Init(ctx context.Context) (func(), error) {
	topic, err := pubsub.OpenTopic(ctx, q.topicName)
	if err != nil {
		return nil, err
	}
	q.topic = topic
	return func() { topic.Shutdown(ctx) }, nil
}

func (q *InMemoryQueue) Publish(ctx context.Context, event Event) error {
	msg, err := event.ToMessage()
	if err != nil {
		return err
	}
	return q.topic.Send(ctx, msg)
}

func (q *InMemoryQueue) Subscribe(ctx context.Context) (Subscription, error) {
	subscription, err := pubsub.OpenSubscription(ctx, q.topicName)
	if err != nil {
		return nil, err
	}
	return wrappedSubscription(subscription)
}

func NewInMemoryQueue(config *InMemoryConfig) *InMemoryQueue {
	name := ""
	if config != nil {
		name = config.Name
	}
	return &InMemoryQueue{
		topicName: "mem://delivery" + name,
	}
}

// ============================== RabbitMQ ==============================

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

// ============================== GoCloud PubSub Wrapper ==============================

type WrappedSubscription struct {
	subscription *pubsub.Subscription
}

var _ Subscription = &WrappedSubscription{}

func (s *WrappedSubscription) Receive(ctx context.Context) (*Message, error) {
	msg, err := s.subscription.Receive(ctx)
	if err != nil {
		return nil, err
	}
	event := Event{}
	if err := event.FromMessage(msg); err != nil {
		return nil, err
	}
	return &Message{
		QueueMessage: msg,
		Event:        event,
	}, nil
}

func (s *WrappedSubscription) Shutdown(ctx context.Context) error {
	return s.subscription.Shutdown(ctx)
}

func wrappedSubscription(subscription *pubsub.Subscription) (Subscription, error) {
	return &WrappedSubscription{subscription: subscription}, nil
}
