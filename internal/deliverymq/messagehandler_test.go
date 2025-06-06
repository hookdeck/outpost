package deliverymq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMessageHandler_DestinationGetterError(t *testing.T) {
	// Test scenario:
	// - Event is NOT eligible for retry
	// - Destination lookup fails with error (system error in destination getter)
	// - Should be nacked (let system retry)
	// - Should NOT use retry scheduler
	// - Should NOT call alert monitor (no destination)
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
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
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

	// Wait a bit for any goroutines
	time.Sleep(50 * time.Millisecond)

	// Assert behavior
	assert.True(t, mockMsg.nacked, "message should be nacked on system error")
	assert.False(t, mockMsg.acked, "message should not be acked on system error")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled for system error")
	alertMonitor.AssertNotCalled(t, "HandleAttempt", mock.Anything, mock.Anything)
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
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		logPublisher,
		destGetter,
		eventGetter,
		newMockPublisher(nil), // won't be called
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
		alertMonitor,
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
	assert.Empty(t, logPublisher.deliveries, "should not log delivery for pre-delivery error")
	alertMonitor.AssertNotCalled(t, "HandleAttempt", mock.Anything, mock.Anything)
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
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

	// Setup message handler
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		testutil.CreateTestRedisClient(t),
		logPublisher,
		destGetter,
		eventGetter,
		newMockPublisher(nil), // won't be called
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
		alertMonitor,
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
	require.NoError(t, err)

	// Assert behavior
	assert.False(t, mockMsg.nacked, "message should not be nacked when destination is deleted")
	assert.True(t, mockMsg.acked, "message should be acked when destination is deleted")
	assert.Empty(t, retryScheduler.schedules, "no retry should be scheduled")
	assert.Empty(t, logPublisher.deliveries, "should not log delivery for pre-delivery error")
	alertMonitor.AssertNotCalled(t, "HandleAttempt", mock.Anything, mock.Anything)
}

func TestMessageHandler_PublishError_EligibleForRetry(t *testing.T) {
	// Test scenario:
	// - Publish fails with a publish error
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
	publishErr := &destregistry.ErrDestinationPublishAttempt{
		Err:      errors.New("webhook returned 429"),
		Provider: "webhook",
		Data: map[string]interface{}{
			"error":   "publish_failed",
			"message": "webhook returned 429",
		},
	}
	publisher := newMockPublisher([]error{publishErr})
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
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
	assert.Equal(t, deliveryEvent.GetRetryID(), retryScheduler.taskIDs[0],
		"should use GetRetryID for task ID")
	require.Len(t, logPublisher.deliveries, 1, "should have one delivery")
	assert.Equal(t, models.DeliveryStatusFailed, logPublisher.deliveries[0].Delivery.Status, "delivery status should be Failed")
	assertAlertMonitor(t, alertMonitor, false, &destination, publishErr.Data)
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
	publishErr := &destregistry.ErrDestinationPublishAttempt{
		Err:      errors.New("webhook returned 400"),
		Provider: "webhook",
		Data: map[string]interface{}{
			"error":   "publish_failed",
			"message": "webhook returned 429",
		},
	}
	publisher := newMockPublisher([]error{publishErr})
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
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
	require.Len(t, logPublisher.deliveries, 1, "should have one delivery")
	assert.Equal(t, models.DeliveryStatusFailed, logPublisher.deliveries[0].Delivery.Status, "delivery status should be Failed")
	assertAlertMonitor(t, alertMonitor, false, &destination, publishErr.Data)
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
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
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
	logPublisher := newMockLogPublisher(nil)

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
		newMockAlertMonitor(),
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
	require.Len(t, logPublisher.deliveries, 1, "should have one delivery")
	assert.Equal(t, models.DeliveryStatusSuccess, logPublisher.deliveries[0].Delivery.Status, "delivery status should be OK")
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
		newMockAlertMonitor(),
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
		newMockAlertMonitor(),
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
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
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
	require.NoError(t, err)

	// Assert behavior
	assert.True(t, mockMsg.acked, "message should be acked for disabled destination")
	assert.False(t, mockMsg.nacked, "message should not be nacked for disabled destination")
	assert.Equal(t, 0, publisher.current, "should not attempt to publish to disabled destination")
	assert.Empty(t, retryScheduler.schedules, "should not schedule retry")
	assert.Empty(t, retryScheduler.canceled, "should not attempt to cancel retries")
	assert.Empty(t, logPublisher.deliveries, "should not log delivery for pre-delivery error")
	alertMonitor.AssertNotCalled(t, "HandleAttempt", mock.Anything, mock.Anything)
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
		newMockAlertMonitor(),
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
		newMockAlertMonitor(),
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

func TestManualDelivery_Success(t *testing.T) {
	// Test scenario:
	// - Manual delivery succeeds
	// - Should cancel any pending retries
	// - Should be acked
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
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil}) // successful publish
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
		Manual:        true, // Manual delivery
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.NoError(t, err)

	// Assert behavior
	assert.True(t, mockMsg.acked, "message should be acked")
	assert.False(t, mockMsg.nacked, "message should not be nacked")
	assert.Equal(t, 1, publisher.current, "should publish once")
	assert.Len(t, retryScheduler.canceled, 1, "should cancel pending retries")
	assert.Equal(t, deliveryEvent.GetRetryID(), retryScheduler.canceled[0], "should cancel with correct retry ID")
	require.Len(t, logPublisher.deliveries, 1, "should have one delivery")
	assert.Equal(t, models.DeliveryStatusSuccess, logPublisher.deliveries[0].Delivery.Status, "delivery status should be OK")
	assertAlertMonitor(t, alertMonitor, true, &destination, nil)
}

