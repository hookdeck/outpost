package api_test

import (
	"context"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/services/api"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationAPIService_PublishMQConsumer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	// ===== Arrange =====
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	publishQueueConfig := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
	deliveryQueueConfig := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}

	// Set up destinations
	redisConfig := testutil.CreateTestRedisConfig(t)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", redisConfig.Host, redisConfig.Port),
		Password: redisConfig.Password,
		DB:       redisConfig.Database,
	})
	entityStore := setupTestEntityStore(t, redisClient, nil)
	destination := models.Destination{
		ID:       uuid.New().String(),
		TenantID: uuid.New().String(),
		Type:     "webhook",
		Topics:   []string{"*"},
		Config: map[string]string{
			"url": "http://localhost:8080",
		},
		Credentials: map[string]string{},
		CreatedAt:   time.Now(),
		DisabledAt:  nil,
	}
	entityStore.UpsertDestination(ctx, destination)
	log.Println("destination", destination.TenantID)

	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	apiService, err := api.NewService(ctx, wg,
		&config.Config{
			Redis:                 redisConfig,
			PublishQueueConfig:    &publishQueueConfig,
			DeliveryQueueConfig:   &deliveryQueueConfig,
			PublishMaxConcurrency: 1,
			AESEncryptionSecret:   "secret",
			Topics:                testutil.TestTopics,
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
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(destination.TenantID),
	)
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
	receivedDeliveryEvent := models.DeliveryEvent{}
	err = receivedDeliveryEvent.FromMessage(msg)
	require.Nil(t, err, "unable to parse event from message")
	assert.Equal(t, event.ID, receivedDeliveryEvent.Event.ID)
	// idempotency check
	assert.Equal(t, 1, len(messages))
}
