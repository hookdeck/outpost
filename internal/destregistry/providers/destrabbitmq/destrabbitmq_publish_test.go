package destrabbitmq_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestIntegrationRabbitMQDestination_Publish(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	// Get RabbitMQ config from testinfra
	mqConfig := testinfra.NewMQRabbitMQConfig(t)

	// Create RabbitMQ provider
	provider, err := destrabbitmq.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	// Create test destination
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": testutil.ExtractRabbitURL(mqConfig.RabbitMQ.ServerURL),
			"exchange":   mqConfig.RabbitMQ.Exchange,
			"queue":      mqConfig.RabbitMQ.Queue,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": testutil.ExtractRabbitUsername(mqConfig.RabbitMQ.ServerURL),
			"password": testutil.ExtractRabbitPassword(mqConfig.RabbitMQ.ServerURL),
		}),
	)

	// Create publisher
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	// Create message channel for verification
	deliveries := make(chan amqp091.Delivery)
	cleanup, err := setupRabbitMQConsumer(context.Background(), mqConfig, deliveries)
	require.NoError(t, err)
	defer cleanup()

	t.Run("should publish message", func(t *testing.T) {
		// Create test event
		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{
				"test_key": "test_value",
			}),
			testutil.EventFactory.WithMetadata(map[string]string{
				"meta_key": "meta_value",
			}),
		)

		// Publish event
		err = publisher.Publish(context.Background(), &event)
		require.NoError(t, err)

		// Verify received message
		select {
		case delivery := <-deliveries:
			// Verify message body
			var body map[string]interface{}
			err = json.Unmarshal(delivery.Body, &body)
			require.NoError(t, err)
			require.Equal(t, "test_value", body["test_key"])
			require.Equal(t, "meta_value", delivery.Headers["meta_key"])

		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run("should publish concurrently", func(t *testing.T) {
		// Publish multiple messages concurrently
		numMessages := 10
		var wg sync.WaitGroup
		errChan := make(chan error, numMessages)

		for i := 0; i < numMessages; i++ {
			wg.Add(1)
			go func(messageID int) {
				defer wg.Done()
				event := testutil.EventFactory.Any(
					testutil.EventFactory.WithData(map[string]interface{}{
						"message_id": messageID,
					}),
				)
				if err := publisher.Publish(context.Background(), &event); err != nil {
					errChan <- err
				}
			}(i)
		}

		// Wait for all publishes to complete
		wg.Wait()
		close(errChan)

		// Check for any publish errors
		for err := range errChan {
			require.NoError(t, err)
		}

		// Verify all messages were received
		receivedMessages := make(map[int]bool)
		timeout := time.After(5 * time.Second)

		for i := 0; i < numMessages; i++ {
			select {
			case delivery := <-deliveries:
				var body map[string]interface{}
				err := json.Unmarshal(delivery.Body, &body)
				require.NoError(t, err)
				messageID := int(body["message_id"].(float64))
				receivedMessages[messageID] = true
			case <-timeout:
				t.Fatal("timeout waiting for messages")
			}
		}

		// Verify all messages were received
		require.Len(t, receivedMessages, numMessages)
	})
}

func setupRabbitMQConsumer(ctx context.Context, config mqs.QueueConfig, deliveries chan<- amqp091.Delivery) (func(), error) {
	conn, err := amqp091.Dial(config.RabbitMQ.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Ensure queue exists
	_, err = ch.QueueDeclare(
		config.RabbitMQ.Queue, // name
		true,                  // durable
		false,                 // delete when unused
		false,                 // exclusive
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	// Start consuming
	msgs, err := ch.Consume(
		config.RabbitMQ.Queue, // queue
		"",                    // consumer
		true,                  // auto-ack
		false,                 // exclusive
		false,                 // no-local
		false,                 // no-wait
		nil,                   // args
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to start consuming: %w", err)
	}

	// Start goroutine to forward messages
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-msgs:
				if !ok {
					return
				}
				deliveries <- d
			}
		}
	}()

	// Return cleanup function
	cleanup := func() {
		ch.Close()
		conn.Close()
	}

	return cleanup, nil
}
