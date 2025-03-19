package destrabbitmq_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockChannel mocks the Channel interface for testing
type MockChannel struct {
	mock.Mock
}

func (m *MockChannel) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp091.Publishing) error {
	args := m.Called(ctx, exchange, key, mandatory, immediate, msg)
	return args.Error(0)
}

// Make sure MockChannel implements the needed methods
func (m *MockChannel) Close() error {
	return nil // Simple implementation for testing
}

func (m *MockChannel) IsClosed() bool {
	return false // Simple implementation for testing
}

// MockConnection mocks the Connection interface for testing
type MockConnection struct {
	mock.Mock
}

func (m *MockConnection) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConnection) IsClosed() bool {
	args := m.Called()
	return args.Bool(0)
}

// Ensure MockConnection implements destrabbitmq.AMQPConnection
var _ destrabbitmq.AMQPConnection = (*MockConnection)(nil)

func TestRabbitMQPublisher_Publish(t *testing.T) {

	t.Run("should use topic as routing_key", func(t *testing.T) {

		// Set up specific test topic and data
		topic := "test.topic"
		eventData := map[string]interface{}{"key": "value"}

		// Set up RabbitMQ configuration
		mqConfig := testinfra.NewMQRabbitMQConfig(t)
		exchangeName := "test-exchange"

		// Create destination with the test topic
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("rabbitmq"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"server_url": testutil.ExtractRabbitURL(mqConfig.RabbitMQ.ServerURL),
				"exchange":   exchangeName,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"username": testutil.ExtractRabbitUsername(mqConfig.RabbitMQ.ServerURL),
				"password": testutil.ExtractRabbitPassword(mqConfig.RabbitMQ.ServerURL),
			}),
			testutil.DestinationFactory.WithTopics([]string{topic}),
		)

		// Create a mock channel to verify the call parameters
		mockChannel := new(MockChannel)

		// Create a mock connection
		mockConnection := new(MockConnection)
		mockConnection.On("IsClosed").Return(false)

		// Set up expectations for the mock
		mockChannel.On("PublishWithContext",
			mock.Anything, // context
			mock.Anything, // exchange name
			topic,         // routing key (event topic)
			mock.Anything, // mandatory
			mock.Anything, // immediate
			mock.Anything, // message
		).Return(nil)

		// Create the RabbitMQ provider
		provider, err := destrabbitmq.New(testutil.Registry.MetadataLoader())
		assert.NoError(t, err)

		// Create the publisher
		pub, err := provider.CreatePublisher(context.Background(), &dest)
		assert.NoError(t, err)

		// Cast to RabbitMQPublisher to access internal fields
		rabbitMQPub, ok := pub.(*destrabbitmq.RabbitMQPublisher)
		assert.True(t, ok, "publisher should be a RabbitMQPublisher")

		// Set up both the connection and channel for testing
		rabbitMQPub.SetupForTesting(mockConnection, mockChannel)

		// Create and publish the event
		event := &models.Event{
			Topic: topic,
			Data:  eventData,
		}

		ctx := context.Background()
		delivery, err := pub.Publish(ctx, event)

		// Verify the result
		assert.NoError(t, err)
		assert.Equal(t, "success", delivery.Status)

		// Verify the mock was called with expected parameters
		mockChannel.AssertCalled(t, "PublishWithContext",
			mock.Anything, // We don't need to check the exact context
			exchangeName,
			topic, // Important: verify the topic is used as routing key
			false,
			false,
			mock.Anything,
		)

		// Verify that all expectations were met
		mockChannel.AssertExpectations(t)
		mockConnection.AssertExpectations(t)
	})
}
