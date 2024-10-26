package deliverymq

import (
	"context"
	"encoding/json"

	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/hookdeck/EventKit/internal/scheduler"
)

func NewRetryScheduler(deliverymq *DeliveryMQ, redisConfig *redis.RedisConfig) scheduler.Scheduler {
	exec := func(ctx context.Context, msg string) error {
		retryMessage := RetryMessage{}
		if err := retryMessage.FromString(msg); err != nil {
			return err
		}
		deliveryEvent := retryMessage.ToDeliveryEvent()
		if err := deliverymq.Publish(ctx, deliveryEvent); err != nil {
			return err
		}
		return nil
	}
	return scheduler.New("deliverymq-retry", redisConfig, exec)
}

type RetryMessage struct {
	DeliveryEventID string
	EventID         string
	TenantID        string
	DestinationID   string
	Attempt         int
	Telemetry       *models.DeliveryEventTelemetry
}

func (m *RetryMessage) ToString() (string, error) {
	json, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(json), nil
}

func (m *RetryMessage) FromString(str string) error {
	return json.Unmarshal([]byte(str), &m)
}

func (m *RetryMessage) ToDeliveryEvent() models.DeliveryEvent {
	return models.DeliveryEvent{
		ID:            m.DeliveryEventID,
		Attempt:       m.Attempt,
		DestinationID: m.DestinationID,
		Event:         models.Event{ID: m.EventID, TenantID: m.TenantID},
		Telemetry:     m.Telemetry,
	}
}

func RetryMessageFromDeliveryEvent(deliveryEvent models.DeliveryEvent) RetryMessage {
	return RetryMessage{
		DeliveryEventID: deliveryEvent.ID,
		EventID:         deliveryEvent.Event.ID,
		TenantID:        deliveryEvent.Event.TenantID,
		DestinationID:   deliveryEvent.DestinationID,
		Attempt:         deliveryEvent.Attempt + 1,
		Telemetry:       deliveryEvent.Telemetry,
	}
}
