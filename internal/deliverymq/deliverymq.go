package deliverymq

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
)

type DeliveryInfra interface {
	DeclareInfrastructure(ctx context.Context) error
}

type DeliveryMQ struct {
	queueConfig *mqs.QueueConfig
	queue       mqs.Queue
}

type DeliveryMQOption struct {
	QueueConfig *mqs.QueueConfig
}

func WithQueue(queueConfig *mqs.QueueConfig) func(opts *DeliveryMQOption) {
	return func(opts *DeliveryMQOption) {
		opts.QueueConfig = queueConfig
	}
}

func New(opts ...func(opts *DeliveryMQOption)) *DeliveryMQ {
	options := &DeliveryMQOption{}
	for _, opt := range opts {
		opt(options)
	}
	queueConfig := options.QueueConfig
	queue := mqs.NewQueue(queueConfig)

	return &DeliveryMQ{
		queueConfig: queueConfig,
		queue:       queue,
	}
}

func (q *DeliveryMQ) Init(ctx context.Context) (func(), error) {
	return q.queue.Init(ctx)
}

func (q *DeliveryMQ) Publish(ctx context.Context, event models.DeliveryEvent) error {
	return q.queue.Publish(ctx, &event)
}

func (q *DeliveryMQ) Subscribe(ctx context.Context) (mqs.Subscription, error) {
	return q.queue.Subscribe(ctx)
}
