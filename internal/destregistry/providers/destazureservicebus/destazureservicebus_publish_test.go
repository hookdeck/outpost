package destazureservicebus_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destazureservicebus"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// AzureServiceBusConsumer implements testsuite.MessageConsumer
type AzureServiceBusConsumer struct {
	client   *azservicebus.Client
	receiver *azservicebus.Receiver
	msgChan  chan testsuite.Message
	done     chan struct{}
}

func NewAzureServiceBusConsumer(connectionString, topic, subscription string) (*AzureServiceBusConsumer, error) {
	client, err := azservicebus.NewClientFromConnectionString(connectionString, nil)
	if err != nil {
		return nil, err
	}

	receiver, err := client.NewReceiverForSubscription(topic, subscription, nil)
	if err != nil {
		return nil, err
	}

	c := &AzureServiceBusConsumer{
		client:   client,
		receiver: receiver,
		msgChan:  make(chan testsuite.Message),
		done:     make(chan struct{}),
	}
	go c.consume()
	return c, nil
}

func (c *AzureServiceBusConsumer) consume() {
	ctx := context.Background()
	for {
		select {
		case <-c.done:
			return
		default:
			messages, err := c.receiver.ReceiveMessages(ctx, 1, nil)
			if err != nil {
				continue
			}

			for _, msg := range messages {
				// Parse custom properties as metadata
				metadata := make(map[string]string)
				for k, v := range msg.ApplicationProperties {
					if strVal, ok := v.(string); ok {
						metadata[k] = strVal
					}
				}

				// Parse the message body
				var data interface{}
				if err := json.Unmarshal(msg.Body, &data); err != nil {
					// If JSON parsing fails, use raw bytes
					data = msg.Body
				}

				dataBytes, _ := json.Marshal(data)

				c.msgChan <- testsuite.Message{
					Data:     dataBytes,
					Metadata: metadata,
					Raw:      msg,
				}

				// Complete the message
				if err := c.receiver.CompleteMessage(ctx, msg, nil); err != nil {
					// Log error but continue
					continue
				}
			}
		}
	}
}

func (c *AzureServiceBusConsumer) Consume() <-chan testsuite.Message {
	return c.msgChan
}

func (c *AzureServiceBusConsumer) Close() error {
	close(c.done)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if c.receiver != nil {
		c.receiver.Close(ctx)
	}
	if c.client != nil {
		c.client.Close(ctx)
	}
	return nil
}

type AzureServiceBusAsserter struct{}

func (a *AzureServiceBusAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	// Phase 1: Basic assertion structure
	metadata := msg.Metadata

	// Verify system metadata
	assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present")
	testsuite.AssertTimestampIsUnixSeconds(t, metadata["timestamp"])
	assert.Equal(t, event.ID, metadata["event-id"], "event-id should match")
	assert.Equal(t, event.Topic, metadata["topic"], "topic should match")

	// Verify custom metadata
	for k, v := range event.Metadata {
		assert.Equal(t, v, metadata[k], "metadata key %s should match expected value", k)
	}
}

type AzureServiceBusSuite struct {
	testsuite.PublisherSuite
	consumer *AzureServiceBusConsumer
}

func TestDestinationAzureServiceBusSuite(t *testing.T) {
	suite.Run(t, new(AzureServiceBusSuite))
}

func (s *AzureServiceBusSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))
	mqConfig := testinfra.GetMQAzureConfig(t, "TestDestinationAzureServiceBusSuite")

	// Create consumer
	consumer, err := NewAzureServiceBusConsumer(
		mqConfig.AzureServiceBus.ConnectionString,
		mqConfig.AzureServiceBus.Topic,
		mqConfig.AzureServiceBus.Subscription,
	)
	require.NoError(t, err)
	s.consumer = consumer

	// Create provider
	provider, err := destazureservicebus.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	// Create destination
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("azure_servicebus"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"name": mqConfig.AzureServiceBus.Topic,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"connection_string": mqConfig.AzureServiceBus.ConnectionString,
		}),
	)

	// Initialize publisher suite with custom asserter
	cfg := testsuite.Config{
		Provider: provider,
		Dest:     &destination,
		Consumer: s.consumer,
		Asserter: &AzureServiceBusAsserter{},
	}
	s.InitSuite(cfg)
}

func (s *AzureServiceBusSuite) TearDownSuite() {
	if s.consumer != nil {
		_ = s.consumer.Close()
	}
}
