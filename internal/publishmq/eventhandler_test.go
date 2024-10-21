package publishmq_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/publishmq"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// NOTE: This test seems to be a bit flaky.
func TestPublishMQEventHandler_Concurrency(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	mockEventTracer := testutil.NewMockEventTracer(exporter)

	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient, models.NewAESCipher("secret"))
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{
		InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)},
	}))
	cleanup, err := deliveryMQ.Init(ctx)
	require.NoError(t, err)
	defer cleanup()
	eventHandler := publishmq.NewEventHandler(logger,
		testutil.CreateTestRedisClient(t),
		deliveryMQ,
		entityStore,
		mockEventTracer,
	)

	tenant := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	entityStore.UpsertTenant(ctx, tenant)
	destFactory := testutil.DestinationFactory
	for i := 0; i < 5; i++ {
		entityStore.UpsertDestination(ctx, destFactory.Any(destFactory.WithTenantID(tenant.ID)))
	}

	err = eventHandler.Handle(ctx, &models.Event{
		ID:       uuid.New().String(),
		Topic:    "mytopic",
		TenantID: tenant.ID,
		Metadata: map[string]string{},
		Data: map[string]interface{}{
			"mykey": "myvalue",
		},
	})
	require.Nil(t, err)

	spans := exporter.GetSpans()
	var startDeliverySpans tracetest.SpanStubs
	for _, span := range spans {
		if span.Name != "StartDelivery" {
			continue
		}
		log.Println(span.StartTime, "|", span.EndTime)
		startDeliverySpans = append(startDeliverySpans, span)
	}
	require.Len(t, startDeliverySpans, 5)
	currentSpan := startDeliverySpans[0]
	for index, span := range startDeliverySpans {
		if index == 0 {
			continue
		}
		require.Less(t, span.StartTime, currentSpan.EndTime, "events are not delivered concurrently")
		currentSpan = span
	}
}
