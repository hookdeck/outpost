package logmq

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
)

type LogInfra interface {
	DeclareInfrastructure(ctx context.Context) error
}

type LogMQ struct {
	queueConfig *mqs.QueueConfig
	queue       mqs.Queue
}

type LogMQOption struct {
	QueueConfig *mqs.QueueConfig
}

func WithQueue(queueConfig *mqs.QueueConfig) func(opts *LogMQOption) {
	return func(opts *LogMQOption) {
		opts.QueueConfig = queueConfig
	}
}

func New(opts ...func(opts *LogMQOption)) *LogMQ {
	options := &LogMQOption{}
	for _, opt := range opts {
		opt(options)
	}
	queueConfig := options.QueueConfig
	queue := mqs.NewQueue(queueConfig)

	return &LogMQ{
		queueConfig: queueConfig,
		queue:       queue,
	}
}

func (q *LogMQ) Init(ctx context.Context) (func(), error) {
	return q.queue.Init(ctx)
}

func (q *LogMQ) Publish(ctx context.Context, event models.DeliveryEvent) error {
	return q.queue.Publish(ctx, &event)
}

func (q *LogMQ) Subscribe(ctx context.Context) (mqs.Subscription, error) {
	return q.queue.Subscribe(ctx)
}
