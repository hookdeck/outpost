package deliverymq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type RetryDeliveryMQSuite struct {
	ctx                  context.Context
	mqConfig             *mqs.QueueConfig
	retryMaxCount        int
	retryBackoff         backoff.Backoff
	schedulerPollBackoff time.Duration
	publisher            deliverymq.Publisher
	eventGetter          deliverymq.RetryEventGetter
	logPublisher         deliverymq.LogPublisher
	destGetter           deliverymq.DestinationGetter
	alertMonitor         deliverymq.AlertMonitor
	deliveryMQ           *deliverymq.DeliveryMQ
	teardown             func()
}

func (s *RetryDeliveryMQSuite) SetupTest(t *testing.T) {
	require.NotNil(t, s.ctx, "RetryDeliveryMQSuite.ctx is not set")
	require.NotNil(t, s.mqConfig, "RetryDeliveryMQSuite.mqConfig is not set")
	require.NotNil(t, s.publisher, "RetryDeliveryMQSuite.publisher is not set")
	require.NotNil(t, s.eventGetter, "RetryDeliveryMQSuite.eventGetter is not set")
	require.NotNil(t, s.logPublisher, "RetryDeliveryMQSuite.logPublisher is not set")
	require.NotNil(t, s.destGetter, "RetryDeliveryMQSuite.destGetter is not set")
	require.NotNil(t, s.alertMonitor, "RetryDeliveryMQSuite.alertMonitor is not set")

	// Setup delivery MQ and handler
	s.deliveryMQ = deliverymq.New(deliverymq.WithQueue(s.mqConfig))
	cleanup, err := s.deliveryMQ.Init(s.ctx)
	require.NoError(t, err)

	// Setup retry scheduler
	// Use provided poll backoff or default to 100ms
	pollBackoff := s.schedulerPollBackoff
	if pollBackoff == 0 {
		pollBackoff = 100 * time.Millisecond
	}
	retryScheduler, err := deliverymq.NewRetryScheduler(s.deliveryMQ, testutil.CreateTestRedisConfig(t), "", pollBackoff, testutil.CreateTestLogger(t), s.eventGetter)
	require.NoError(t, err)
	require.NoError(t, retryScheduler.Init(s.ctx))
	go retryScheduler.Monitor(s.ctx)

	// Setup message handler
	// Use provided backoff or default to 1 second
	retryBackoff := s.retryBackoff
	if retryBackoff == nil {
		retryBackoff = &backoff.ConstantBackoff{Interval: 1 * time.Second}
	}
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		s.logPublisher,
		s.destGetter,
		s.publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		retryBackoff,
		s.retryMaxCount,
		s.alertMonitor,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(24*time.Hour)),
	)

	// Setup message consumer
	mq := mqs.NewQueue(s.mqConfig)
	subscription, err := mq.Subscribe(s.ctx)
	require.NoError(t, err)

	go func() {
		for {
			msg, err := subscription.Receive(s.ctx)
			if err != nil {
				return
			}
			handler.Handle(s.ctx, msg)
		}
	}()

	s.teardown = func() {
		subscription.Shutdown(s.ctx)
		retryScheduler.Shutdown()
		cleanup()
	}
}

func (suite *RetryDeliveryMQSuite) TeardownTest(t *testing.T) {
	suite.teardown()
}

func TestDeliveryMQRetry_EligibleForRetryFalse(t *testing.T) {
	// Test scenario:
	// - Event is not eligible for retry
	// - Publish fails with a publish error (not system error)
	// - Should only attempt to publish once and not retry

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(false), // key test condition
	)

	// Setup mocks
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 400"),
			Provider: "webhook",
		},
	})
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)

	suite := &RetryDeliveryMQSuite{
		ctx:                  ctx,
		mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:            publisher,
		eventGetter:          eventGetter,
		logPublisher:         newMockLogPublisher(nil),
		destGetter:           &mockDestinationGetter{dest: &destination},
		alertMonitor:         newMockAlertMonitor(),
		retryMaxCount:        10,
		retryBackoff:         &backoff.ConstantBackoff{Interval: 50 * time.Millisecond},
		schedulerPollBackoff: 10 * time.Millisecond,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

	<-ctx.Done()
	assert.Equal(t, 1, publisher.Current(), "should only attempt once when retry is not eligible")
}

