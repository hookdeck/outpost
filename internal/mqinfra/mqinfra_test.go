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

const retryLimit = 3

func testMQInfra(t *testing.T, mqConfig mqs.QueueConfig, dlqConfig mqs.QueueConfig) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	ctx := context.Background()
	infra := mqinfra.New(&mqConfig)
	require.NoError(t, infra.Declare(ctx))

	t.Cleanup(func() {
		require.NoError(t, infra.TearDown(ctx))
	})

	t.Run("should create queue", func(t *testing.T) {
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
	})

	// Assertion:
	// - When the message is nacked, it should be retried 5 times before being sent to the DLQ.
	// - Afterwards, reading the DLQ should return the message.
	t.Run("should create dlq queue", func(t *testing.T) {
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
				msg.Nack()
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

		msgCount := 0
	loop:
		for {
			select {
			case <-msgchan:
				msgCount++
			case <-time.After(3 * time.Second):
				break loop
			}
		}
		require.Equal(t, retryLimit+1, msgCount)

		dlmq := mqs.NewQueue(&dlqConfig)
		dlsubscription, err := dlmq.Subscribe(ctx)
		require.NoError(t, err)
		dlmsgchan := make(chan *testutil.MockMsg)
		go func() {
			for {
				msg, err := dlsubscription.Receive(ctx)
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
					dlmsgchan <- mockMsg
				}
			}
		}()
		var dlmsg *testutil.MockMsg
		dlmsgCount := 0
	dlloop:
		for {
			select {
			case dlmsg = <-dlmsgchan:
				dlmsgCount++
			case <-time.After(1 * time.Second):
				break dlloop
			}
		}
		assert.Equal(t, 1, dlmsgCount)
		assert.Equal(t, msg.ID, dlmsg.ID)
	})
}

func TestIntegrationMQInfra_RabbitMQ(t *testing.T) {
	exchange := uuid.New().String()
	queue := uuid.New().String()

	testMQInfra(t,
		mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: testinfra.EnsureRabbitMQ(),
				Exchange:  exchange,
				Queue:     queue,
			},
			Policy: mqs.Policy{
				RetryLimit: retryLimit,
			},
		},
		mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: testinfra.EnsureRabbitMQ(),
				Exchange:  exchange + ".dlx",
				Queue:     queue + ".dlq",
			},
		},
	)
}

func TestIntegrationMQInfra_AWSSQS(t *testing.T) {
	q := uuid.New().String()

	testMQInfra(t,
		mqs.QueueConfig{
			AWSSQS: &mqs.AWSSQSConfig{
				Endpoint:                  testinfra.EnsureLocalStack(),
				ServiceAccountCredentials: "test:test:",
				Region:                    "us-east-1",
				Topic:                     q,
			},
			Policy: mqs.Policy{
				RetryLimit:        retryLimit,
				VisibilityTimeout: 1,
			},
		},
		mqs.QueueConfig{
			AWSSQS: &mqs.AWSSQSConfig{
				Endpoint:                  testinfra.EnsureLocalStack(),
				ServiceAccountCredentials: "test:test:",
				Region:                    "us-east-1",
				Topic:                     q + "-dlq",
			},
		},
	)
}
