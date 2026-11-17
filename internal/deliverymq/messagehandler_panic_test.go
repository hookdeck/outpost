package deliverymq_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type panickingPublisher struct{}

func (panickingPublisher) Publish(context.Context, *models.Event) (*destregistry.Delivery, error) {
	panic("publisher blew up")
}

func (panickingPublisher) Close() error { return nil }

// panickingProvider implements only what the registry calls on a delivery.
type panickingProvider struct {
	destregistry.Provider
}

func (panickingProvider) Metadata() *metadata.ProviderMetadata { return &metadata.ProviderMetadata{} }

func (panickingProvider) CreatePublisher(context.Context, *models.Destination) (destregistry.Publisher, error) {
	return panickingPublisher{}, nil
}

func TestMessageHandler_PublisherPanic_NacksMessage(t *testing.T) {
	t.Parallel()

	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("panicking"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, registry.RegisterProvider("panicking", panickingProvider{}))

	retryScheduler := newMockRetryScheduler()
	logPublisher := newMockLogPublisher(nil)
	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		logPublisher,
		&mockDestinationGetter{dest: &destination},
		registry,
		testutil.NewMockEventTracer(nil),
		retryScheduler,
		&backoff.ConstantBackoff{Interval: 1 * time.Second},
		10,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(24*time.Hour)),
	)

	task := models.DeliveryTask{Event: event, DestinationID: destination.ID}
	mockMsg, msg := newDeliveryMockMessage(task)

	require.NotPanics(t, func() {
		require.Error(t, handler.Handle(context.Background(), msg))
	})

	assert.True(t, mockMsg.nacked, "the message is nacked, so the queue redelivers and dead-letters it")
	assert.False(t, mockMsg.acked)
	assert.Empty(t, retryScheduler.schedules, "no retry is scheduled")
	assert.Empty(t, logPublisher.entries, "no attempt is logged")
}
