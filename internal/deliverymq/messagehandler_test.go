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
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageHandler_DestinationGetterError(t *testing.T) {
	// Test scenario:
	// - Event is NOT eligible for retry
	// - Destination lookup fails with error (system error in destination getter)
	// - Should be nacked (let system retry)
	// - Should NOT use retry scheduler
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(false), // not eligible for retry
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{err: errors.New("destination lookup failed")}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		newMockPublisher(nil), // won't be called due to early error
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)

	// Assert behavior
	assert.True(t, mockMsg.nacked, "message should be nacked on system error")
	assert.False(t, mockMsg.acked, "message should not be acked on system error")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled for system error")
}

func TestMessageHandler_DestinationNotFound(t *testing.T) {
	// Test scenario:
	// - Destination lookup returns nil, nil (not found)
	// - Should return error
	// - Message should be nacked (no retry)
	// - No retry should be scheduled
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true), // even with retry enabled
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: nil, err: nil} // destination not found
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		newMockPublisher(nil), // won't be called
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)

	// Assert behavior
	assert.True(t, mockMsg.nacked, "message should be nacked when destination not found")
	assert.False(t, mockMsg.acked, "message should not be acked when destination not found")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled")
}

func TestMessageHandler_DestinationDeleted(t *testing.T) {
	// Test scenario:
	// - Destination lookup returns ErrDestinationDeleted
	// - Should return error but ack message (no retry needed)
	// - No retry should be scheduled
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true), // even with retry enabled
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{err: models.ErrDestinationDeleted}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		newMockPublisher(nil), // won't be called
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.ErrorIs(t, err, models.ErrDestinationDeleted)

	// Assert behavior
	assert.False(t, mockMsg.nacked, "message should not be nacked when destination is deleted")
	assert.True(t, mockMsg.acked, "message should be acked when destination is deleted")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled")
}

func TestMessageHandler_PublishError_EligibleForRetry(t *testing.T) {
	// Test scenario:
	// - Publish returns ErrDestinationPublishAttempt
	// - Event is eligible for retry and under max attempts
	// - Should schedule retry and ack
	t.Parallel()

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

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 429"),
			Provider: "webhook",
		},
	})

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)

	// Assert behavior
	assert.False(t, mockMsg.nacked, "message should not be nacked when scheduling retry")
	assert.True(t, mockMsg.acked, "message should be acked when scheduling retry")
	assert.Len(t, retryScheduler.schedules, 1, "retry should be scheduled")
}

func TestMessageHandler_PublishError_NotEligible(t *testing.T) {
	// Test scenario:
	// - Publish returns ErrDestinationPublishAttempt
	// - Event is NOT eligible for retry
	// - Should ack (no retry, no nack)
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(false),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{
		&destregistry.ErrDestinationPublishAttempt{
			Err:      errors.New("webhook returned 400"),
			Provider: "webhook",
		},
	})

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)

	// Assert behavior
	assert.False(t, mockMsg.nacked, "message should not be nacked for ineligible retry")
	assert.True(t, mockMsg.acked, "message should be acked for ineligible retry")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled")
	assert.Equal(t, 1, publisher.current, "should only attempt once")
}

func TestMessageHandler_EventGetterError(t *testing.T) {
	// Test scenario:
	// - Event getter fails to retrieve event during retry
	// - Should be treated as system error
	// - Should nack for retry
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.err = errors.New("failed to get event")
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil})

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message simulating a retry
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Attempt:       2, // Retry attempt
		DestinationID: destination.ID,
		Event: models.Event{
			ID:       event.ID,
			TenantID: event.TenantID,
			// Minimal event data as it would be in a retry
		},
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get event")

	// Assert behavior
	assert.True(t, mockMsg.nacked, "message should be nacked on event getter error")
	assert.False(t, mockMsg.acked, "message should not be acked on event getter error")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled for system error")
	assert.Equal(t, 0, publisher.current, "publish should not be attempted")
}

func TestMessageHandler_RetryFlow(t *testing.T) {
	// Test scenario:
	// - Message is a retry attempt (Attempt > 1)
	// - Event getter successfully retrieves full event data
	// - Message is processed normally
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil}) // Successful publish

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message simulating a retry
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Attempt:       2, // Retry attempt
		DestinationID: destination.ID,
		Event: models.Event{
			ID:       event.ID,
			TenantID: event.TenantID,
			// Minimal event data as it would be in a retry
		},
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.NoError(t, err)

	// Assert behavior
	assert.True(t, mockMsg.acked, "message should be acked on successful retry")
	assert.False(t, mockMsg.nacked, "message should not be nacked on successful retry")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled")
	assert.Equal(t, 1, publisher.current, "publish should succeed once")
	assert.Equal(t, event.ID, eventGetter.lastRetrievedID, "event getter should be called with correct ID")
}

