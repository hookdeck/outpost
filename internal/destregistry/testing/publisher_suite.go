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
	// Raw contains the provider-specific message type (e.g. amqp.Delivery, http.Request)
	Raw interface{}
}

// MessageAsserter allows providers to add their own assertions on the raw message
type MessageAsserter interface {
	// AssertMessage is called after the base assertions to allow provider-specific checks
	AssertMessage(t TestingT, msg Message, event models.Event)
}

// TestingT is a subset of testing.T that we need for assertions
type TestingT interface {
	Errorf(format string, args ...interface{})
	FailNow()
	Helper()
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
	// Optional asserter for provider-specific message checks
	Asserter MessageAsserter
}

// PublisherSuite is the base test suite that providers should embed
type PublisherSuite struct {
	suite.Suite
	provider destregistry.Provider
	dest     *models.Destination
	consumer MessageConsumer
	asserter MessageAsserter
	pub      destregistry.Publisher
}

func (s *PublisherSuite) InitSuite(cfg Config) {
	s.provider = cfg.Provider
	s.dest = cfg.Dest
	s.consumer = cfg.Consumer
	s.asserter = cfg.Asserter
}

func (s *PublisherSuite) SetupTest() {
	pub, err := s.provider.CreatePublisher(context.Background(), s.dest)
	s.Require().NoError(err)
	s.pub = pub
}

func (s *PublisherSuite) TearDownTest() {
	if s.pub != nil {
		// Add timeout to Close() call
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		done := make(chan struct{})
		go func() {
			s.pub.Close()
			close(done)
		}()

		select {
		case <-done:
			// Close completed
		case <-closeCtx.Done():
			s.Fail("Close() timed out")
		}
	}
}

// verifyMessage performs base message verification and calls provider-specific assertions
func (s *PublisherSuite) verifyMessage(msg Message, event models.Event) {
	// Base verification of data and metadata
	var body map[string]interface{}
	err := json.Unmarshal(msg.Data, &body)
	s.Require().NoError(err, "failed to unmarshal message data")

	// Compare data by converting both to JSON first to handle type differences
	eventDataJSON, err := json.Marshal(event.Data)
	s.Require().NoError(err, "failed to marshal event data")
	msgDataJSON, err := json.Marshal(body)
	s.Require().NoError(err, "failed to marshal message data")
	s.Require().JSONEq(string(eventDataJSON), string(msgDataJSON), "message data mismatch")

	// Compare metadata by converting both to map[string]string
	s.Require().Equal(map[string]string(event.Metadata), msg.Metadata, "message metadata mismatch")

	// Provider-specific assertions if available
	if s.asserter != nil {
		s.asserter.AssertMessage(s.T(), msg, event)
	}
}

func (s *PublisherSuite) TestBasicPublish() {
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithData(map[string]interface{}{
			"test_key": "test_value",
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"meta_key": "meta_value",
		}),
	)

	err := s.pub.Publish(context.Background(), &event)
	s.Require().NoError(err)

	select {
	case msg := <-s.consumer.Consume():
		s.verifyMessage(msg, event)
	case <-time.After(5 * time.Second):
		s.Fail("timeout waiting for message")
	}
}

func (s *PublisherSuite) TestConcurrentPublish() {
	const numMessages = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numMessages)

	events := make([]models.Event, numMessages)
	for i := 0; i < numMessages; i++ {
		events[i] = testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{
				"message_id": i,
			}),
		)
	}

	// Publish messages concurrently
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.pub.Publish(context.Background(), &events[i]); err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		s.Require().NoError(err)
	}

	// Verify all messages were received
	receivedMessages := make(map[int]bool)
	timeout := time.After(5 * time.Second)

	for i := 0; i < numMessages; i++ {
		select {
		case msg := <-s.consumer.Consume():
			// Get the message ID first
			var body map[string]interface{}
			err := json.Unmarshal(msg.Data, &body)
			s.Require().NoError(err)
			messageID := int(body["message_id"].(float64))

			// Verify against the correct event
			s.verifyMessage(msg, events[messageID])
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

	var wg sync.WaitGroup
	events := make([]models.Event, totalMessages)
	for i := 0; i < totalMessages; i++ {
		events[i] = testutil.EventFactory.Any(
			testutil.EventFactory.WithData(map[string]interface{}{
				"message_id": i,
			}),
		)
	}

	// Start publishing messages
	for i := 0; i < totalMessages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.pub.Publish(context.Background(), &events[i])
			if err == nil {
				successCount.Add(1)
			} else if errors.Is(err, destregistry.ErrPublisherClosed) {
				closedCount.Add(1)
			} else {
				s.Failf("unexpected error", "got %v", err)
			}
		}(i)
	}

	// Add timeout to Close() call
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	var closeErr error
	go func() {
		closeErr = s.pub.Close()
		close(done)
	}()

	select {
	case <-done:
		// Close completed
	case <-closeCtx.Done():
		s.Fail("Close() timed out")
	}
	s.Require().NoError(closeErr)

	// Add timeout to wg.Wait()
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		// Wait completed
	case <-time.After(5 * time.Second):
		s.Fail("timed out waiting for publishes to complete")
		return
	}

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

			// Only verify messages that were successfully published
			s.verifyMessage(msg, events[messageID])
			receivedMessages[messageID] = true
			receivedCount++
		case <-timeout:
			s.Failf("timeout waiting for messages", "got %d/%d", receivedCount, expectedCount)
			return
		}
	}
}
