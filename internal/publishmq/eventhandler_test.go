package publishmq_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIntegrationPublishMQEventHandler_Concurrency(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	exporter := tracetest.NewInMemoryExporter()
	mockEventTracer := testutil.NewMockEventTracer(exporter)

	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient, models.NewAESCipher("secret"), testutil.TestTopics)
	mqConfig := testinfra.NewMQAWSConfig(t, nil)
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqConfig))
	cleanup, err := deliveryMQ.Init(ctx)
	require.NoError(t, err)
	defer cleanup()
	eventHandler := publishmq.NewEventHandler(logger,
		testutil.CreateTestRedisClient(t),
		deliveryMQ,
		entityStore,
		mockEventTracer,
		testutil.TestTopics,
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

	err = eventHandler.Handle(ctx, testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithTenantID(tenant.ID),
	))
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

func TestEventHandler_WildcardTopic(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	exporter := tracetest.NewInMemoryExporter()
	mockEventTracer := testutil.NewMockEventTracer(exporter)

	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient, models.NewAESCipher("secret"), testutil.TestTopics)
	mqConfig := testinfra.NewMQAWSConfig(t, nil)
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqConfig))
	cleanup, err := deliveryMQ.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	// Create a subscription to receive delivery events
	subscription, err := deliveryMQ.Subscribe(ctx)
	require.NoError(t, err)

	eventHandler := publishmq.NewEventHandler(logger,
		testutil.CreateTestRedisClient(t),
		deliveryMQ,
		entityStore,
		mockEventTracer,
		testutil.TestTopics,
	)

	tenant := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	entityStore.UpsertTenant(ctx, tenant)

	// Create destinations with different topics
	destFactory := testutil.DestinationFactory
	destinations := []models.Destination{
		destFactory.Any(
			destFactory.WithTenantID(tenant.ID),
			destFactory.WithTopics([]string{"user.created"}),
		),
		destFactory.Any(
			destFactory.WithTenantID(tenant.ID),
			destFactory.WithTopics([]string{"user.updated"}),
		),
		destFactory.Any(
			destFactory.WithTenantID(tenant.ID),
			destFactory.WithTopics([]string{"user.deleted"}),
		),
	}
	for _, dest := range destinations {
		err := entityStore.UpsertDestination(ctx, dest)
		require.NoError(t, err)
	}

	// Create a disabled destination to verify it's not matched
	disabledDest := destFactory.Any(
		destFactory.WithTenantID(tenant.ID),
		destFactory.WithTopics([]string{"user.created"}),
	)
	now := time.Now()
	disabledDest.DisabledAt = &now
	err = entityStore.UpsertDestination(ctx, disabledDest)
	require.NoError(t, err)

	// Test publishing with wildcard topic
	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithTopic("*"),
	)
	err = eventHandler.Handle(ctx, event)
	require.NoError(t, err)

	// Verify that the event was delivered to all enabled destinations
	spans := exporter.GetSpans()
	var deliverySpans tracetest.SpanStubs
	for _, span := range spans {
		if span.Name != "StartDelivery" {
			continue
		}
		deliverySpans = append(deliverySpans, span)
	}

	// Should have one delivery span per enabled destination
	require.Len(t, deliverySpans, len(destinations), "event should be delivered to all enabled destinations")

	// Verify each destination received the event by checking the delivery queue
	destinationIDs := make(map[string]bool)
	for _, dest := range destinations {
		destinationIDs[dest.ID] = false
	}

	// Create a context with timeout for receiving messages
	receiveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Consume messages from the delivery queue to verify deliveries
	for i := 0; i < len(destinations); i++ {
		msg, err := subscription.Receive(receiveCtx)
		require.NoError(t, err)

		var deliveryEvent models.DeliveryEvent
		err = deliveryEvent.FromMessage(msg)
		require.NoError(t, err)

		// Verify this is a destination we expect
		_, exists := destinationIDs[deliveryEvent.DestinationID]
		require.True(t, exists, "delivery to unexpected destination: %s", deliveryEvent.DestinationID)
		destinationIDs[deliveryEvent.DestinationID] = true

		// Verify this is not the disabled destination
		require.NotEqual(t, disabledDest.ID, deliveryEvent.DestinationID, "disabled destination should not receive events")

		// Verify event data is correct
		require.Equal(t, event.ID, deliveryEvent.Event.ID)
		require.Equal(t, event.Topic, deliveryEvent.Event.Topic)
		require.Equal(t, event.TenantID, deliveryEvent.Event.TenantID)

		// Acknowledge the message
		msg.Ack()
	}

	// Verify all destinations received the event
	for destID, received := range destinationIDs {
		require.True(t, received, "destination %s did not receive the event", destID)
	}

	// Verify no more messages by waiting a bit with a short timeout
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	msg, err := subscription.Receive(shortCtx)
	require.Error(t, err, "context deadline exceeded")
	require.Nil(t, msg)
}
