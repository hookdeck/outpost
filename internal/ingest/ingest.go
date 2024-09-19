package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hookdeck/EventKit/internal/mqs"
)

type Event struct {
	ID               string                 `json:"id"`
	TenantID         string                 `json:"tenant_id"`
	DestinationID    string                 `json:"destination_id"`
	Topic            string                 `json:"topic"`
	EligibleForRetry bool                   `json:"eligible_for_retry"`
	Time             time.Time              `json:"time"`
	Metadata         map[string]string      `json:"metadata"`
	Data             map[string]interface{} `json:"data"`
}

var _ mqs.IncomingMessage = &Event{}

func (e *Event) FromMessage(msg *mqs.Message) error {
	return json.Unmarshal(msg.Body, e)
}

func (e *Event) ToMessage() (*mqs.Message, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &mqs.Message{Body: data}, nil
}

type DeliveryInfra interface {
	DeclareInfrastructure(ctx context.Context) error
}

type Ingestor struct {
	queueConfig *mqs.QueueConfig
	queue       mqs.Queue
	infra       DeliveryInfra
}

type IngestorOption struct {
	QueueConfig *mqs.QueueConfig
}

func WithQueue(queueConfig *mqs.QueueConfig) func(opts *IngestorOption) {
	return func(opts *IngestorOption) {
		opts.QueueConfig = queueConfig
	}
}

func New(opts ...func(opts *IngestorOption)) *Ingestor {
	options := &IngestorOption{}
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

	return &Ingestor{
		queueConfig: queueConfig,
		queue:       queue,
		infra:       infra,
	}
}

func (i *Ingestor) Init(ctx context.Context) (func(), error) {
	if i.infra != nil {
		err := i.infra.DeclareInfrastructure(ctx)
		if err != nil {
			return nil, err
		}
	}

	return i.queue.Init(ctx)
}

func (i *Ingestor) Publish(ctx context.Context, event Event) error {
	return i.queue.Publish(ctx, &event)
}

func (i *Ingestor) Subscribe(ctx context.Context) (mqs.Subscription, error) {
	return i.queue.Subscribe(ctx)
}
