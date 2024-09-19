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

type Ingestor struct {
	queue mqs.Queue
}

type IngestorOption struct {
	Queue mqs.Queue
}

func WithQueue(queueConfig *mqs.QueueConfig) func(opts *IngestorOption) {
	return func(opts *IngestorOption) {
		opts.Queue = mqs.NewQueue(queueConfig)
	}
}

func New(opts ...func(opts *IngestorOption)) *Ingestor {
	options := &IngestorOption{}
	for _, opt := range opts {
		opt(options)
	}
	if options.Queue == nil {
		options.Queue = mqs.NewQueue(nil)
	}
	return &Ingestor{queue: options.Queue}
}

func (i *Ingestor) Init(ctx context.Context) (func(), error) {
	return i.queue.Init(ctx)
}

func (i *Ingestor) Publish(ctx context.Context, event Event) error {
	return i.queue.Publish(ctx, &event)
}

func (i *Ingestor) Subscribe(ctx context.Context) (mqs.Subscription, error) {
	return i.queue.Subscribe(ctx)
}
