package deliverymq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/scheduler"
)

func NewRetryScheduler(deliverymq *DeliveryMQ, redisConfig *redis.RedisConfig, deploymentID string, logger *logging.Logger) (scheduler.Scheduler, error) {
	// Create Redis client for RSMQ
	ctx := context.Background()
	redisClient, err := redis.New(ctx, redisConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client for retry scheduler: %w", err)
	}

	// Create RSMQ adapter
	adapter := rsmq.NewRedisAdapter(redisClient)

	// Construct RSMQ namespace with deployment prefix if provided
	// This creates keys like: deployment:dp_001:rsmq:QUEUES, deployment:dp_001:rsmq:deliverymq-retry:Q
	// Without deployment ID: rsmq:QUEUES, rsmq:deliverymq-retry:Q
	namespace := "rsmq"
	if deploymentID != "" {
		namespace = fmt.Sprintf("deployment:%s:rsmq", deploymentID)
	}

	// Create RSMQ client with deployment-aware namespace
	var rsmqClient *rsmq.RedisSMQ
	if logger != nil {
		rsmqClient = rsmq.NewRedisSMQ(adapter, namespace, logger)
	} else {
		rsmqClient = rsmq.NewRedisSMQ(adapter, namespace)
	}

	// Define execution function
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

	return scheduler.New("deliverymq-retry", rsmqClient, exec), nil
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
