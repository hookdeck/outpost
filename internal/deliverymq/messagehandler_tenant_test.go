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
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sharedEventID = "evt_shared"
	sharedDestID  = "des_shared"
)

type tenantFixture struct {
	handler        func(task models.DeliveryTask) *mockMessage
	publisher      *mockPublisher
	retryScheduler *mockRetryScheduler
	logPublisher   *mockLogPublisher
	task           func(tenantID string, attempt int, manual bool) models.DeliveryTask
}

// newTenantFixture sets up two tenants that each own a destination with the
// same ID and receive an event with the same ID.
func newTenantFixture(t *testing.T, publishResponses []error) *tenantFixture {
	t.Helper()
	ctx := context.Background()

	store := tenantstore.NewMemTenantStore()
	for _, tenantID := range []string{"t1", "t2"} {
		require.NoError(t, store.UpsertTenant(ctx, testutil.TenantFactory.Any(testutil.TenantFactory.WithID(tenantID))))
		require.NoError(t, store.UpsertDestination(ctx, testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(sharedDestID),
			testutil.DestinationFactory.WithTenantID(tenantID),
		)))
	}

	f := &tenantFixture{
		publisher:      newMockPublisher(publishResponses),
		retryScheduler: newMockRetryScheduler(),
		logPublisher:   newMockLogPublisher(nil),
	}
	h := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		f.logPublisher,
		store,
		f.publisher,
		testutil.NewMockEventTracer(nil),
		f.retryScheduler,
		&backoff.ConstantBackoff{Interval: time.Second},
		5,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(24*time.Hour)),
	)
	f.handler = func(task models.DeliveryTask) *mockMessage {
		mockMsg, msg := newDeliveryMockMessage(task)
		require.NoError(t, h.Handle(ctx, msg))
		return mockMsg
	}
	f.task = func(tenantID string, attempt int, manual bool) models.DeliveryTask {
		return models.DeliveryTask{
			Event: testutil.EventFactory.Any(
				testutil.EventFactory.WithID(sharedEventID),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithDestinationID(sharedDestID),
				testutil.EventFactory.WithEligibleForRetry(true),
			),
			DestinationID: sharedDestID,
			Attempt:       attempt,
			Manual:        manual,
		}
	}
	return f
}

func publishFailure() error {
	return &destregistry.ErrDestinationPublishAttempt{
		Err:      errors.New("webhook returned 500"),
		Provider: "webhook",
		Data:     map[string]interface{}{"error": "publish_failed"},
	}
}

func TestMessageHandler_SameEventAndDestinationIDAcrossTenants_DeliveredSeparately(t *testing.T) {
	t.Parallel()
	f := newTenantFixture(t, nil)

	m1 := f.handler(f.task("t1", 1, false))
	m2 := f.handler(f.task("t2", 1, false))

	assert.True(t, m1.acked)
	assert.True(t, m2.acked)
	assert.Equal(t, 2, f.publisher.Current(), "each tenant gets its own delivery attempt")
	require.Len(t, f.logPublisher.entries, 2)
	assert.ElementsMatch(t, []string{"t1", "t2"}, []string{
		f.logPublisher.entries[0].Event.TenantID,
		f.logPublisher.entries[1].Event.TenantID,
	})

	f.handler(f.task("t1", 1, false))
	assert.Equal(t, 2, f.publisher.Current(), "the same task redelivered for one tenant is still deduplicated")
}

func TestMessageHandler_SameEventAndDestinationIDAcrossTenants_RetriesScheduledSeparately(t *testing.T) {
	t.Parallel()
	f := newTenantFixture(t, []error{publishFailure(), publishFailure()})

	f.handler(f.task("t1", 1, false))
	f.handler(f.task("t2", 1, false))

	id1 := models.RetryID("t1", sharedEventID, sharedDestID)
	id2 := models.RetryID("t2", sharedEventID, sharedDestID)
	require.NotEqual(t, id1, id2)
	assert.Len(t, f.retryScheduler.entries, 2, "t2's retry must not replace t1's")
	assert.Contains(t, f.retryScheduler.entries, id1)
	assert.Contains(t, f.retryScheduler.entries, id2)
}

func TestMessageHandler_SameEventAndDestinationIDAcrossTenants_ManualRetryLeavesOtherTenantAlone(t *testing.T) {
	t.Parallel()

	id1 := models.RetryID("t1", sharedEventID, sharedDestID)
	id2 := models.RetryID("t2", sharedEventID, sharedDestID)

	t.Run("successful manual retry cancels only its own tenant's retry", func(t *testing.T) {
		t.Parallel()
		f := newTenantFixture(t, []error{publishFailure(), publishFailure()})
		f.handler(f.task("t1", 1, false))
		f.handler(f.task("t2", 1, false))
		require.Len(t, f.retryScheduler.entries, 2)

		m := f.handler(f.task("t1", 2, true))

		assert.True(t, m.acked)
		assert.Equal(t, []string{id1}, f.retryScheduler.canceled)
		assert.NotContains(t, f.retryScheduler.entries, id1)
		assert.Contains(t, f.retryScheduler.entries, id2)
	})

	t.Run("failed manual retry replaces only its own tenant's retry", func(t *testing.T) {
		t.Parallel()
		f := newTenantFixture(t, []error{publishFailure(), publishFailure(), publishFailure()})
		f.handler(f.task("t1", 1, false))
		f.handler(f.task("t2", 1, false))
		before2 := f.retryScheduler.entries[id2]

		f.handler(f.task("t1", 2, true))

		require.Len(t, f.retryScheduler.entries, 2)
		assert.Equal(t, before2, f.retryScheduler.entries[id2], "t2's pending retry is untouched")
		for _, canceled := range f.retryScheduler.canceled {
			assert.NotEqual(t, id2, canceled)
		}
	})
}
