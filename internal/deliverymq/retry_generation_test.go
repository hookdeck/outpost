package deliverymq_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The generation stamped on a scheduled retry survives the real scheduler and
// the logstore round trip: a retry for a destination that was deleted and
// recreated under the same ID is dropped, while a retry for the same
// destination is delivered.
func TestDeliveryMQRetry_GenerationCheckedRetry(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name      string
		recreated bool
	}{
		{"same destination: retried", false},
		{"recreated under the same ID: dropped", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			dest := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithID("sub_0123456789abcdef0123456789abcdef"),
				testutil.DestinationFactory.WithType(mcpType),
				testutil.DestinationFactory.WithCreatedAt(created),
			)
			getter := &sequenceDestinationGetter{dests: []*models.Destination{&dest}}
			if tc.recreated {
				// The first read (first attempt) sees the original; every later
				// read (the retry) sees its replacement.
				replacement := dest
				replacement.CreatedAt = created.Add(5 * time.Minute)
				getter.set(&dest, &replacement)
			}

			publisher := newMockPublisher([]error{failure(false)})
			eventGetter := newMockEventGetter()
			logPublisher := newMockLogPublisher(nil)
			logPublisher.eventGetter = eventGetter

			suite := &RetryDeliveryMQSuite{
				ctx:                  ctx,
				mqConfig:             &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}},
				publisher:            publisher,
				eventGetter:          eventGetter,
				logPublisher:         logPublisher,
				destGetter:           getter,
				retryMaxCount:        10,
				retryBackoff:         &backoff.ConstantBackoff{Interval: time.Hour}, // default policy unused
				schedulerPollBackoff: 10 * time.Millisecond,
				handlerOpts: []deliverymq.MessageHandlerOption{
					deliverymq.WithGenerationCheckedTypes(mcpType),
					deliverymq.WithRetryPolicy(mcpType, &backoff.ConstantBackoff{Interval: 10 * time.Millisecond}, 3),
				},
			}
			suite.SetupTest(t)
			defer suite.TeardownTest(t)

			require.NoError(t, suite.deliveryMQ.Publish(ctx, models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)))

			if !tc.recreated {
				require.Eventually(t, func() bool { return publisher.Current() >= 2 }, 5*time.Second, 10*time.Millisecond,
					"the retry reaches the same destination")
				return
			}
			require.Eventually(t, func() bool { return getter.callCount() >= 2 }, 5*time.Second, 10*time.Millisecond,
				"the retry executes")
			assert.Never(t, func() bool { return publisher.Current() > 1 }, 300*time.Millisecond, 10*time.Millisecond,
				"the retry never reaches the recreated destination")
		})
	}
}
