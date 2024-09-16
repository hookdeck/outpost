package ingest_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationIngester_InMemory(t *testing.T) {
	t.Parallel()

	testIngestor(t, func() ingest.IngestConfig {
		return ingest.IngestConfig{InMemory: &ingest.InMemoryConfig{Name: testutil.RandomString(5)}}
	})
}

func TestIntegrationIngester_RabbitMQ(t *testing.T) {
	t.Parallel()

	rabbitmqURL, terminate, err := testutil.StartTestcontainerRabbitMQ()
	require.Nil(t, err)
	defer terminate()

	config := ingest.IngestConfig{RabbitMQ: &ingest.RabbitMQConfig{
		ServerURL:       rabbitmqURL,
		PublishExchange: "eventkit",
		PublishQueue:    "eventkit.publish",
	}}

	testIngestor(t, func() ingest.IngestConfig { return config })
}

func testIngestor(t *testing.T, makeConfig func() ingest.IngestConfig) {
	t.Run("should initialize without error", func(t *testing.T) {
		config := makeConfig()
		ingestor, err := ingest.New(&config)
		assert.Nil(t, err)
		cleanup, err := ingestor.Init(context.Background())
		assert.Nil(t, err)
		subscription, err := ingestor.Subscribe(context.Background())
		assert.Nil(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		msg, err := subscription.Receive(ctx)
		assert.Nil(t, msg)
		assert.Equal(t, err, context.DeadlineExceeded)
		defer cleanup()
	})

	t.Run("should publish and receive message", func(t *testing.T) {
		config := makeConfig()
		ingestor, err := ingest.New(&config)
		cleanup, _ := ingestor.Init(context.Background())
		defer cleanup()

		msgchan := make(chan *ingest.Message)
		subscription, err := ingestor.Subscribe(context.Background())
		require.Nil(t, err)

		go func() {
			msg, err := subscription.Receive(context.Background())
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
		err = ingestor.Publish(context.Background(), event)
		require.Nil(t, err)

		receivedMsg := <-msgchan
		defer func() {
			receivedMsg.Ack()
			time.Sleep(time.Second / 2) // wait for ack to be processed
		}()
		require.NotNil(t, receivedMsg)
		assert.Equal(t, event.ID, receivedMsg.Event.ID)
	})
}
