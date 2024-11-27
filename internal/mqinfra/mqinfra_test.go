package mqinfra_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationMQInfra_RabbitMQ(t *testing.T) {
	t.Cleanup(testinfra.Start(t))

	mqConfig := mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: testinfra.EnsureRabbitMQ(),
			Exchange:  uuid.New().String(),
			Queue:     uuid.New().String(),
		},
	}

	ctx := context.Background()
	require.NoError(t, mqinfra.DeclareMQ(ctx, mqConfig))

	t.Cleanup(func() {
		require.NoError(t, mqinfra.TeardownMQ(ctx, mqConfig))
	})

	mq := mqs.NewQueue(&mqConfig)
	cleanup, err := mq.Init(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	subscription, err := mq.Subscribe(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		subscription.Shutdown(ctx)
	})
	msgchan := make(chan *testutil.MockMsg)
	go func() {
		for {
			msg, err := subscription.Receive(ctx)
			if err != nil {
				log.Println(err)
				return
			}
			log.Println("Received message", msg)
			msg.Ack()
			mockMsg := &testutil.MockMsg{}
			if err := mockMsg.FromMessage(msg); err != nil {
				log.Println("Error parsing message", err)
			} else {
				msgchan <- mockMsg
			}
		}
	}()

	msg := &testutil.MockMsg{ID: uuid.New().String()}
	require.NoError(t, mq.Publish(ctx, msg))

	var receivedMsg *testutil.MockMsg
	select {
	case receivedMsg = <-msgchan:
	case <-time.After(1 * time.Second):
		require.Fail(t, "timeout waiting for message")
	}

	assert.Equal(t, msg.ID, receivedMsg.ID)
}
