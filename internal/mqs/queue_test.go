package mqs_test

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationMQ_InMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	testMQ(t, func() mqs.QueueConfig {
		config := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
		return config
	})
}

func TestIntegrationMQ_RabbitMQ(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQRabbitMQConfig(t)
	testMQ(t, func() mqs.QueueConfig { return config })
}

func TestIntegrationMQ_AWS(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQAWSConfig(t, nil)
	testMQ(t, func() mqs.QueueConfig { return config })
}

type Msg struct {
	ID   string
	Data map[string]string
}

var _ mqs.IncomingMessage = &Msg{}

func (e *Msg) FromMessage(msg *mqs.Message) error {
	return json.Unmarshal(msg.Body, e)
}

func (e *Msg) ToMessage() (*mqs.Message, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return &mqs.Message{Body: data}, nil
}

func testMQ(t *testing.T, makeConfig func() mqs.QueueConfig) {
	t.Run("should initialize without error", func(t *testing.T) {
		config := makeConfig()
		queue := mqs.NewQueue(&config)
		cleanup, err := queue.Init(context.Background())
		require.Nil(t, err)
		defer cleanup()
		subscription, err := queue.Subscribe(context.Background())
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
		queue := mqs.NewQueue(&config)
		cleanup, err := queue.Init(context.Background())
		require.Nil(t, err)
		defer cleanup()

		msgchan := make(chan *mqs.Message)
		subscription, err := queue.Subscribe(ctx)
		require.Nil(t, err)
		defer subscription.Shutdown(ctx)

		go func() {
			msg, err := subscription.Receive(ctx)
			if err != nil {
				log.Println("subscription error", err)
			}
			msgchan <- msg
		}()

		msg := Msg{
			ID:   "123",
			Data: map[string]string{"mykey": "myvalue"},
		}
		err = queue.Publish(ctx, &msg)
		require.Nil(t, err)

		receivedMsg := <-msgchan
		require.NotNil(t, receivedMsg)
		parsedMsg := Msg{}
		err = parsedMsg.FromMessage(receivedMsg)
		assert.Nil(t, err)
		assert.Equal(t, msg.ID, parsedMsg.ID)

		receivedMsg.Ack()
	})
}
