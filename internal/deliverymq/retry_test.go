package deliverymq_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/scheduler"
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
	deliveryMQ           *deliverymq.DeliveryMQ
	teardown             func()
	cancel               context.CancelFunc
}

// SetupTest must run inside a synctest bubble. TeardownTest stops every
// goroutine it started, so the bubble can exit.
func (s *RetryDeliveryMQSuite) SetupTest(t *testing.T) {
	require.NotNil(t, s.ctx, "RetryDeliveryMQSuite.ctx is not set")
	require.NotNil(t, s.mqConfig, "RetryDeliveryMQSuite.mqConfig is not set")
	require.NotNil(t, s.publisher, "RetryDeliveryMQSuite.publisher is not set")
	require.NotNil(t, s.eventGetter, "RetryDeliveryMQSuite.eventGetter is not set")
	require.NotNil(t, s.logPublisher, "RetryDeliveryMQSuite.logPublisher is not set")
	require.NotNil(t, s.destGetter, "RetryDeliveryMQSuite.destGetter is not set")
	s.ctx, s.cancel = context.WithCancel(s.ctx)
	_, redisConfig, redisClient := testutil.NewSynctestRedis(t)

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
	retryScheduler, err := deliverymq.NewRetryScheduler(s.deliveryMQ, redisConfig, "", pollBackoff, testutil.CreateTestLogger(t), s.eventGetter, deliverymq.WithRetryNotReadyDelay(time.Second))
	require.NoError(t, err)
	require.NoError(t, retryScheduler.Init(s.ctx))
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		retryScheduler.Monitor(s.ctx)
	}()

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
		idempotence.New(redisClient, idempotence.WithSuccessfulTTL(24*time.Hour)),
	)

	// Setup message consumer
	mq := mqs.NewQueue(s.mqConfig)
	subscription, err := mq.Subscribe(s.ctx)
	require.NoError(t, err)

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for {
			msg, err := subscription.Receive(context.Background())
			if err != nil {
				return
			}
			handler.Handle(s.ctx, msg)
		}
	}()

	s.teardown = func() {
		stopRetryStack(subscription, consumerDone, s.cancel, monitorDone, retryScheduler, cleanup)
	}
}

// stopRetryStack stops everything a bubble test started, so the bubble can
// exit. The subscription and its consumer are optional.
func stopRetryStack(subscription mqs.Subscription, consumerDone <-chan struct{}, cancel context.CancelFunc, monitorDone <-chan struct{}, s scheduler.Scheduler, cleanup func()) {
	synctest.Wait()
	if subscription != nil {
		subscription.Shutdown(context.Background())
		<-consumerDone
	}
	cancel()
	<-monitorDone
	s.Shutdown()
	cleanup()
	// mempubsub's receive loop sleeps 250ms between polls and ignores ctx;
	// wait for it to exit before the bubble ends.
	advance(time.Second)
}

// advance moves the fake clock forward by d and lets every goroutine in the
// bubble settle.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func (suite *RetryDeliveryMQSuite) TeardownTest(t *testing.T) {
	suite.teardown()
}

func TestDeliveryMQRetry_EligibleForRetryFalse(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Test scenario:
		// - Event is not eligible for retry
		// - Publish fails with a publish error (not system error)
		// - Should only attempt to publish once and not retry

		ctx, cancel := context.WithCancel(context.Background())
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

		suite := &RetryDeliveryMQSuite{
			ctx:                  ctx,
			mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}},
			publisher:            publisher,
			eventGetter:          eventGetter,
			logPublisher:         newMockLogPublisher(nil),
			destGetter:           &mockDestinationGetter{dest: &destination},
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

		advance(2 * time.Second)
		assert.Equal(t, 1, publisher.Current(), "should only attempt once when retry is not eligible")
	})
}

func TestDeliveryMQRetry_EligibleForRetryTrue(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Test scenario:
		// - Event is eligible for retry
		// - First two publish attempts fail with publish errors
		// - Third attempt succeeds
		// - Should attempt exactly 3 times

		ctx, cancel := context.WithCancel(context.Background())
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
		logPublisher := newMockLogPublisher(nil)
		logPublisher.eventGetter = eventGetter

		suite := &RetryDeliveryMQSuite{
			ctx:           ctx,
			mqConfig:      &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}},
			publisher:     publisher,
			eventGetter:   eventGetter,
			logPublisher:  logPublisher,
			destGetter:    &mockDestinationGetter{dest: &destination},
			retryMaxCount: 10,
			// Over retryLookupSkew: a shorter backoff lets the retry's lookup find the previous attempt.
			retryBackoff:         &backoff.ConstantBackoff{Interval: 2 * time.Second},
			schedulerPollBackoff: 10 * time.Millisecond,
		}
		suite.SetupTest(t)
		defer suite.TeardownTest(t)

		task := models.DeliveryTask{
			Event:         event,
			DestinationID: destination.ID,
		}
		require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

		advance(10 * time.Second)

		assert.Equal(t, 3, publisher.Current(), "should retry until success (2 failures + 1 success)")
	})
}

