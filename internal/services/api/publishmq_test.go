package api_test

import (
	"context"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/services/api"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIServicePublishMQConsumer(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	publishQueueConfig := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
	deliveryQueueConfig := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}

	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	apiService, err := api.NewService(ctx, wg,
		&config.Config{
			Redis:               testutil.CreateTestRedisConfig(t),
			PublishQueueConfig:  &publishQueueConfig,
			DeliveryQueueConfig: &deliveryQueueConfig,
		},
		testutil.CreateTestLogger(t),
	)
	require.Nil(t, err, "should create API service without error")

	errchan := make(chan error)

	// ===== Act =====

	// Initialize publishmq
	publishMQ := mqs.NewQueue(&publishQueueConfig)
	cleanupPublishMQ, err := publishMQ.Init(ctx)
	require.Nil(t, err)
	defer cleanupPublishMQ()

	// Run API service
	err = apiService.Run(ctx)
	require.Nil(t, err, "should run API service without error")

	// Subscribe to deliverymq
	readychan := make(chan struct{})
	messages := []*mqs.Message{}
	go func() {
		defer close(errchan)

		deliveryMQ := mqs.NewQueue(&deliveryQueueConfig)
		subscription, err := deliveryMQ.Subscribe(ctx)
		defer subscription.Shutdown(ctx)
		if err != nil {
			errchan <- err
			return
		}
		readychan <- struct{}{}
		close(readychan)

		log.Println("receiving...")
		for {
			msg, err := subscription.Receive(ctx)
			if err != nil {
				if err == context.DeadlineExceeded {
					errchan <- nil
					return
				} else {
					errchan <- err
					continue
				}
			}
			messages = append(messages, msg)
		}
	}()

	// Publish to publishmq
	<-readychan
	log.Println("publishing...")
	event := models.Event{
		ID:               uuid.New().String(),
		TenantID:         uuid.New().String(),
		DestinationID:    uuid.New().String(),
		Topic:            "test",
		EligibleForRetry: true,
		Time:             time.Now(),
		Metadata:         map[string]string{},
		Data: map[string]interface{}{
			"mykey": "myvalue",
		},
	}
	// Publish events twice to test idempotency
	err = publishMQ.Publish(ctx, &event)
	require.Nil(t, err, "should publish event without error")
	err = publishMQ.Publish(ctx, &event)
	require.Nil(t, err, "should publish event without error")

	// ===== Assert =====
	<-ctx.Done()

	err = <-errchan
	require.Nil(t, err)
	require.Greater(t, len(messages), 0, "should receive at least one message")
	msg := messages[0]
	receivedEvent := models.Event{}
	err = receivedEvent.FromMessage(msg)
	require.Nil(t, err, "unable to parse event from message")
	assert.Equal(t, event.ID, receivedEvent.ID)
	// idempotency check
	assert.Equal(t, 1, len(messages))
}
