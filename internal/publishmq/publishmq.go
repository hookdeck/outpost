package publishmq

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqs"
)

type PublishMQ struct {
	queueConfig *mqs.QueueConfig
	queue       mqs.Queue
}

type PublishMQOption struct {
	QueueConfig *mqs.QueueConfig
}

func WithQueue(queueConfig *mqs.QueueConfig) func(opts *PublishMQOption) {
	return func(opts *PublishMQOption) {
		opts.QueueConfig = queueConfig
	}
}

func New(opts ...func(opts *PublishMQOption)) *PublishMQ {
	options := &PublishMQOption{}
	for _, opt := range opts {
		opt(options)
	}
	queueConfig := options.QueueConfig
	queue := mqs.NewQueue(queueConfig)

	return &PublishMQ{
		queueConfig: queueConfig,
		queue:       queue,
	}
}

func (q *PublishMQ) Subscribe(ctx context.Context) (mqs.Subscription, error) {
	return q.queue.Subscribe(ctx)
}
