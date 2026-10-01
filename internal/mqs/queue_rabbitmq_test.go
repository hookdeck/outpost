package mqs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestMQ_RabbitMQRedialCooldown(t *testing.T) {
	t.Parallel()
	queue := mqs.NewRabbitMQQueue(&mqs.RabbitMQConfig{
		ServerURL: "amqp://guest:guest@127.0.0.1:1/",
		Queue:     "test",
	})
	ctx := context.Background()

	// The broker is unreachable, so the first publish dials and fails.
	firstErr := queue.Publish(ctx, &Msg{ID: "first"})
	require.Error(t, firstErr)

	// Within the cooldown the cached dial error is returned without redialing.
	require.ErrorIs(t, queue.Publish(ctx, &Msg{ID: "second"}), firstErr)
}

func TestIntegrationMQ_RabbitMQPublishReconnects(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQRabbitMQConfig(t)

	ctx := context.Background()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	receive := func(t *testing.T) *Msg {
		t.Helper()
		subscription, err := queue.Subscribe(ctx)
		require.NoError(t, err)
		defer subscription.Shutdown(ctx)
		receiveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		msg, err := subscription.Receive(receiveCtx)
		require.NoError(t, err)
		msg.Ack()
		parsed := &Msg{}
		require.NoError(t, parsed.FromMessage(msg))
		return parsed
	}

	// Sanity check: publish and receive over the initial connection.
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "before-disconnect"}))
	require.Equal(t, "before-disconnect", receive(t).ID)

	// Simulate the broker dropping the connection.
	require.NoError(t, mqs.ForceCloseRabbitMQConnection(queue))

	// The publish must transparently redial instead of failing forever.
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "after-disconnect"}))
	require.Equal(t, "after-disconnect", receive(t).ID)
}

func TestIntegrationMQ_RabbitMQSubscriptionReconnects(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQRabbitMQConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	subscription, err := queue.Subscribe(ctx)
	require.NoError(t, err)

	received := make(chan string, 10)
	csm := consumer.New(subscription, handlerFunc(func(ctx context.Context, msg *mqs.Message) error {
		msg.Ack()
		parsed := &Msg{}
		if err := parsed.FromMessage(msg); err != nil {
			return err
		}
		received <- parsed.ID
		return nil
	}),
		consumer.WithConcurrency(5),
		consumer.WithMaxConsecutiveErrors(3),
		consumer.WithInitialBackoff(50*time.Millisecond),
	)
	runErr := make(chan error, 1)
	go func() { runErr <- csm.Run(ctx) }()

	// Messages whose ack was lost with the connection are redelivered, so
	// skip anything that isn't the expected ID.
	awaitMessage := func(id string) {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for {
			select {
			case got := <-received:
				if got == id {
					return
				}
			case err := <-runErr:
				t.Fatalf("consumer exited: %v", err)
			case <-timeout:
				t.Fatalf("timed out waiting for %s", id)
			}
		}
	}

	require.NoError(t, queue.Publish(ctx, &Msg{ID: "before-disconnect"}))
	awaitMessage("before-disconnect")

	// Simulate the broker dropping the connection.
	require.NoError(t, mqs.ForceCloseRabbitMQConnection(queue))

	require.NoError(t, queue.Publish(ctx, &Msg{ID: "after-disconnect"}))
	awaitMessage("after-disconnect")

	cancel()
	require.ErrorIs(t, <-runErr, context.Canceled)
}

type handlerFunc func(ctx context.Context, msg *mqs.Message) error

func (f handlerFunc) Handle(ctx context.Context, msg *mqs.Message) error { return f(ctx, msg) }

func TestIntegrationMQ_RabbitMQNoRedialAfterCleanup(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQRabbitMQConfig(t)

	ctx := context.Background()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)

	subscription, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer subscription.Shutdown(ctx)

	cleanup()

	receiveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = subscription.Receive(receiveCtx)
	require.Error(t, err)
	require.True(t, mqs.RabbitMQConnectionClosed(queue), "queue redialed after cleanup")
}

func TestIntegrationMQ_RabbitMQReject(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	config := &mqs.RabbitMQConfig{
		ServerURL: testinfra.EnsureRabbitMQ(),
		Exchange:  uuid.New().String(),
		Queue:     uuid.New().String(),
	}
	dlx := config.Exchange + "-dlx"
	dlq := config.Queue + "-dlq"

	conn, err := amqp091.Dial(config.ServerURL)
	require.NoError(t, err)
	defer conn.Close()
	ch, err := conn.Channel()
	require.NoError(t, err)
	defer ch.Close()
	require.NoError(t, ch.ExchangeDeclare(config.Exchange, "topic", false, true, false, false, nil))
	require.NoError(t, ch.ExchangeDeclare(dlx, "fanout", false, true, false, false, nil))
	_, err = ch.QueueDeclare(dlq, false, true, false, false, nil)
	require.NoError(t, err)
	require.NoError(t, ch.QueueBind(dlq, "", dlx, false, nil))
	_, err = ch.QueueDeclare(config.Queue, false, true, false, false, amqp091.Table{
		"x-dead-letter-exchange": dlx,
	})
	require.NoError(t, err)
	require.NoError(t, ch.QueueBind(config.Queue, config.Queue, config.Exchange, false, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := mqs.NewQueue(&mqs.QueueConfig{RabbitMQ: config})
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()
	subscription, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer subscription.Shutdown(ctx)

	receive := func() *Msg {
		t.Helper()
		rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
		defer rcancel()
		msg, err := subscription.Receive(rctx)
		require.NoError(t, err)
		msg.Ack()
		parsed := &Msg{}
		require.NoError(t, parsed.FromMessage(msg))
		return parsed
	}

	require.NoError(t, queue.Publish(ctx, &Msg{ID: "rejected"}))
	rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
	defer rcancel()
	msg, err := subscription.Receive(rctx)
	require.NoError(t, err)
	msg.Reject()

	// The rejected message is dead-lettered instead of redelivered.
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "next"}))
	require.Equal(t, "next", receive().ID)

	require.Eventually(t, func() bool {
		d, ok, err := ch.Get(dlq, true)
		require.NoError(t, err)
		if !ok {
			return false
		}
		parsed := &Msg{}
		require.NoError(t, json.Unmarshal(d.Body, parsed))
		return parsed.ID == "rejected"
	}, 5*time.Second, 100*time.Millisecond)
}
