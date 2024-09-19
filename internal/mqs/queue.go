package mqs

import (
	"context"
	"errors"

	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/mempubsub"
)

type Queue interface {
	Init(ctx context.Context) (func(), error)
	Publish(ctx context.Context, msg IncomingMessage) error
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

type IncomingMessage interface {
	ToMessage() (*Message, error)
	FromMessage(msg *Message) error
}

type Message struct {
	QueueMessage
	Body []byte
}

func NewQueue(config *QueueConfig) Queue {
	if config == nil {
		return NewInMemoryQueue(nil)
	}
	if config.AWSSQS != nil {
		return NewAWSQueue(config.AWSSQS)
	} else if config.AzureServiceBus != nil {
		return &UnimplementedQueue{}
	} else if config.GCPPubSub != nil {
		return &UnimplementedQueue{}
	} else if config.RabbitMQ != nil {
		return NewRabbitMQQueue(config.RabbitMQ)
	} else {
		return NewInMemoryQueue(config.InMemory)
	}
}

// ============================== Unimplemented Queue ==============================

type UnimplementedQueue struct{}

var _ Queue = &UnimplementedQueue{}

func (q *UnimplementedQueue) Init(ctx context.Context) (func(), error) {
	return nil, errors.New("unimplemented")
}

func (q *UnimplementedQueue) Publish(ctx context.Context, msg IncomingMessage) error {
	return errors.New("unimplemented")
}

func (q *UnimplementedQueue) Subscribe(ctx context.Context) (Subscription, error) {
	return nil, errors.New("unimplemented")
}

// ============================== In-memory Queue ==============================

type InMemoryQueue struct {
	topicName string
	topic     *pubsub.Topic
}

var _ Queue = &InMemoryQueue{}

func (q *InMemoryQueue) Init(ctx context.Context) (func(), error) {
	topic, err := pubsub.OpenTopic(ctx, q.topicName)
	if err != nil {
		return nil, err
	}
	q.topic = topic
	return func() { topic.Shutdown(ctx) }, nil
}

func (q *InMemoryQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	msg, err := incomingMessage.ToMessage()
	if err != nil {
		return err
	}
	return q.topic.Send(ctx, &pubsub.Message{Body: msg.Body})
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
		topicName: "mem://queue" + name,
	}
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
	return &Message{
		QueueMessage: msg,
		Body:         msg.Body,
	}, nil
}

func (s *WrappedSubscription) Shutdown(ctx context.Context) error {
	return s.subscription.Shutdown(ctx)
}

func wrappedSubscription(subscription *pubsub.Subscription) (Subscription, error) {
	return &WrappedSubscription{subscription: subscription}, nil
}
