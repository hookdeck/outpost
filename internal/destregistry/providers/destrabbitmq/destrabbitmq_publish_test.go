package destrabbitmq_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// RabbitMQConsumer implements testsuite.MessageConsumer
type RabbitMQConsumer struct {
	conn     *amqp091.Connection
	channel  *amqp091.Channel
	messages chan testsuite.Message
}

func NewRabbitMQConsumer(serverURL, exchange string) (*RabbitMQConsumer, error) {
	consumer := &RabbitMQConsumer{
		messages: make(chan testsuite.Message, 100),
	}

	// Connect to RabbitMQ
	conn, err := amqp091.Dial(serverURL)
	if err != nil {
		return nil, err
	}

	// Create channel
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Ensure exchange exists
	err = ch.ExchangeDeclare(
		exchange, // name
		"topic",  // type
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	// Create a temporary queue
	queue, err := ch.QueueDeclare(
		"",    // name (empty = auto-generated name)
		false, // durable
		true,  // delete when unused
		true,  // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	// Bind queue to exchange with wildcard routing key
	err = ch.QueueBind(
		queue.Name, // queue name
		"#",        // routing key (# = match all)
		exchange,   // exchange
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	// Start consuming
	deliveries, err := ch.Consume(
		queue.Name, // queue
		"",         // consumer
		true,       // auto-ack
		false,      // exclusive
		false,      // no-local
		false,      // no-wait
		nil,        // args
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	consumer.conn = conn
	consumer.channel = ch

	// Forward messages with raw delivery
	go func() {
		for d := range deliveries {
			consumer.messages <- testsuite.Message{
				Data:     d.Body,
				Metadata: toStringMap(d.Headers),
				Raw:      d,
			}
		}
	}()

	return consumer, nil
}

func (c *RabbitMQConsumer) Consume() <-chan testsuite.Message {
	return c.messages
}

func (c *RabbitMQConsumer) Close() error {
	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	close(c.messages)
	return nil
}

// RabbitMQAsserter implements provider-specific message assertions
type RabbitMQAsserter struct{}

func (a *RabbitMQAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	delivery, ok := msg.Raw.(amqp091.Delivery)
	assert.True(t, ok, "raw message should be amqp.Delivery")

	// Assert RabbitMQ-specific properties
	assert.Equal(t, "application/json", delivery.ContentType)
	assert.Equal(t, event.Topic, delivery.RoutingKey, "routing key should match event topic")

	// Verify system metadata
	metadata := msg.Metadata
	assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present")
	testsuite.AssertTimestampIsUnixSeconds(t, metadata["timestamp"])
	assert.Equal(t, event.ID, metadata["event-id"], "event-id should match")
	assert.Equal(t, event.Topic, metadata["topic"], "topic should match")

	// Verify custom metadata
	for k, v := range event.Metadata {
		assert.Equal(t, v, metadata[k], "metadata key %s should match expected value", k)
	}
}

// RabbitMQPublishSuite reimplements the publish tests using the shared test suite
type RabbitMQPublishSuite struct {
	testsuite.PublisherSuite
	consumer *RabbitMQConsumer
}

func (s *RabbitMQPublishSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))
	rabbitURL := testinfra.EnsureRabbitMQ()
	exchange := idgen.String()

	provider, err := destrabbitmq.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": testutil.ExtractRabbitURL(rabbitURL),
			"exchange":   exchange,
			// "tls":         "false", // should default to false if omitted
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": testutil.ExtractRabbitUsername(rabbitURL),
			"password": testutil.ExtractRabbitPassword(rabbitURL),
		}),
	)

	consumer, err := NewRabbitMQConsumer(rabbitURL, exchange)
	require.NoError(t, err)
	s.consumer = consumer

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &RabbitMQAsserter{}, // Add RabbitMQ-specific assertions
	})
}

func (s *RabbitMQPublishSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
}

func TestRabbitMQPublishIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(RabbitMQPublishSuite))
}

// Helper functions

func toStringMap(table amqp091.Table) map[string]string {
	result := make(map[string]string)
	for k, v := range table {
		if str, ok := v.(string); ok {
			result[k] = str
		}
	}
	return result
}

// TestRabbitMQPublisher_ConnectionErrors tests that connection errors (connection refused, DNS failures)
// return a Delivery object alongside the error, NOT nil.
//
// This is important because the messagehandler uses the presence of a Delivery object to distinguish
// between "pre-delivery errors" (system issues) and "delivery errors" (destination issues):
// - nil delivery + error → PreDeliveryError → nack → DLQ
// - delivery + error → DeliveryError → ack + retry
//
// Connection errors are destination-level failures and should trigger retries, not go to DLQ.
// See: https://github.com/hookdeck/outpost/issues/571
func TestRabbitMQPublisher_ConnectionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		serverURL    string
		description  string
		expectedCode string
	}{
		{
			name:         "connection refused",
			serverURL:    "127.0.0.1:1", // Port 1 is typically not listening
			description:  "simulates a server that is not running",
			expectedCode: "connection_refused",
		},
		{
			name:         "DNS failure",
			serverURL:    "this-domain-does-not-exist-abc123xyz.invalid:5672",
			description:  "simulates an invalid/non-existent domain",
			expectedCode: "dns_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider, err := destrabbitmq.New(testutil.Registry.MetadataLoader(), nil)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("rabbitmq"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"server_url": tt.serverURL,
					"exchange":   "test-exchange",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"username": "guest",
					"password": "guest",
				}),
			)

			publisher, err := provider.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)
			defer publisher.Close()

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{"key": "value"}),
			)

			// Attempt to publish to unreachable endpoint
			delivery, err := publisher.Publish(context.Background(), &event)

			// Should return an error
			require.Error(t, err, "should return error for %s", tt.description)

			// CRITICAL: Should return a Delivery object, NOT nil
			// This ensures the error is treated as a DeliveryError (retryable)
			// rather than a PreDeliveryError (goes to DLQ)
			require.NotNil(t, delivery, "delivery should NOT be nil for connection errors - "+
				"returning nil causes messagehandler to treat this as PreDeliveryError (nack → DLQ) "+
				"instead of DeliveryError (ack + retry)")

			// Verify the delivery has appropriate status and code
			assert.Equal(t, "failed", delivery.Status, "delivery status should be 'failed'")
			assert.Equal(t, tt.expectedCode, delivery.Code, "delivery code should indicate error type")
		})
	}
}
