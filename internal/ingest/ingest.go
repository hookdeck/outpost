package ingest

import (
	"context"
	"encoding/json"
	"time"

	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/mempubsub"
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

func (e *Event) FromMessage(msg *pubsub.Message) error {
	return json.Unmarshal(msg.Body, e)
}

func (e *Event) ToMessage() (*pubsub.Message, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &pubsub.Message{Body: data}, nil
}

type Ingestor struct {
	queue IngestQueue
}

func New(config *IngestConfig) (*Ingestor, error) {
	queue, err := NewQueue(config)
	if err != nil {
		return nil, err
	}
	return &Ingestor{queue: queue}, nil
}

func (i *Ingestor) Init(ctx context.Context) (func(), error) {
	return i.queue.Init(ctx)
}

func (i *Ingestor) Publish(ctx context.Context, event Event) error {
	return i.queue.Publish(ctx, event)
}

func (i *Ingestor) Subscribe(ctx context.Context) (Subscription, error) {
	return i.queue.Subscribe(ctx)
}
