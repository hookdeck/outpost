package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
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

type Ingestor struct {
	logger      *otelzap.Logger
	redisClient *redis.Client
	topic       *pubsub.Topic
}

func getDeliveryTopic() string {
	return "mem://delivery"
}

func New(logger *otelzap.Logger, redisClient *redis.Client) *Ingestor {
	return &Ingestor{
		logger:      logger,
		redisClient: redisClient,
	}
}

func (i *Ingestor) OpenDeliveryTopic(ctx context.Context) (func(), error) {
	topic, err := pubsub.OpenTopic(ctx, getDeliveryTopic())
	if err != nil {
		return nil, err
	}
	i.topic = topic
	return func() {
		topic.Shutdown(ctx)
	}, nil
}

func (i *Ingestor) OpenSubscriptionDeliveryTopic(ctx context.Context) (*pubsub.Subscription, error) {
	return pubsub.OpenSubscription(ctx, getDeliveryTopic())
}

func (i *Ingestor) Ingest(ctx context.Context, event Event) error {
	marshaledEvent, err := json.Marshal(event)
	if err != nil {
		i.logger.Ctx(ctx).Error("failed to marshal event", zap.Error(err))
		return err
	}
	i.logger.Ctx(ctx).Info("ingest", zap.String("event", string(marshaledEvent)))

	// Temporarily save the event in Redis for debugging purposes.
	err = i.redisClient.Set(ctx, "event:"+event.ID, marshaledEvent, 0).Err()
	if err != nil {
		i.logger.Ctx(ctx).Error("failed to save event", zap.Error(err))
		return err
	}

	err = i.publish(ctx, marshaledEvent)
	if err != nil {
		i.logger.Ctx(ctx).Error("failed to publish event", zap.Error(err))
		return err
	}
	return nil
}

func (i *Ingestor) publish(ctx context.Context, messageBody []byte) error {
	return i.topic.Send(ctx, &pubsub.Message{
		Body: messageBody,
	})
}

func (e *Event) FromMessage(msg *pubsub.Message) error {
	return json.Unmarshal(msg.Body, e)
}
