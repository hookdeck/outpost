package testing

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
)

// Message represents a message that was received by the consumer
type Message struct {
	Data     []byte
	Metadata map[string]string
}

// MessageConsumer is the interface that providers must implement
type MessageConsumer interface {
	// Consume returns a channel that receives messages
	Consume() <-chan Message
	// Close stops consuming messages
	Close() error
}

// Config is used to initialize the test suite
type Config struct {
	Provider destregistry.Provider
	Dest     *models.Destination
	Consumer MessageConsumer
}

// PublisherSuite is the base test suite that providers should embed
type PublisherSuite struct {
	suite.Suite
	provider destregistry.Provider
	dest     *models.Destination
	consumer MessageConsumer
	pub      destregistry.Publisher
}

func (s *PublisherSuite) InitSuite(cfg Config) {
	s.provider = cfg.Provider
	s.dest = cfg.Dest
	s.consumer = cfg.Consumer
}

func (s *PublisherSuite) SetupTest() {
	// Create a new publisher for each test
	pub, err := s.provider.CreatePublisher(context.Background(), s.dest)
	s.Require().NoError(err)
	s.pub = pub
}

func (s *PublisherSuite) TearDownTest() {
	if s.pub != nil {
		s.pub.Close()
	}
}

// Common test cases that all providers get "for free"

func (s *PublisherSuite) TestBasicPublish() {
	// Create test event with realistic data
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithData(map[string]interface{}{
			"test_key": "test_value",
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"meta_key": "meta_value",
		}),
	)

	// Publish event
	err := s.pub.Publish(context.Background(), &event)
	s.Require().NoError(err)

	// Verify message was delivered
	select {
	case msg := <-s.consumer.Consume():
		var body map[string]interface{}
		err = json.Unmarshal(msg.Data, &body)
		s.Require().NoError(err)
		s.Equal("test_value", body["test_key"])
		s.Equal("meta_value", msg.Metadata["meta_key"])
	case <-time.After(5 * time.Second):
		s.Fail("timeout waiting for message")
	}
}

func (s *PublisherSuite) TestConcurrentPublish() {
	const numMessages = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numMessages)

	// Publish messages concurrently
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(messageID int) {
			defer wg.Done()
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{
					"message_id": messageID,
				}),
			)
			if err := s.pub.Publish(context.Background(), &event); err != nil {
				errChan <- err
			}
		}(i)
	}

	// Wait for all publishes to complete
	wg.Wait()
	close(errChan)

	// Check for any publish errors
	for err := range errChan {
		s.Require().NoError(err)
	}

	// Verify all messages were received
	receivedMessages := make(map[int]bool)
	timeout := time.After(5 * time.Second)

	for i := 0; i < numMessages; i++ {
		select {
		case msg := <-s.consumer.Consume():
			var body map[string]interface{}
			err := json.Unmarshal(msg.Data, &body)
			s.Require().NoError(err)
			messageID := int(body["message_id"].(float64))
			receivedMessages[messageID] = true
		case <-timeout:
			s.Fail("timeout waiting for messages")
		}
	}

	s.Len(receivedMessages, numMessages)
}

func (s *PublisherSuite) TestClosePublisherDuringConcurrentPublish() {
	const totalMessages = 1000
	var successCount atomic.Int32
	var closedCount atomic.Int32

	// Start publishing messages
	var wg sync.WaitGroup
	for i := 0; i < totalMessages; i++ {
		wg.Add(1)
		go func(messageID int) {
			defer wg.Done()
			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{
					"message_id": messageID,
				}),
			)

			err := s.pub.Publish(context.Background(), &event)
			if err == nil {
				successCount.Add(1)
			} else if errors.Is(err, destregistry.ErrPublisherClosed) {
				closedCount.Add(1)
			} else {
				s.Failf("unexpected error", "got %v", err)
			}
		}(i)
	}

	// Close the publisher
	err := s.pub.Close()
	s.Require().NoError(err)

	// Wait for all publish attempts to complete
	wg.Wait()

	// Verify counts
	total := successCount.Load() + closedCount.Load()
	s.Equal(int32(totalMessages), total, "all publish attempts should either succeed or get closed error")
	s.Greater(successCount.Load(), int32(0), "some messages should be published successfully")
	s.Greater(closedCount.Load(), int32(0), "some messages should be rejected due to closed publisher")

	// Verify successful messages were delivered
	receivedCount := 0
	expectedCount := int(successCount.Load())
	receivedMessages := make(map[int]bool)
	timeout := time.After(5 * time.Second)

	for receivedCount < expectedCount {
		select {
		case msg := <-s.consumer.Consume():
			var body map[string]interface{}
			err := json.Unmarshal(msg.Data, &body)
			s.Require().NoError(err)
			messageID := int(body["message_id"].(float64))
			receivedMessages[messageID] = true
			receivedCount++
		case <-timeout:
			s.Failf("timeout waiting for messages", "got %d/%d", receivedCount, expectedCount)
		}
	}

	s.Equal(expectedCount, len(receivedMessages), "should receive all successfully published messages")
}