func TestManualDelivery_PublishError(t *testing.T) {
	// Test scenario:
	// - Manual delivery fails with publish error
	// - Should not schedule retry (manual delivery never retries)
	// - Should be acked
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
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publishErr := &destregistry.ErrDestinationPublishAttempt{
		Err:      errors.New("webhook returned 429"),
		Provider: "webhook",
		Data: map[string]interface{}{
			"error":   "publish_failed",
			"message": "webhook returned 429",
		},
	}
	publisher := newMockPublisher([]error{publishErr})
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
		Manual:        true, // Manual delivery
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)

	// Assert behavior
	assert.True(t, mockMsg.acked, "message should be acked")
	assert.False(t, mockMsg.nacked, "message should not be nacked")
	assert.Equal(t, 1, publisher.current, "should attempt publish once")
	assert.Empty(t, retryScheduler.schedules, "should not schedule retry for manual delivery")
	require.Len(t, logPublisher.deliveries, 1, "should have one delivery")
	assert.Equal(t, models.DeliveryStatusFailed, logPublisher.deliveries[0].Delivery.Status, "delivery status should be Failed")
	assertAlertMonitor(t, alertMonitor, false, &destination, publishErr.Data)
}

func TestManualDelivery_CancelError(t *testing.T) {
	// Test scenario:
	// - Manual delivery succeeds but retry cancellation fails
	// - Should be treated as post-delivery error
	// - Should nack for retry
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
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
	retryScheduler.cancelResp = []error{errors.New("failed to cancel retry")}
	publisher := newMockPublisher([]error{nil}) // successful publish
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
		Manual:        true, // Manual delivery
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to cancel retry")

	// Assert behavior
	assert.True(t, mockMsg.nacked, "message should be nacked on retry cancel error")
	assert.False(t, mockMsg.acked, "message should not be acked on retry cancel error")
	assert.Equal(t, 1, publisher.current, "should publish once")
	assert.Len(t, retryScheduler.canceled, 1, "should attempt to cancel retry")
	assert.Equal(t, deliveryEvent.GetRetryID(), retryScheduler.canceled[0], "should cancel with correct retry ID")
	require.Len(t, logPublisher.deliveries, 1, "should have one delivery")
	assert.Equal(t, models.DeliveryStatusSuccess, logPublisher.deliveries[0].Delivery.Status, "delivery status should be OK despite cancel error")
	assertAlertMonitor(t, alertMonitor, true, &destination, nil)
}

