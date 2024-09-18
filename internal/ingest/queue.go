package ingest

import (
	"context"
	"errors"

	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/mempubsub"
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
