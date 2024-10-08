package publishmq_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/eventtracer"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/publishmq"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/require"
	traceSDK "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestPublishMQEventHandler_Concurrency(t *testing.T) {
	t.Parallel()

	// awsEndpoint, terminate, err := testutil.StartTestcontainerLocalstack()
	// require.Nil(t, err)
	// defer terminate()
	// queueConfig := mqs.QueueConfig{AWSSQS: &mqs.AWSSQSConfig{
	// 	Endpoint:                  awsEndpoint,
	// 	Region:                    "eu-central-1",
	// 	ServiceAccountCredentials: "test:test:",
	// 	Topic:                     "eventkit",
	// }}
	// testutil.DeclareTestAWSInfrastructure(context.Background(), queueConfig.AWSSQS, nil)

	exporter := tracetest.NewInMemoryExporter()
	mockEventTracer := newMockEventTracer(exporter)

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
	destFactory := testutil.MockDestinationFactory
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

type mockEventTracerImpl struct {
	tracer        trace.Tracer
	receive       func(context.Context, *models.Event) (context.Context, trace.Span)
	startDelivery func(context.Context, *models.DeliveryEvent) (context.Context, trace.Span)
	deliver       func(context.Context, *models.DeliveryEvent) (context.Context, trace.Span)
}

var _ eventtracer.EventTracer = (*mockEventTracerImpl)(nil)

func (m *mockEventTracerImpl) Receive(ctx context.Context, event *models.Event) (context.Context, trace.Span) {
	return m.receive(ctx, event)
}

func (m *mockEventTracerImpl) StartDelivery(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	return m.startDelivery(ctx, deliveryEvent)
}

func (m *mockEventTracerImpl) Deliver(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
	return m.deliver(ctx, deliveryEvent)
}

func newMockEventTracer(exporter traceSDK.SpanExporter) *mockEventTracerImpl {
	traceProvider := traceSDK.NewTracerProvider(traceSDK.WithBatcher(
		exporter,
		traceSDK.WithBatchTimeout(0),
	))

	mockEventTracer := &mockEventTracerImpl{
		tracer: traceProvider.Tracer("mockeventtracer"),
	}
	mockEventTracer.receive = func(ctx context.Context, event *models.Event) (context.Context, trace.Span) {
		return mockEventTracer.tracer.Start(ctx, "Receive")
	}
	mockEventTracer.startDelivery = func(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
		return mockEventTracer.tracer.Start(ctx, "StartDelivery")
	}
	mockEventTracer.deliver = func(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span) {
		return mockEventTracer.tracer.Start(ctx, "Deliver")
	}

	return mockEventTracer
}
