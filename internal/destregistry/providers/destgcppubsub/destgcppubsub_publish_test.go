package destgcppubsub_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"cloud.google.com/go/pubsub"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destgcppubsub"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GCPPubSubConsumer implements testsuite.MessageConsumer
type GCPPubSubConsumer struct {
	client       *pubsub.Client
	subscription *pubsub.Subscription
	messages     chan testsuite.Message
	done         chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewGCPPubSubConsumer(projectID, subscriptionID, endpoint string) (*GCPPubSubConsumer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create client with emulator settings
	client, err := pubsub.NewClient(ctx, projectID,
		option.WithEndpoint(endpoint),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	subscription := client.Subscription(subscriptionID)

	consumer := &GCPPubSubConsumer{
		client:       client,
		subscription: subscription,
		messages:     make(chan testsuite.Message, 100),
		done:         make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start consuming messages
	go consumer.consume()

	return consumer, nil
}

func (c *GCPPubSubConsumer) consume() {
	err := c.subscription.Receive(c.ctx, func(ctx context.Context, msg *pubsub.Message) {
		// Convert attributes to metadata
		metadata := make(map[string]string)
		for k, v := range msg.Attributes {
			metadata[k] = v
		}

		// Send to channel
		select {
		case c.messages <- testsuite.Message{
			Data:     msg.Data,
			Metadata: metadata,
			Raw:      msg,
		}:
		case <-c.done:
			return
		}

		// Acknowledge the message
		msg.Ack()
	})

	if err != nil && err != context.Canceled {
		fmt.Printf("Error in GCP Pub/Sub consume loop: %v\n", err)
	}
}

func (c *GCPPubSubConsumer) Consume() <-chan testsuite.Message {
	return c.messages
}

func (c *GCPPubSubConsumer) Close() error {
	close(c.done)
	c.cancel()
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// GCPPubSubAsserter implements testsuite.MessageAsserter
type GCPPubSubAsserter struct{}

func (a *GCPPubSubAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	// Assert the raw message is a Pub/Sub message
	pubsubMsg, ok := msg.Raw.(*pubsub.Message)
	assert.True(t, ok, "raw message should be *pubsub.Message")

	// Verify event data
	expectedData, _ := json.Marshal(event.Data)
	assert.JSONEq(t, string(expectedData), string(msg.Data), "event data should match")

	// Verify system metadata
	metadata := msg.Metadata
	assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present")
	assert.Equal(t, event.ID, metadata["event-id"], "event-id should match")
	assert.Equal(t, event.Topic, metadata["topic"], "topic should match")

	// Verify custom metadata
	for k, v := range event.Metadata {
		assert.Equal(t, v, metadata[k], "metadata key %s should match expected value", k)
	}

	// Verify Pub/Sub specific attributes
	assert.NotNil(t, pubsubMsg.Attributes, "attributes should be set")
}

// GCPPubSubPublishSuite uses the shared test suite
type GCPPubSubPublishSuite struct {
	testsuite.PublisherSuite
	consumer *GCPPubSubConsumer
	config   mqs.QueueConfig
}

func (s *GCPPubSubPublishSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))

	// Set up GCP Pub/Sub test infrastructure
	mqConfig := testinfra.NewMQGCPConfig(t, nil)
	s.config = mqConfig

	// Get emulator endpoint
	endpoint := testinfra.EnsureGCP()

	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("gcp_pubsub"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"project_id": mqConfig.GCPPubSub.ProjectID,
			"topic":      mqConfig.GCPPubSub.TopicID,
			"endpoint":   endpoint, // For emulator
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"service_account_json": `{"type":"service_account","project_id":"test-project"}`, // Dummy JSON for emulator
		}),
	)

	// Create consumer
	consumer, err := NewGCPPubSubConsumer(
		mqConfig.GCPPubSub.ProjectID,
		mqConfig.GCPPubSub.SubscriptionID,
		endpoint,
	)
	require.NoError(t, err)
	s.consumer = consumer

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &GCPPubSubAsserter{},
	})
}

func (s *GCPPubSubPublishSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
}

func TestGCPPubSubPublishIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(GCPPubSubPublishSuite))
}