func TestDeliveryMQRetry_SystemError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Test scenario:
		// - Event is NOT eligible for retry
		// - But we get a system error (not a publish error)
		// - System errors should always trigger retry regardless of retry eligibility
		// - Should attempt multiple times (measured by handler executions)

		ctx, cancel := context.WithCancel(context.Background())
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

		suite := &RetryDeliveryMQSuite{
			ctx:                  ctx,
			mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}},
			publisher:            newMockPublisher(nil), // publisher won't be called due to early error
			eventGetter:          eventGetter,
			logPublisher:         newMockLogPublisher(nil),
			destGetter:           destGetter,
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

		advance(2 * time.Second)
		assert.Greater(t, destGetter.current, 1, "handler should execute multiple times on system error")
	})
}

func TestDeliveryMQRetry_RetryMaxCount(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Test scenario:
		// - Event is eligible for retry
		// - Publishing continuously fails with publish errors
		// - RetryMaxCount is 2 (allowing 1 initial + 2 retries = 3 total attempts)
		// - Should stop after max retries even though errors continue

		ctx, cancel := context.WithCancel(context.Background())
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
		logPublisher := newMockLogPublisher(nil)
		logPublisher.eventGetter = eventGetter

		suite := &RetryDeliveryMQSuite{
			ctx:           ctx,
			mqConfig:      &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}},
			publisher:     publisher,
			eventGetter:   eventGetter,
			logPublisher:  logPublisher,
			destGetter:    &mockDestinationGetter{dest: &destination},
			retryMaxCount: 2, // 1 initial + 2 retries = 3 total attempts
			// Over retryLookupSkew: a shorter backoff lets the retry's lookup find the previous attempt.
			retryBackoff:         &backoff.ConstantBackoff{Interval: 2 * time.Second},
			schedulerPollBackoff: 10 * time.Millisecond,
		}
		suite.SetupTest(t)
		defer suite.TeardownTest(t)

		task := models.DeliveryTask{
			Event:         event,
			DestinationID: destination.ID,
			Attempt:       1,
		}
		require.NoError(t, suite.deliveryMQ.Publish(ctx, task))

		// Three attempts take 4s of fake time; 10s also covers a 4th that must not happen.
		advance(10 * time.Second)

		assert.Equal(t, 3, publisher.Current(), "should stop after max retries (1 initial + 2 retries = 3 total attempts)")
	})
}

func TestRetryScheduler_EventNotFound(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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

		// Event getter has no records — simulates data not yet in logstore
		eventGetter := newMockEventGetter()

		suite := &RetryDeliveryMQSuite{
			ctx:                  ctx,
			mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}},
			publisher:            publisher,
			eventGetter:          eventGetter,
			logPublisher:         newMockLogPublisher(nil),
			destGetter:           &mockDestinationGetter{dest: &destination},
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

		// Initial delivery waits on the queue's 250ms receive poll; the retry
		// follows after the 50ms backoff + 10ms poll.
		advance(time.Second)
		eventGetter.mu.Lock()
		require.NotEmpty(t, eventGetter.requests, "retry should have looked up the event")
		eventGetter.mu.Unlock()

		// Should only have 1 attempt - the retry was skipped because no prior attempt found
		assert.Equal(t, 1, publisher.Current(), "should skip retry when no prior attempt found in logstore")
	})
}

func TestRetryScheduler_EventFetchError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
		eventGetter.err = errors.New("database connection error")

		suite := &RetryDeliveryMQSuite{
			ctx:                  ctx,
			mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}},
			publisher:            publisher,
			eventGetter:          eventGetter,
			logPublisher:         newMockLogPublisher(nil),
			destGetter:           &mockDestinationGetter{dest: &destination},
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

		// Initial delivery waits on the queue's 250ms receive poll; the retry
		// follows after the 50ms backoff + 10ms poll.
		advance(time.Second)
		eventGetter.mu.Lock()
		require.NotEmpty(t, eventGetter.requests, "retry should have looked up the event")
		eventGetter.mu.Unlock()

		// Delivery should still be at 1 because event fetch error prevented retry delivery
		// Note: The retry message is NOT deleted, it will be retried after visibility timeout (30s)
		assert.Equal(t, 1, publisher.Current(), "retry delivery should not proceed when event fetch fails")
	})
}

