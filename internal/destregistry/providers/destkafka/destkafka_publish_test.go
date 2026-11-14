package destkafka_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

// KafkaConsumer implements testsuite.MessageConsumer
type KafkaConsumer struct {
	client       *kgo.Client
	msgChan      chan testsuite.Message
	done         chan struct{}
	shuttingDown atomic.Bool
	wg           sync.WaitGroup
}

func NewKafkaConsumer(brokerAddr, topic string) (*KafkaConsumer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokerAddr),
		kgo.SASL(plain.Auth{User: "admin", Pass: "admin-secret"}.AsMechanism()),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.FetchMaxWait(500*time.Millisecond),
	)
	if err != nil {
		return nil, err
	}

	c := &KafkaConsumer{
		client:  client,
		msgChan: make(chan testsuite.Message, 100),
		done:    make(chan struct{}),
	}
	c.wg.Add(1)
	go c.consume()
	return c, nil
}

func (c *KafkaConsumer) consume() {
	defer c.wg.Done()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		fetches := c.client.PollFetches(ctx)
		cancel()

		fetches.EachRecord(func(rec *kgo.Record) {
			// Extract metadata from headers
			metadata := make(map[string]string)
			for _, h := range rec.Headers {
				metadata[h.Key] = string(h.Value)
			}

			if !c.shuttingDown.Load() {
				c.msgChan <- testsuite.Message{
					Data:     rec.Value,
					Metadata: metadata,
					Raw:      rec,
				}
			}
		})
	}
}

func (c *KafkaConsumer) Consume() <-chan testsuite.Message {
	return c.msgChan
}

func (c *KafkaConsumer) Close() error {
	c.shuttingDown.Store(true)
	close(c.done)
	c.wg.Wait()
	close(c.msgChan)
	c.client.Close()
	return nil
}

// KafkaAsserter implements testsuite.MessageAsserter
type KafkaAsserter struct{}

func (a *KafkaAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	kafkaMsg, ok := msg.Raw.(*kgo.Record)
	assert.True(t, ok, "raw message should be *kgo.Record")

	// Verify content-type header
	assert.Equal(t, "application/json", msg.Metadata["content-type"])

	// Verify message key is set (event ID by default)
	assert.Equal(t, event.ID, string(kafkaMsg.Key), "message key should be event ID")

	// Verify system metadata in headers
	metadata := msg.Metadata
	assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present")
	testsuite.AssertTimestampIsISO8601(t, metadata["timestamp"])
	assert.Equal(t, event.ID, metadata["event-id"], "event-id should match")
	assert.Equal(t, event.Topic, metadata["topic"], "topic should match")

	// Verify custom metadata
	for k, v := range event.Metadata {
		assert.Equal(t, v, metadata[k], "metadata key %s should match expected value", k)
	}
}

// KafkaPublishSuite runs the shared publisher test suite for Kafka
type KafkaPublishSuite struct {
	testsuite.PublisherSuite
	consumer *KafkaConsumer
	// proxied publishes through a two-hop CONNECT proxy chain.
	proxied bool
}

func (s *KafkaPublishSuite) SetupSuite() {
	t := s.T()
	testutil.SkipUnlessDest(t)
	t.Cleanup(testinfra.Start(t))

	brokerAddr := testinfra.EnsureKafka(t)
	topic := "test-topic-" + idgen.String()

	// Ensure topic exists by creating it
	ensureKafkaTopic(t, brokerAddr, topic)

	var opts []destkafka.Option
	if s.proxied {
		hops, err := proxychain.Parse(proxychaintest.New(t, false).URL + " " + proxychaintest.New(t, false).URL)
		require.NoError(t, err)
		opts = append(opts, destkafka.WithProxy(hops))
	}
	provider, err := destkafka.New(testutil.Registry.MetadataLoader(), nil, opts...)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("kafka"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"brokers":        brokerAddr,
			"topic":          topic,
			"sasl_mechanism": "plain",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "admin",
			"password": "admin-secret",
		}),
	)

	consumer, err := NewKafkaConsumer(brokerAddr, topic)
	require.NoError(t, err)
	s.consumer = consumer

	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &KafkaAsserter{},
	})
}

func (s *KafkaPublishSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
}

func TestKafkaPublishIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(KafkaPublishSuite))
}

func TestKafkaPublishThroughProxyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, &KafkaPublishSuite{proxied: true})
}

// TestKafkaPublisher_ConnectionErrors tests that connection errors return a Delivery object alongside the error.
func TestKafkaPublisher_ConnectionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		brokers      string
		description  string
		expectedCode string
	}{
		{
			name:         "connection refused",
			brokers:      "127.0.0.1:1",
			description:  "simulates a server that is not running",
			expectedCode: "connection_refused",
		},
		{
			name:         "DNS failure",
			brokers:      "this-domain-does-not-exist-abc123xyz.invalid:9092",
			description:  "simulates an invalid/non-existent domain",
			expectedCode: "dns_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
			require.NoError(t, err)

			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("kafka"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"brokers":        tt.brokers,
					"topic":          "test-topic",
					"sasl_mechanism": "plain",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"username": "user",
					"password": "pass",
				}),
			)

			publisher, err := provider.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)
			defer publisher.Close()

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
			)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			delivery, err := publisher.Publish(ctx, &event)

			require.Error(t, err, "should return error for %s", tt.description)
			require.NotNil(t, delivery, "delivery should NOT be nil for connection errors")
			assert.Equal(t, "failed", delivery.Status, "delivery status should be 'failed'")
			assert.Equal(t, tt.expectedCode, delivery.Code, "delivery code should indicate error type")
		})
	}
}

// TestKafkaPublisher_TopicNotFoundIntegration tests that publishing to a missing topic fails within the delivery timeout.
func TestKafkaPublisher_TopicNotFoundIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	testutil.SkipUnlessDest(t)
	t.Cleanup(testinfra.Start(t))
	brokerAddr := testinfra.EnsureKafka(t)

	provider, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("kafka"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"brokers":        brokerAddr,
			"topic":          "test-topic-missing-" + idgen.String(),
			"sasl_mechanism": "plain",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "admin",
			"password": "admin-secret",
		}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	delivery, err := publisher.Publish(ctx, &event)
	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "failed", delivery.Status)
	assert.Equal(t, "topic_not_found", delivery.Code)
}

// Helper to ensure a Kafka topic exists
func ensureKafkaTopic(t *testing.T, brokerAddr, topic string) {
	t.Helper()

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokerAddr),
		kgo.SASL(plain.Auth{User: "admin", Pass: "admin-secret"}.AsMechanism()),
	)
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = kadm.NewClient(client).CreateTopic(ctx, 1, 1, nil, topic)
	if !errors.Is(err, kerr.TopicAlreadyExists) {
		require.NoError(t, err)
	}

	// Give Kafka a moment to fully create the topic
	time.Sleep(500 * time.Millisecond)
}