func TestManualDelivery_DestinationDisabled(t *testing.T) {
	// Test scenario:
	// - Manual delivery to disabled destination
	// - Should be treated as pre-delivery error
	// - Should ack without attempting publish or retry cancellation
	t.Parallel()

	// Setup test data
	tenant := models.Tenant{ID: uuid.New().String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
		testutil.DestinationFactory.WithDisabledAt(time.Now()),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true), // even with retry enabled
	)

	// Setup mocks
	destGetter := &mockDestinationGetter{dest: &destination}
	eventGetter := newMockEventGetter()
	eventGetter.registerEvent(&event)
	retryScheduler := newMockRetryScheduler()
	publisher := newMockPublisher([]error{nil}) // won't be called
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()

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
		alertMonitor,
	)

	// Create and handle message
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: destination.ID,
		Manual:        true, // Manual delivery
	}
	mockMsg, msg := newDeliveryMockMessage(deliveryEvent)

	// Handle message
	err := handler.Handle(context.Background(), msg)
	require.NoError(t, err)

	// Assert behavior
	assert.True(t, mockMsg.acked, "message should be acked for disabled destination")
	assert.False(t, mockMsg.nacked, "message should not be nacked for disabled destination")
	assert.Equal(t, 0, publisher.current, "should not attempt to publish to disabled destination")
	assert.Empty(t, retryScheduler.schedules, "should not schedule retry")
	assert.Empty(t, retryScheduler.canceled, "should not attempt to cancel retries")
	assert.Empty(t, logPublisher.deliveries, "should not log delivery for pre-delivery error")
	alertMonitor.AssertNotCalled(t, "HandleAttempt", mock.Anything, mock.Anything)
}

func TestMessageHandler_PublishSuccess(t *testing.T) {
	// Test scenario:
	// - Publish succeeds
	// - Should call alert monitor with successful attempt
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
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()
	alertMonitor.ExpectedCalls = nil // Clear default expectations

	// Setup alert monitor expectations
	alertMonitor.On("HandleAttempt", mock.Anything, mock.MatchedBy(func(attempt alert.DeliveryAttempt) bool {
		return attempt.Success && // Should be a successful attempt
			attempt.Destination.ID == destination.ID && // Should have correct destination
			attempt.DeliveryEvent != nil && // Should have delivery event
			attempt.DeliveryResponse == nil // No error data for success
	})).Return(nil)

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
		alertMonitor,
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
	require.NoError(t, err)

	// Assert behavior
	assert.True(t, mockMsg.acked, "message should be acked on success")
	assert.False(t, mockMsg.nacked, "message should not be nacked on success")
	assertAlertMonitor(t, alertMonitor, true, &destination, nil)
}

func TestMessageHandler_AlertMonitorError(t *testing.T) {
	// Test scenario:
	// - Publish succeeds
	// - Alert monitor fails
	// - Should still succeed overall (alert errors don't affect main flow)
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
	logPublisher := newMockLogPublisher(nil)
	alertMonitor := newMockAlertMonitor()
	alertMonitor.On("HandleAttempt", mock.Anything, mock.Anything).Return(errors.New("alert monitor failed"))

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
		alertMonitor,
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
	require.NoError(t, err)

	// Assert behavior
	assert.True(t, mockMsg.acked, "message should be acked despite alert monitor error")
	assert.False(t, mockMsg.nacked, "message should not be nacked despite alert monitor error")
	assert.Equal(t, 1, publisher.current, "should publish once")
	require.Len(t, logPublisher.deliveries, 1, "should have one delivery")
	assert.Equal(t, models.DeliveryStatusSuccess, logPublisher.deliveries[0].Delivery.Status, "delivery status should be OK")

	// Verify alert monitor was called but error was ignored
	// Wait for the HandleAttempt call to be made
	require.Eventually(t, func() bool {
		for _, call := range alertMonitor.Calls {
			if call.Method == "HandleAttempt" {
				return true
			}
		}
		return false
	}, 200*time.Millisecond, 10*time.Millisecond, "timed out waiting for HandleAttempt call on alertMonitor")
	alertMonitor.AssertCalled(t, "HandleAttempt", mock.Anything, mock.Anything)
}

// Helper function to assert alert monitor calls
func assertAlertMonitor(t *testing.T, m *mockAlertMonitor, success bool, destination *models.Destination, expectedData map[string]interface{}) {
	t.Helper()

	// Wait for the alert monitor to be called
	require.Eventually(t, func() bool {
		return len(m.Calls) > 0
	}, 200*time.Millisecond, 10*time.Millisecond, "timed out waiting for alert monitor to be called")

	calls := m.Calls // m.Calls is now guaranteed to be non-empty

	lastCall := calls[len(calls)-1]
	attempt := lastCall.Arguments[1].(alert.DeliveryAttempt)

	assert.Equal(t, success, attempt.Success, "alert attempt success should match")
	assert.Equal(t, destination.ID, attempt.Destination.ID, "alert attempt destination should match")
	assert.NotNil(t, attempt.DeliveryEvent, "alert attempt should have delivery event")

	if expectedData != nil {
		assert.Equal(t, expectedData, attempt.DeliveryResponse, "alert attempt data should match")
	} else {
		assert.Nil(t, attempt.DeliveryResponse, "alert attempt should not have data")
	}
}
