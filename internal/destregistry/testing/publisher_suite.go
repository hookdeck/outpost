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

	// Verify that expected metadata is a subset of received metadata
	for k, v := range event.Metadata {
		s.Require().Equal(v, msg.Metadata[k], "metadata key %s should match expected value", k)
	}

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
	const maxFailedAttempts = 10
	const maxTotalAttempts = 100
	const publishInterval = 20 * time.Millisecond
	const closeAfter = 150 * time.Millisecond

	var successCount atomic.Int32
	var closedCount atomic.Int32
	var failedCount atomic.Int32
	var totalAttempts atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start publishing messages at a fixed rate
	publishDone := make(chan struct{})
	go func() {
		defer close(publishDone)
		messageID := 0
		ticker := time.NewTicker(publishInterval)
		defer ticker.Stop()

		for failedCount.Load() < maxFailedAttempts && totalAttempts.Load() < maxTotalAttempts {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				totalAttempts.Add(1)
				event := testutil.EventFactory.Any(
					testutil.EventFactory.WithData(map[string]interface{}{
						"message_id": messageID,
					}),
				)
				messageID++

				err := s.pub.Publish(ctx, &event)
				if err == nil {
					successCount.Add(1)
				} else if errors.Is(err, destregistry.ErrPublisherClosed) {
					closedCount.Add(1)
					failedCount.Add(1)
				} else {
					s.Failf("unexpected error", "got %v", err)
					return
				}
			}
		}
	}()

	// Close publisher after fixed delay
	time.Sleep(closeAfter)
	closeErr := s.pub.Close()
	s.Require().NoError(closeErr)

	// Wait for publishing to complete
	<-publishDone

	total := successCount.Load() + closedCount.Load()
	s.Greater(total, int32(0), "should have processed some messages")
	s.Greater(successCount.Load(), int32(0), "some messages should be published successfully")
	s.Greater(failedCount.Load(), int32(0), "some messages should be rejected due to closed publisher")
	s.LessOrEqual(totalAttempts.Load(), int32(maxTotalAttempts), "should not exceed max total attempts")

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
			expectedEvent := testutil.EventFactory.Any(
				testutil.EventFactory.WithData(map[string]interface{}{
					"message_id": messageID,
				}),
			)
			s.verifyMessage(msg, expectedEvent)
			receivedMessages[messageID] = true
			receivedCount++
		case <-timeout:
			s.Failf("timeout waiting for messages", "got %d/%d", receivedCount, expectedCount)
			return
		}
	}
}
