package mqs

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/mempubsub"
)

type QueueConfig struct {
	AWSSQS          *AWSSQSConfig
	AzureServiceBus *AzureServiceBusConfig
	GCPPubSub       *GCPPubSubConfig
	RabbitMQ        *RabbitMQConfig
	InMemory        *InMemoryConfig // mainly for testing purposes
}

type InMemoryConfig struct {
	Name string
}

type Queue interface {
	Init(ctx context.Context) (func(), error)
	Publish(ctx context.Context, msg IncomingMessage) error
	Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error)
}

type Subscription interface {
	Receive(ctx context.Context) (*Message, error)
	Shutdown(ctx context.Context) error
}

// ConcurrentSubscription indicates a subscription that manages its own concurrency
// internally (e.g. via SDK flow control). When true, the consumer should skip its
// own semaphore-based concurrency limiting.
type ConcurrentSubscription interface {
	SupportsConcurrency() bool
}

// SubscribeOption configures subscription behavior.
type SubscribeOption func(*SubscribeOptions)

// SubscribeOptions holds options for Subscribe.
type SubscribeOptions struct {
	Concurrency int
}

// WithConcurrency sets the max in-flight messages for the subscription.
func WithConcurrency(n int) SubscribeOption {
	return func(o *SubscribeOptions) { o.Concurrency = n }
}

// ApplySubscribeOptions applies all options and returns the result.
func ApplySubscribeOptions(opts []SubscribeOption) SubscribeOptions {
	var o SubscribeOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
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
	LoggableID string
	Body       []byte
}

func NewQueue(config *QueueConfig) Queue {
	if config == nil {
		return NewInMemoryQueue(nil)
	}
	if config.AWSSQS != nil {
		return NewAWSQueue(config.AWSSQS)
	} else if config.AzureServiceBus != nil {
		return NewAzureServiceBusQueue(config.AzureServiceBus)
	} else if config.GCPPubSub != nil {
		return NewGCPPubSubQueue(config.GCPPubSub)
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

func (q *UnimplementedQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	return nil, errors.New("unimplemented")
}

// ============================== In-memory Queue ==============================

type InMemoryQueue struct {
	base      *wrappedBaseQueue
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
	return q.base.Publish(ctx, q.topic, incomingMessage, nil)
}

func (q *InMemoryQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	subscription, err := pubsub.OpenSubscription(ctx, q.topicName)
	if err != nil {
		return nil, err
	}
	return q.base.Subscribe(ctx, subscription)
}

func NewInMemoryQueue(config *InMemoryConfig) *InMemoryQueue {
	name := ""
	if config != nil {
		name = config.Name
	}
	return &InMemoryQueue{
		base:      newWrappedBaseQueue(),
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
		LoggableID:   msg.LoggableID,
		Body:         msg.Body,
	}, nil
}

func (s *WrappedSubscription) Shutdown(ctx context.Context) error {
	return s.subscription.Shutdown(ctx)
}

func wrappedSubscription(subscription *pubsub.Subscription) (Subscription, error) {
	return &WrappedSubscription{subscription: subscription}, nil
}

// ============================== Base Queue Impl ==============================

type wrappedBaseQueue struct {
	once   *sync.Once
	tracer trace.Tracer
}

func newWrappedBaseQueue() *wrappedBaseQueue {
	var once sync.Once
	return &wrappedBaseQueue{once: &once}
}

func (q *wrappedBaseQueue) initTracer() {
	q.tracer = otel.GetTracerProvider().Tracer("github.com/hookdeck/outpost/internal/mqs")
}

func (q *wrappedBaseQueue) Publish(ctx context.Context, topic *pubsub.Topic, incomingMessage IncomingMessage, metadata map[string]string) error {
	q.once.Do(q.initTracer)
	ctx, span := q.tracer.Start(ctx, "Queue.Publish")
	defer span.End()

	msg, err := incomingMessage.ToMessage()
	if err != nil {
		span.RecordError(err)
		return err
	}
	err = topic.Send(ctx, &pubsub.Message{Body: msg.Body, Metadata: metadata})
	if err != nil {
		span.RecordError(err)
		return err
	}
	return err
}

func (q *wrappedBaseQueue) Subscribe(ctx context.Context, subscription *pubsub.Subscription) (Subscription, error) {
	return wrappedSubscription(subscription)
}
