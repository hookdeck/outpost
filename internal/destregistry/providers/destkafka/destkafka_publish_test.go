package destkafka_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// KafkaConsumer implements testsuite.MessageConsumer
type KafkaConsumer struct {
	reader       *kafka.Reader
	msgChan      chan testsuite.Message
	done         chan struct{}
	shuttingDown atomic.Bool
	wg           sync.WaitGroup
}

func NewKafkaConsumer(brokerAddr, topic string) (*KafkaConsumer, error) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{brokerAddr},
		Topic:       topic,
		StartOffset: kafka.FirstOffset,
		MaxWait:     500 * time.Millisecond,
	})

	c := &KafkaConsumer{
		reader:  reader,
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
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			msg, err := c.reader.ReadMessage(ctx)
			cancel()
			if err != nil {
				continue
			}

			// Extract metadata from headers
			metadata := make(map[string]string)
			for _, h := range msg.Headers {
				metadata[h.Key] = string(h.Value)
			}

			if !c.shuttingDown.Load() {
				c.msgChan <- testsuite.Message{
					Data:     msg.Value,
					Metadata: metadata,
					Raw:      msg,
				}
			}
		}
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
	return c.reader.Close()
}

// KafkaAsserter implements testsuite.MessageAsserter
type KafkaAsserter struct{}

func (a *KafkaAsserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	kafkaMsg, ok := msg.Raw.(kafka.Message)
	assert.True(t, ok, "raw message should be kafka.Message")

	// Verify content-type header
	assert.Equal(t, "application/json", msg.Metadata["content-type"])

	// Verify message key is set (event ID by default)
	assert.Equal(t, event.ID, string(kafkaMsg.Key), "message key should be event ID")

	// Verify system metadata in headers
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

// KafkaPublishSuite runs the shared publisher test suite for Kafka
type KafkaPublishSuite struct {
	testsuite.PublisherSuite
	consumer *KafkaConsumer
}

func (s *KafkaPublishSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))

	brokerAddr := testinfra.EnsureKafka()
	topic := "test-topic-" + idgen.String()

	// Ensure topic exists by creating it
	ensureKafkaTopic(t, brokerAddr, topic)

	provider, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("kafka"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"brokers": brokerAddr,
			"topic":   topic,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{}),
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
					"brokers": tt.brokers,
					"topic":   "test-topic",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{}),
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

// Helper to ensure a Kafka topic exists
func ensureKafkaTopic(t *testing.T, brokerAddr, topic string) {
	t.Helper()

	conn, err := kafka.Dial("tcp", brokerAddr)
	require.NoError(t, err)
	defer conn.Close()

	err = conn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	})
	require.NoError(t, err)

	// Give Kafka a moment to fully create the topic
	time.Sleep(500 * time.Millisecond)
}
