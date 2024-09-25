package deliverymq

import (
	"context"

	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
)

type DeliveryInfra interface {
	DeclareInfrastructure(ctx context.Context) error
}

type DeliveryMQ struct {
	queueConfig *mqs.QueueConfig
	queue       mqs.Queue
	infra       DeliveryInfra
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

	// infra
	var infra DeliveryInfra
	if queueConfig == nil {
	} else if queueConfig.AWSSQS != nil {
		infra = NewDeliveryAWSInfra(queueConfig.AWSSQS)
	} else if queueConfig.AzureServiceBus != nil {
		// ...
	} else if queueConfig.GCPPubSub != nil {
		// ...
	} else if queueConfig.RabbitMQ != nil {
		infra = NewDeliveryRabbitMQInfra(queueConfig.RabbitMQ)
	}

	return &DeliveryMQ{
		queueConfig: queueConfig,
		queue:       queue,
		infra:       infra,
	}
}

func (q *DeliveryMQ) Init(ctx context.Context) (func(), error) {
	if q.infra != nil {
		err := q.infra.DeclareInfrastructure(ctx)
		if err != nil {
			return nil, err
		}
	}

	return q.queue.Init(ctx)
}

func (q *DeliveryMQ) Publish(ctx context.Context, event models.DeliveryEvent) error {
	return q.queue.Publish(ctx, &event)
}

func (q *DeliveryMQ) Subscribe(ctx context.Context) (mqs.Subscription, error) {
	return q.queue.Subscribe(ctx)
}