func TestMessageHandler_Idempotency(t *testing.T) {
	// Test scenario:
	// - Message with same ID is processed twice
	// - Second attempt should be idempotent
	// - Should ack without publishing
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil})
	logPublisher := newMockLogPublisher(nil)

	// Setup message handler with Redis for idempotency
	redis := testutil.CreateTestRedisClient(t)
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		redis,
		logPublisher,
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create message with fixed ID for idempotency check
	messageID := uuid.New().String()
	deliveryEvent := models.DeliveryEvent{
		ID:            messageID,
		Event:         event,
		DestinationID: destination.ID,
	}

	// First attempt
	mockMsg1, msg1 := newDeliveryMockMessage(deliveryEvent)
	err := handler.Handle(context.Background(), msg1)
	require.NoError(t, err)
	assert.True(t, mockMsg1.acked, "first attempt should be acked")
	assert.False(t, mockMsg1.nacked, "first attempt should not be nacked")
	assert.Equal(t, 1, publisher.current, "first attempt should publish")

	// Second attempt with same message ID
	mockMsg2, msg2 := newDeliveryMockMessage(deliveryEvent)
	err = handler.Handle(context.Background(), msg2)
	require.NoError(t, err)
	assert.True(t, mockMsg2.acked, "duplicate should be acked")
	assert.False(t, mockMsg2.nacked, "duplicate should not be nacked")
	assert.Equal(t, 1, publisher.current, "duplicate should not publish again")
}

func TestMessageHandler_IdempotencyWithSystemError(t *testing.T) {
	// Test scenario:
	// - First attempt fails with system error (event getter error)
	// - Second attempt with same message ID succeeds after error is cleared
	// - Should demonstrate that system errors don't affect idempotency
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	eventGetter.err = errors.New("failed to get event") // Will fail first attempt
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil})
	logPublisher := newMockLogPublisher(nil)

	// Setup message handler with Redis for idempotency
	redis := testutil.CreateTestRedisClient(t)
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		redis,
		logPublisher,
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create retry message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Attempt:       2,
		DestinationID: destination.ID,
		Event: models.Event{
			ID:       event.ID,
			TenantID: event.TenantID,
		},
	}

	// First attempt - should fail with system error
	mockMsg1, msg1 := newDeliveryMockMessage(deliveryEvent)
	err := handler.Handle(context.Background(), msg1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get event")
	assert.True(t, mockMsg1.nacked, "first attempt should be nacked")
	assert.False(t, mockMsg1.acked, "first attempt should not be acked")
	assert.Equal(t, 0, publisher.current, "publish should not be attempted")

	// Clear the error for second attempt
	eventGetter.clearError()

	// Second attempt with same message ID - should succeed
	mockMsg2, msg2 := newDeliveryMockMessage(deliveryEvent)
	err = handler.Handle(context.Background(), msg2)
	require.NoError(t, err)
	assert.True(t, mockMsg2.acked, "second attempt should be acked")
	assert.False(t, mockMsg2.nacked, "second attempt should not be nacked")
	assert.Equal(t, 1, publisher.current, "publish should succeed once")
	assert.Equal(t, event.ID, eventGetter.lastRetrievedID, "event getter should be called with correct ID")
}

func TestMessageHandler_DestinationDisabled(t *testing.T) {
	// Test scenario:
	// - Destination is disabled
	// - Should be treated as a destination error (not system error)
	// - Should ack without retry or publish attempt
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
		testutil.DestinationFactory.WithDisabledAt(time.Now()),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(false),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil}) // won't be called

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		newMockLogPublisher(nil),
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)

	// Assert behavior
	assert.False(t, mockMsg.nacked, "message should not be nacked for disabled destination")
	assert.True(t, mockMsg.acked, "message should be acked for disabled destination")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled")
	assert.Equal(t, 0, publisher.current, "should not attempt to publish to disabled destination")
}

func TestMessageHandler_LogPublisherError(t *testing.T) {
	// Test scenario:
	// - Publish succeeds but log publisher fails
	// - Should be treated as system error
	// - Should nack for retry
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil}) // publish succeeds
	logPublisher := newMockLogPublisher(errors.New("log publish failed"))

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		logPublisher,
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "log publish failed")

	// Assert behavior
	assert.True(t, mockMsg.nacked, "message should be nacked on log publisher error")
	assert.False(t, mockMsg.acked, "message should not be acked on log publisher error")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled for system error")
	assert.Equal(t, 1, publisher.current, "publish should succeed once")
}

func TestMessageHandler_PublishAndLogError(t *testing.T) {
	// Test scenario:
	// - Both publish and log publisher fail
	// - Should join both errors
	// - Should be treated as system error
	// - Should nack for retry
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{errors.New("publish failed")})
	logPublisher := newMockLogPublisher(errors.New("log publish failed"))

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		logPublisher,
		destGetter,
		eventGetter,
		publisher,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish failed")
	assert.Contains(t, err.Error(), "log publish failed")

	// Assert behavior
	assert.True(t, mockMsg.nacked, "message should be nacked on system error")
	assert.False(t, mockMsg.acked, "message should not be acked on system error")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled for system error")
	assert.Equal(t, 1, publisher.current, "publish should be attempted once")
}