func TestDeliveryMQRetry_EligibleForRetryTrue(t *testing.T) {
	// Test scenario:
	// - Event is eligible for retry
	// - First two publish attempts fail with publish errors
	// - Third attempt succeeds
	// - Should attempt exactly 3 times

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true), // key test condition
	)

	// Setup mocks with two failures then success
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 429"),
			Provider: "webhook",
		},
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 503"),
			Provider: "webhook",
		},
		nil, // succeeds on 3rd try
	})
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)

	suite := &RetryDeliveryMQSuite{
		ctx:                  ctx,
		mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:            publisher,
		eventGetter:          eventGetter,
		logPublisher:         newMockLogPublisher(nil),
		destGetter:           &mockDestinationGetter{dest: &destination},
		alertMonitor:         newMockAlertMonitor(),
		retryMaxCount:        10,
		retryBackoff:         &backoff.ConstantBackoff{Interval: 50 * time.Millisecond},
		schedulerPollBackoff: 10 * time.Millisecond,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

	// Wait for all attempts to complete
	// Note: 50ms backoff + 10ms poll interval = fast, deterministic retries
	require.Eventually(t, func() bool {
		return publisher.Current() >= 3
	}, 5*time.Second, 10*time.Millisecond, "should complete 3 attempts (2 failures + 1 success)")

	assert.Equal(t, 3, publisher.Current(), "should retry until success (2 failures + 1 success)")
}

func TestDeliveryMQRetry_SystemError(t *testing.T) {
	// Test scenario:
	// - Event is NOT eligible for retry
	// - But we get a system error (not a publish error)
	// - System errors should always trigger retry regardless of retry eligibility
	// - Should attempt multiple times (measured by handler executions)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(false), // even with retry disabled
	)

	// Setup mocks with system error
	destGetter := &mockDestinationGetter{err: errors.New("destination lookup failed")}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)

	suite := &RetryDeliveryMQSuite{
		ctx:                  ctx,
		mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:            newMockPublisher(nil), // publisher won't be called due to early error
		eventGetter:          eventGetter,
		logPublisher:         newMockLogPublisher(nil),
		destGetter:           destGetter,
		alertMonitor:         newMockAlertMonitor(),
		retryMaxCount:        10,
		retryBackoff:         &backoff.ConstantBackoff{Interval: 50 * time.Millisecond},
		schedulerPollBackoff: 10 * time.Millisecond,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

	<-ctx.Done()
	assert.Greater(t, destGetter.current, 1, "handler should execute multiple times on system error")
}

func TestDeliveryMQRetry_RetryMaxCount(t *testing.T) {
	// Test scenario:
	// - Event is eligible for retry
	// - Publishing continuously fails with publish errors
	// - RetryMaxCount is 2 (allowing 1 initial + 2 retries = 3 total attempts)
	// - Should stop after max retries even though errors continue

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)

	// Setup mocks with continuous publish failures
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 429"),
			Provider: "webhook",
		},
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 429"),
			Provider: "webhook",
		},
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 429"),
			Provider: "webhook",
		},
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 429"),
			Provider: "webhook",
		}, // 4th attempt should never happen
	})
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)

	suite := &RetryDeliveryMQSuite{
		ctx:                  ctx,
		mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:            publisher,
		eventGetter:          eventGetter,
		logPublisher:         newMockLogPublisher(nil),
		destGetter:           &mockDestinationGetter{dest: &destination},
		alertMonitor:         newMockAlertMonitor(),
		retryMaxCount:        2, // 1 initial + 2 retries = 3 total attempts
		retryBackoff:         &backoff.ConstantBackoff{Interval: 50 * time.Millisecond},
		schedulerPollBackoff: 10 * time.Millisecond,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

	// Poll until we get 3 attempts or timeout
	// With 50ms backoff + 10ms poll: initial + 60ms + retry + 60ms + retry = ~150ms minimum
	require.Eventually(t, func() bool {
		return publisher.Current() >= 3
	}, 5*time.Second, 10*time.Millisecond, "should complete 3 attempts (1 initial + 2 retries)")

	assert.Equal(t, 3, publisher.Current(), "should stop after max retries (1 initial + 2 retries = 3 total attempts)")
}

