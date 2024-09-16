package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: Each tests in this suite cannot be run in parallel because it will close the other's in-memory topic
func TestIngester_InMemory(t *testing.T) {
	t.Parallel()

	t.Run("should initialize without error", func(t *testing.T) {
		ingestor, err := ingest.New(&ingest.IngestConfig{InMemory: &ingest.InMemoryConfig{Name: testutil.RandomString(5)}})
		assert.Nil(t, err)
		cleanup, err := ingestor.Init(context.Background())
		assert.Nil(t, err)
		defer cleanup()
	})

	t.Run("should publish and receive message", func(t *testing.T) {
		ingestor, err := ingest.New(&ingest.IngestConfig{InMemory: &ingest.InMemoryConfig{Name: testutil.RandomString(5)}})
		cleanup, _ := ingestor.Init(context.Background())
		defer cleanup()

		msgchan := make(chan ingest.Message)
		subscription, err := ingestor.Subscribe(context.Background())
		require.Nil(t, err)

		go func() {
			msg, _ := subscription.Receive(context.Background())
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
		assert.Equal(t, event.ID, receivedMsg.Event.ID)
	})
}
