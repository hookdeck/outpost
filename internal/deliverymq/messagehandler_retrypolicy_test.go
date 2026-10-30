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
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failureRun struct {
	msg       *mockMessage
	scheduler *mockRetryScheduler
	logs      *mockLogPublisher
}

// runFailedDelivery handles one delivery of an eligible event of destType
// whose publish fails with pubErr.
func runFailedDelivery(t *testing.T, destType string, attemptNo int, manual bool, pubErr error, opts ...deliverymq.MessageHandlerOption) failureRun {
	t.Helper()
	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType(destType),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)
	retryScheduler := newMockRetryScheduler()
	logs := newMockLogPublisher(nil)
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		logs,
		&mockDestinationGetter{dest: &destination},
		newMockPublisher([]error{pubErr}),
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: time.Second},
		10,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(24*time.Hour)),
		opts...,
	)
	mockMsg, msg := newDeliveryMockMessage(models.DeliveryTask{
		Event:         event,
		DestinationID: destination.ID,
		Attempt:       attemptNo,
		Manual:        manual,
	})
	require.NoError(t, handler.Handle(context.Background(), msg))
	return failureRun{msg: mockMsg, scheduler: retryScheduler, logs: logs}
}

func publishAttemptErr(nonRetryable, skipCount bool) error {
	return &destregistry.ErrDestinationPublishAttempt{
		Err:              errors.New("receiver returned an error"),
		Provider:         "webhook",
		Data:             map[string]interface{}{"error": "publish_failed"},
		NonRetryable:     nonRetryable,
		SkipFailureCount: skipCount,
	}
}

func TestMessageHandler_NonRetryableError_NotScheduled(t *testing.T) {
	t.Parallel()
	run := runFailedDelivery(t, "webhook", 1, false, publishAttemptErr(true, false))

	assert.True(t, run.msg.acked)
	assert.False(t, run.msg.nacked)
	assert.Empty(t, run.scheduler.schedules)
	require.Len(t, run.logs.entries, 1)
	assert.Equal(t, models.AttemptStatusFailed, run.logs.entries[0].Attempt.Status)
	assert.True(t, run.logs.entries[0].NonRetryable)
	assert.False(t, run.logs.entries[0].SkipFailureCount)
}

func TestMessageHandler_RetryableError_FlagsUnset(t *testing.T) {
	t.Parallel()
	run := runFailedDelivery(t, "webhook", 1, false, publishAttemptErr(false, false))

	assert.Len(t, run.scheduler.schedules, 1)
	require.Len(t, run.logs.entries, 1)
	assert.Equal(t, 11, run.logs.entries[0].AttemptMax)
	assert.False(t, run.logs.entries[0].NonRetryable)
	assert.False(t, run.logs.entries[0].SkipFailureCount)
}

func TestMessageHandler_NonRetryableError_ManualCancelsPendingRetry(t *testing.T) {
	t.Parallel()
	run := runFailedDelivery(t, "webhook", 2, true, publishAttemptErr(true, false))

	assert.True(t, run.msg.acked)
	assert.Empty(t, run.scheduler.schedules)
	assert.Len(t, run.scheduler.canceled, 1)
}

func TestMessageHandler_SkipFailureCount_IndependentOfRetry(t *testing.T) {
	t.Parallel()
	run := runFailedDelivery(t, "webhook", 1, false, publishAttemptErr(false, true))

	assert.Len(t, run.scheduler.schedules, 1, "retry still scheduled")
	require.Len(t, run.logs.entries, 1)
	assert.True(t, run.logs.entries[0].SkipFailureCount)
	assert.False(t, run.logs.entries[0].NonRetryable)
}

type fakeResolver map[string]struct {
	b          backoff.Backoff
	maxRetries int
}

func (r fakeResolver) RetryPolicy(destType string) (backoff.Backoff, int, bool) {
	p, ok := r[destType]
	return p.b, p.maxRetries, ok
}

func TestMessageHandler_TypeRetryPolicy(t *testing.T) {
	t.Parallel()
	resolver := fakeResolver{"webhook": {&backoff.ConstantBackoff{Interval: 7 * time.Second}, 2}}
	withResolver := deliverymq.WithRetryPolicyResolver(resolver)

	t.Run("own backoff within its limit", func(t *testing.T) {
		t.Parallel()
		run := runFailedDelivery(t, "webhook", 1, false, publishAttemptErr(false, false), withResolver)
		require.Len(t, run.scheduler.entries, 1)
		for _, e := range run.scheduler.entries {
			assert.Equal(t, 7*time.Second, e.delay)
		}
		assert.Equal(t, 3, run.logs.entries[0].AttemptMax)
	})
	t.Run("no retry past its limit", func(t *testing.T) {
		t.Parallel()
		run := runFailedDelivery(t, "webhook", 3, false, publishAttemptErr(false, false), withResolver)
		assert.Empty(t, run.scheduler.schedules)
		assert.Equal(t, 3, run.logs.entries[0].AttemptMax)
	})
	t.Run("other type keeps global policy", func(t *testing.T) {
		t.Parallel()
		run := runFailedDelivery(t, "rabbitmq", 3, false, publishAttemptErr(false, false), withResolver)
		require.Len(t, run.scheduler.entries, 1)
		for _, e := range run.scheduler.entries {
			assert.Equal(t, time.Second, e.delay)
		}
		assert.Equal(t, 11, run.logs.entries[0].AttemptMax)
	})
	t.Run("not ok falls back to global", func(t *testing.T) {
		t.Parallel()
		run := runFailedDelivery(t, "webhook", 3, false, publishAttemptErr(false, false),
			deliverymq.WithRetryPolicyResolver(fakeResolver{}))
		assert.Len(t, run.scheduler.schedules, 1)
		assert.Equal(t, 11, run.logs.entries[0].AttemptMax)
	})
}
