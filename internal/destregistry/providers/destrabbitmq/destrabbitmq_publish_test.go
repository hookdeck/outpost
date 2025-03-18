package destrabbitmq_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
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

func NewRabbitMQConsumer(config mqs.QueueConfig) (*RabbitMQConsumer, error) {
	consumer := &RabbitMQConsumer{
		messages: make(chan testsuite.Message, 100),
	}

	// Connect to RabbitMQ
	conn, err := amqp091.Dial(config.RabbitMQ.ServerURL)
	if err != nil {
		return nil, err
	}

	// Create channel
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Ensure queue exists
	_, err = ch.QueueDeclare(
		config.RabbitMQ.Queue,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	// Start consuming
	deliveries, err := ch.Consume(
		config.RabbitMQ.Queue,
		"",    // consumer
		true,  // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
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
				Raw:      d, // Include the raw amqp.Delivery
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
	// assert.NotEmpty(t, delivery.MessageId)
	// assert.NotEmpty(t, delivery.Timestamp)

	// Could add more RabbitMQ-specific assertions:
	// - Exchange routing
	// - Message persistence
	// - Priority
	// - etc.
}

// RabbitMQPublishSuite reimplements the publish tests using the shared test suite
type RabbitMQPublishSuite struct {
	testsuite.PublisherSuite
	consumer *RabbitMQConsumer
}

func (s *RabbitMQPublishSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))
	mqConfig := testinfra.NewMQRabbitMQConfig(t)

	provider, err := destrabbitmq.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url":  testutil.ExtractRabbitURL(mqConfig.RabbitMQ.ServerURL),
			"exchange":    mqConfig.RabbitMQ.Exchange,
			"routing_key": mqConfig.RabbitMQ.Queue,
			// "tls":         "false", // should default to false if omitted
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": testutil.ExtractRabbitUsername(mqConfig.RabbitMQ.ServerURL),
			"password": testutil.ExtractRabbitPassword(mqConfig.RabbitMQ.ServerURL),
		}),
	)

	consumer, err := NewRabbitMQConsumer(mqConfig)
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

func TestRabbitMQPublish(t *testing.T) {
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