func TestRetryScheduler_EventNotFound(t *testing.T) {
	// Test scenario:
	// - Initial delivery fails and schedules a retry
	// - Before retry executes, the event is deleted from logstore
	// - Retry scheduler should skip publishing (not error) when event returns (nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)

	// Setup mocks - publisher fails on first attempt
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 503"),
			Provider: "webhook",
		},
	})

	// Event getter does NOT have the event registered
	// This simulates event being deleted from logstore before retry
	eventGetter := newMockEventGetter()
	// Intentionally NOT calling: eventGetter.registerEvent(&event)

	suite := &RetryDeliveryMQSuite{
		ctx:                  ctx,
		mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:            publisher,
		eventGetter:          eventGetter,
		logPublisher:         newMockLogPublisher(nil),
		destGetter:           &mockDestinationGetter{dest: &destination},
		alertMonitor:         newMockAlertMonitor(),
		retryMaxCount:        10,
		retryBackoff:         &backoff.ConstantBackoff{Interval: 50 * time.Millisecond},
		schedulerPollBackoff: 10 * time.Millisecond,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	// Publish task with full event data (simulates initial delivery)
	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

	// Wait for initial delivery attempt and retry scheduling
	require.Eventually(t, func() bool {
		return publisher.Current() >= 1
	}, 2*time.Second, 10*time.Millisecond, "should complete initial delivery attempt")

	// Wait enough time for retry to be processed (if it were to happen)
	// 50ms backoff + 10ms poll = 60ms minimum for retry
	time.Sleep(200 * time.Millisecond)

	// Should only have 1 attempt - the retry was skipped because event not found
	assert.Equal(t, 1, publisher.Current(), "should skip retry when event not found in logstore (returns nil, nil)")
}

func TestRetryScheduler_EventFetchError(t *testing.T) {
	// Test scenario:
	// - Initial delivery fails and schedules a retry
	// - When retry scheduler tries to fetch event, it gets a transient error
	// - Retry scheduler should return error (which means message is not deleted)
	// - The message stays in queue for retry after visibility timeout
	// - Delivery should NOT proceed when event fetch fails

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)

	// Setup mocks - publisher fails on first attempt
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 503"),
			Provider: "webhook",
		},
		nil, // Second attempt would succeed if it were reached
	})

	// Event getter returns error (simulating transient DB error)
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	eventGetter.err = errors.New("database connection error")

	suite := &RetryDeliveryMQSuite{
		ctx:                  ctx,
		mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:            publisher,
		eventGetter:          eventGetter,
		logPublisher:         newMockLogPublisher(nil),
		destGetter:           &mockDestinationGetter{dest: &destination},
		alertMonitor:         newMockAlertMonitor(),
		retryMaxCount:        10,
		retryBackoff:         &backoff.ConstantBackoff{Interval: 50 * time.Millisecond},
		schedulerPollBackoff: 10 * time.Millisecond,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	// Publish task with full event data (simulates initial delivery)
	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

	// Wait for initial delivery attempt
	require.Eventually(t, func() bool {
		return publisher.Current() >= 1
	}, 2*time.Second, 10*time.Millisecond, "should complete initial delivery attempt")

	// Wait enough time for retry to be attempted (but it should fail with event fetch error)
	// 50ms backoff + 10ms poll = 60ms minimum for retry attempt
	time.Sleep(200 * time.Millisecond)

	// Delivery should still be at 1 because event fetch error prevented retry delivery
	// Note: The retry message is NOT deleted, it will be retried after visibility timeout (30s)
	assert.Equal(t, 1, publisher.Current(), "retry delivery should not proceed when event fetch fails")
}

