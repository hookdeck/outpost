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
	infra       LogInfra
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

	// infra
	var infra LogInfra
	if queueConfig == nil {
	} else if queueConfig.AWSSQS != nil {
		// ...
	} else if queueConfig.AzureServiceBus != nil {
		// ...
	} else if queueConfig.GCPPubSub != nil {
		// ...
	} else if queueConfig.RabbitMQ != nil {
		infra = NewLogRabbitMQInfra(queueConfig.RabbitMQ)
	}

	return &LogMQ{
		queueConfig: queueConfig,
		queue:       queue,
		infra:       infra,
	}
}

func (q *LogMQ) Init(ctx context.Context) (func(), error) {
	if q.infra != nil {
		err := q.infra.DeclareInfrastructure(ctx)
		if err != nil {
			return nil, err
		}
	}

	return q.queue.Init(ctx)
}

func (q *LogMQ) Publish(ctx context.Context, event models.DeliveryEvent) error {
	return q.queue.Publish(ctx, &event)
}

func (q *LogMQ) Subscribe(ctx context.Context) (mqs.Subscription, error) {
	return q.queue.Subscribe(ctx)
}
