package deliverymq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type RetryDeliveryMQSuite struct {
	ctx           context.Context
	mqConfig      *mqs.QueueConfig
	retryMaxCount int
	publisher     deliverymq.Publisher
	eventGetter   deliverymq.EventGetter
	logPublisher  deliverymq.LogPublisher
	destGetter    deliverymq.DestinationGetter
	alertMonitor  deliverymq.AlertMonitor
	deliveryMQ    *deliverymq.DeliveryMQ
	teardown      func()
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
	retryScheduler := deliverymq.NewRetryScheduler(s.deliveryMQ, testutil.CreateTestRedisConfig(t))
	require.NoError(t, retryScheduler.Init(s.ctx))
	go retryScheduler.Monitor(s.ctx)

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		s.logPublisher,
		s.destGetter,
		s.eventGetter,
		s.publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		s.retryMaxCount,
		s.alertMonitor,
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
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
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
		ctx:           ctx,
		mqConfig:      &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:     publisher,
		eventGetter:   eventGetter,
		logPublisher:  newMockLogPublisher(nil),
		destGetter:    &mockDestinationGetter{dest: &destination},
		alertMonitor:  newMockAlertMonitor(),
		retryMaxCount: 10,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, deliveryEvent))

	<-ctx.Done()
	assert.Equal(t, 1, publisher.Current(), "should only attempt once when retry is not eligible")
}

func TestDeliveryMQRetry_EligibleForRetryTrue(t *testing.T) {
	// Test scenario:
	// - Event is eligible for retry
	// - First two publish attempts fail with publish errors
	// - Third attempt succeeds
	// - Should attempt exactly 3 times
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
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
		ctx:           ctx,
		mqConfig:      &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:     publisher,
		eventGetter:   eventGetter,
		logPublisher:  newMockLogPublisher(nil),
		destGetter:    &mockDestinationGetter{dest: &destination},
		alertMonitor:  newMockAlertMonitor(),
		retryMaxCount: 10,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, deliveryEvent))

	// Wait for all attempts to complete
	done := make(chan struct{})
	go func() {
		for {
			if publisher.Current() >= 3 {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-ctx.Done():
		t.Fatal("test timed out waiting for attempts to complete")
	case <-done:
		// Continue with assertions
	}

	assert.Equal(t, 3, publisher.Current(), "should retry until success (2 failures + 1 success)")
}

func TestDeliveryMQRetry_SystemError(t *testing.T) {
	// Test scenario:
	// - Event is NOT eligible for retry
	// - But we get a system error (not a publish error)
	// - System errors should always trigger retry regardless of retry eligibility
	// - Should attempt multiple times (measured by handler executions)
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
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
		ctx:           ctx,
		mqConfig:      &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:     newMockPublisher(nil), // publisher won't be called due to early error
		eventGetter:   eventGetter,
		logPublisher:  newMockLogPublisher(nil),
		destGetter:    destGetter,
		alertMonitor:  newMockAlertMonitor(),
		retryMaxCount: 10,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, deliveryEvent))

	<-ctx.Done()
	assert.Greater(t, destGetter.current, 1, "handler should execute multiple times on system error")
}

func TestDeliveryMQRetry_RetryMaxCount(t *testing.T) {
	// Test scenario:
	// - Event is eligible for retry
	// - Publishing continuously fails with publish errors
	// - RetryMaxCount is 2 (allowing 1 initial + 2 retries = 3 total attempts)
	// - Should stop after max retries even though errors continue
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
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
		ctx:           ctx,
		mqConfig:      &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
		publisher:     publisher,
		eventGetter:   eventGetter,
		logPublisher:  newMockLogPublisher(nil),
		destGetter:    &mockDestinationGetter{dest: &destination},
		alertMonitor:  newMockAlertMonitor(),
		retryMaxCount: 2, // 1 initial + 2 retries = 3 total attempts
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, deliveryEvent))

	<-ctx.Done()
	assert.Equal(t, 3, publisher.Current(), "should stop after max retries (1 initial + 2 retries = 3 total attempts)")
}
