package deliverymq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/deliverymq"
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
