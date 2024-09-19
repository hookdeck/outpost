package ingest_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationIngester_InMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	testIngestor(t, func() mqs.QueueConfig {
		config := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
		return config
	})
}

func TestIntegrationIngester_RabbitMQ(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	rabbitmqURL, terminate, err := testutil.StartTestcontainerRabbitMQ()
	require.Nil(t, err)
	defer terminate()

	config := mqs.QueueConfig{RabbitMQ: &mqs.RabbitMQConfig{
		ServerURL: rabbitmqURL,
		Exchange:  "eventkit",
		Queue:     "eventkit.delivery",
	}}
	testIngestor(t, func() mqs.QueueConfig { return config })
}

func TestIntegrationIngestor_AWS(t *testing.T) {
	t.Parallel()

	awsEndpoint, terminate, err := testutil.StartTestcontainerLocalstack()
	require.Nil(t, err)
	defer terminate()

	config := mqs.QueueConfig{AWSSQS: &mqs.AWSSQSConfig{
		Endpoint:                  awsEndpoint,
		Region:                    "eu-central-1",
		ServiceAccountCredentials: "test:test:",
		Topic:                     "eventkit",
	}}
	testIngestor(t, func() mqs.QueueConfig { return config })
}

func testIngestor(t *testing.T, makeConfig func() mqs.QueueConfig) {
	t.Run("should initialize without error", func(t *testing.T) {
		config := makeConfig()
		ingestor := ingest.New(ingest.WithQueue(&config))
		cleanup, err := ingestor.Init(context.Background())
		require.Nil(t, err)
		subscription, err := ingestor.Subscribe(context.Background())
		require.Nil(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		msg, err := subscription.Receive(ctx)
		assert.Nil(t, msg)
		assert.Equal(t, err, context.DeadlineExceeded)
		defer cleanup()
	})

	t.Run("should publish and receive message", func(t *testing.T) {
		ctx := context.Background()
		config := makeConfig()
		ingestor := ingest.New(ingest.WithQueue(&config))
		cleanup, err := ingestor.Init(ctx)
		require.Nil(t, err)
		defer cleanup()

		msgchan := make(chan *mqs.Message)
		subscription, err := ingestor.Subscribe(ctx)
		require.Nil(t, err)
		defer subscription.Shutdown(ctx)

		go func() {
			msg, err := subscription.Receive(ctx)
			if err != nil {
				log.Println("subscription error", err)
			}
			msgchan <- msg
		}()

		event := ingest.Event{
			ID:            "123",
			TenantID:      "456",
			DestinationID: "789",
			Topic:         "test",
			Time:          time.Now(),
			Metadata:      map[string]string{"key": "value"},
			Data:          map[string]interface{}{"key": "value"},
		}
		err = ingestor.Publish(ctx, event)
		require.Nil(t, err)

		receivedMsg := <-msgchan
		require.NotNil(t, receivedMsg)
		receivedEvent := ingest.Event{}
		err = receivedEvent.FromMessage(receivedMsg)
		assert.Nil(t, err)
		assert.Equal(t, event.ID, receivedEvent.ID)

		receivedMsg.Ack()
	})
}