func TestRetryScheduler_EventFetchSuccess(t *testing.T) {
	// Test scenario:
	// - Initial delivery fails and schedules a retry
	// - Retry scheduler successfully fetches event from logstore
	// - DeliveryTask published to deliverymq should have full event data (non-zero Time)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)

	// Setup mocks - publisher fails on first attempt, succeeds on second
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 503"),
			Provider: "webhook",
		},
		nil, // Second attempt succeeds
	})

	// Event getter has the event registered
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)

	logPublisher := newMockLogPublisher(nil)

	suite := &RetryDeliveryMQSuite{
		ctx:                  ctx,
		mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:            publisher,
		eventGetter:          eventGetter,
		logPublisher:         logPublisher,
		destGetter:           &mockDestinationGetter{dest: &destination},
		alertMonitor:         newMockAlertMonitor(),
		retryMaxCount:        10,
		retryBackoff:         &backoff.ConstantBackoff{Interval: 50 * time.Millisecond},
		schedulerPollBackoff: 10 * time.Millisecond,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	// Publish task with full event data (simulates initial delivery)
	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

	// Wait for both delivery attempts to complete
	require.Eventually(t, func() bool {
		return publisher.Current() >= 2
	}, 3*time.Second, 10*time.Millisecond, "should complete 2 delivery attempts")

	assert.Equal(t, 2, publisher.Current(), "should complete 2 delivery attempts (initial failure + successful retry)")

	// Verify that the retry delivery had full event data by checking log entries
	require.Len(t, logPublisher.entries, 2, "should have 2 delivery log entries")

	// Both log entries should have non-zero event Time (full event data)
	assert.False(t, logPublisher.entries[0].Event.Time.IsZero(), "first delivery should have full event data")
	assert.False(t, logPublisher.entries[1].Event.Time.IsZero(), "retry delivery should have full event data (fetched from logstore)")
}

// TestRetryScheduler_RaceCondition_EventNotYetPersisted verifies that retries are not
// lost when the retry scheduler queries logstore before the event has been persisted.
//
// Scenario:
//  1. Initial delivery fails, retry is scheduled
//  2. Retry scheduler runs and queries logstore for event data
//  3. Event is not yet persisted (logmq batching delay)
//  4. Retry should remain in queue and be reprocessed later
//  5. Once event is available, retry succeeds
func TestRetryScheduler_RaceCondition_EventNotYetPersisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)

	// Publisher: fails first attempt, succeeds after
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 503"),
			Provider: "webhook",
		},
	})
	logPublisher := newMockLogPublisher(nil)
	destGetter := &mockDestinationGetter{dest: &destination}
	alertMonitor := newMockAlertMonitor()

	// Event getter returns (nil, nil) on first call, then returns event
	// This simulates: logmq hasn't persisted the event yet when retry first runs
	eventGetter := newMockDelayedEventGetter(&event, 1) // Return nil for first call

	// Setup deliveryMQ
	mqConfig := &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(mqConfig))
	cleanup, err := deliveryMQ.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	// Setup retry scheduler with short visibility timeout for faster test
	// When event is not found, the message will be retried after 1 second
	retryScheduler, err := deliverymq.NewRetryScheduler(
		deliveryMQ,
		testutil.CreateTestRedisConfig(t),
		"",
		10*time.Millisecond, // Fast polling
		testutil.CreateTestLogger(t),
		eventGetter,
		deliverymq.WithRetryVisibilityTimeout(1), // 1 second visibility timeout
	)
	require.NoError(t, err)
	require.NoError(t, retryScheduler.Init(ctx))
	go retryScheduler.Monitor(ctx)
	defer retryScheduler.Shutdown()

	// Setup message handler with short retry backoff
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		logPublisher,
		destGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 50 * time.Millisecond}, // Short backoff
		10,
		alertMonitor,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(24*time.Hour)),
	)

	// Setup message consumer
	mq := mqs.NewQueue(mqConfig)
	subscription, err := mq.Subscribe(ctx)
	require.NoError(t, err)
	defer subscription.Shutdown(ctx)

	go func() {
		for {
			msg, err := subscription.Receive(ctx)
			if err != nil {
				return
			}
			handler.Handle(ctx, msg)
		}
	}()

	// Publish task with full event data (simulates initial delivery)
	task := models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, deliveryMQ.Publish(ctx, task))

	// Wait for initial delivery to fail and retry to be scheduled
	require.Eventually(t, func() bool {
		return publisher.Current() >= 1
	}, 2*time.Second, 10*time.Millisecond, "initial delivery should complete")

	// Wait for retry to be processed:
	// - First retry attempt: event not found, message returns to queue
	// - After 1s visibility timeout: message becomes visible again
	// - Second retry attempt: event now available, delivery succeeds
	time.Sleep(2 * time.Second)

	// Should have 2 publish attempts: initial failure + successful retry
	assert.Equal(t, 2, publisher.Current(),
		"expected 2 delivery attempts (initial + retry after event becomes available)")
}