func TestRetryScheduler_EventFetchSuccess(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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

		eventGetter := newMockEventGetter()

		logPublisher := newMockLogPublisher(nil)
		logPublisher.eventGetter = eventGetter

		suite := &RetryDeliveryMQSuite{
			ctx:                  ctx,
			mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}},
			publisher:            publisher,
			eventGetter:          eventGetter,
			logPublisher:         logPublisher,
			destGetter:           &mockDestinationGetter{dest: &destination},
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

		advance(3 * time.Second)

		assert.Equal(t, 2, publisher.Current(), "should complete 2 delivery attempts (initial failure + successful retry)")

		// Verify that the retry delivery had full event data by checking log entries
		require.Len(t, logPublisher.entries, 2, "should have 2 delivery log entries")

		// Both log entries should have non-zero event Time (full event data)
		assert.False(t, logPublisher.entries[0].Event.Time.IsZero(), "first delivery should have full event data")
		assert.False(t, logPublisher.entries[1].Event.Time.IsZero(), "retry delivery should have full event data (fetched from logstore)")

		eventGetter.mu.Lock()
		defer eventGetter.mu.Unlock()
		require.NotEmpty(t, eventGetter.requests)
		for _, req := range eventGetter.requests {
			require.NotNil(t, req.TimeFilter.GTE, "retry lookup should be time bounded")
			assert.True(t, req.TimeFilter.GTE.Before(logPublisher.entries[0].Attempt.Time))
		}
	})
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
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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

		// Event getter returns (nil, nil) on first call, then returns event
		// This simulates: logmq hasn't persisted the event yet when retry first runs
		eventGetter := newMockDelayedEventGetter(&event, 1) // Return nil for first call

		// Setup deliveryMQ
		mqConfig := &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}}
		deliveryMQ := deliverymq.New(deliverymq.WithQueue(mqConfig))
		cleanup, err := deliveryMQ.Init(ctx)
		require.NoError(t, err)
		_, redisConfig, redisClient := testutil.NewSynctestRedis(t)

		// Setup retry scheduler; when the event is not found the task is requeued after 1-2 seconds
		retryScheduler, err := deliverymq.NewRetryScheduler(
			deliveryMQ,
			redisConfig,
			"",
			10*time.Millisecond, // Fast polling
			testutil.CreateTestLogger(t),
			eventGetter,
			deliverymq.WithRetryNotReadyDelay(0),
		)
		require.NoError(t, err)
		require.NoError(t, retryScheduler.Init(ctx))
		monitorDone := make(chan struct{})
		go func() {
			defer close(monitorDone)
			retryScheduler.Monitor(ctx)
		}()

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
			idempotence.New(redisClient, idempotence.WithSuccessfulTTL(24*time.Hour)),
		)

		// Setup message consumer
		mq := mqs.NewQueue(mqConfig)
		subscription, err := mq.Subscribe(ctx)
		require.NoError(t, err)
		consumerDone := make(chan struct{})
		go func() {
			defer close(consumerDone)
			for {
				msg, err := subscription.Receive(context.Background())
				if err != nil {
					return
				}
				handler.Handle(ctx, msg)
			}
		}()

		defer stopRetryStack(subscription, consumerDone, cancel, monitorDone, retryScheduler, cleanup)

		// Publish task with full event data (simulates initial delivery)
		task := models.DeliveryTask{
			Event:         event,
			DestinationID: destination.ID,
		}
		require.NoError(t, deliveryMQ.Publish(ctx, task))

		// The initial delivery waits on the queue's 250ms receive poll; the
		// not-ready requeue takes at least 1s.
		advance(500 * time.Millisecond)
		require.Equal(t, 1, publisher.Current(), "initial delivery should complete")

		// Wait for retry to be processed:
		// - First retry attempt: event not found, message returns to queue
		// - After the not-ready delay (1-2s): message becomes visible again
		// - Second retry attempt: event now available, delivery succeeds
		advance(5 * time.Second)
		require.Equal(t, 2, publisher.Current(), "expected 2 delivery attempts (initial + retry after event becomes available)")
	})
}
