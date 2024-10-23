package deliverymq_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/logmq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type RetryDeliveryMQSuite struct {
	ctx              context.Context
	mqConfig         *mqs.QueueConfig
	webhookCallCount int
	exporter         *tracetest.InMemoryExporter
	redisClient      *redis.Client
	entityStore      models.EntityStore
	logStore         models.LogStore
	deliveryMQ       *deliverymq.DeliveryMQ
	tenant           models.Tenant
	destination      models.Destination
	webhookHandler   func(w http.ResponseWriter, r *http.Request)
	teardown         func()
}

func (s *RetryDeliveryMQSuite) SetupTest(t *testing.T) {
	require.NotNil(t, s.ctx, "RetryDeliveryMQSuite.ctx is not set")
	require.NotNil(t, s.mqConfig, "RetryDeliveryMQSuite.mqConfig is not set")
	require.NotNil(t, s.logStore, "RetryDeliveryMQSuite.logStore is not set")

	teardownFuncs := []func(){}

	s.deliveryMQ = deliverymq.New(deliverymq.WithQueue(s.mqConfig))
	cleanup, err := s.deliveryMQ.Init(s.ctx)
	require.NoError(t, err)
	teardownFuncs = append(teardownFuncs, cleanup)

	mq := mqs.NewQueue(s.mqConfig)
	subscription, err := mq.Subscribe(s.ctx)
	require.NoError(t, err)
	teardownFuncs = append(teardownFuncs, func() { subscription.Shutdown(s.ctx) })

	if s.exporter == nil {
		s.exporter = tracetest.NewInMemoryExporter()
	}
	if s.redisClient == nil {
		s.redisClient = testutil.CreateTestRedisClient(t)
	}
	if s.entityStore == nil {
		s.entityStore = models.NewEntityStore(s.redisClient, models.NewAESCipher(""))
	}
	logMQ := logmq.New()
	cleanupLogMQ, err := logMQ.Init(s.ctx)
	require.NoError(t, err)
	teardownFuncs = append(teardownFuncs, cleanupLogMQ)

	retryScheduler := deliverymq.NewRetryScheduler(s.deliveryMQ, testutil.CreateTestRedisConfig(t))
	require.NoError(t, retryScheduler.Init(s.ctx))
	teardownFuncs = append(teardownFuncs, func() { retryScheduler.Shutdown() })
	go retryScheduler.Monitor(s.ctx)

	handler := deliverymq.NewMessageHandler(
		testutil.CreateTestLogger(t),
		s.redisClient,
		logMQ,
		s.entityStore,
		s.logStore,
		testutil.NewMockEventTracer(s.exporter),
		retryScheduler,
	)

	go func() {
		for {
			msg, err := subscription.Receive(s.ctx)
			if err != nil {
				log.Println("subscription error", err)
				return
			}
			handler.Handle(s.ctx, msg)
		}
	}()

	// Setup webhook server
	mux := http.NewServeMux()
	s.webhookCallCount = 0
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.webhookHandler == nil {
			time.Sleep(time.Second / 3)
			s.webhookCallCount = s.webhookCallCount + 1
			if s.webhookCallCount == 3 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		} else {
			s.webhookHandler(w, r)
		}
	}))
	server := http.Server{
		Addr:    testutil.RandomPort(),
		Handler: mux,
	}
	webhookURL := "http://localhost" + server.Addr + "/webhook"
	go func() {
		server.ListenAndServe()
	}()
	teardownFuncs = append(teardownFuncs, func() {
		server.Shutdown(s.ctx)
	})

	// Setup destination
	s.tenant = models.Tenant{
		ID: uuid.New().String(),
	}
	s.destination = testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhooks"),
		testutil.DestinationFactory.WithTenantID(s.tenant.ID),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": webhookURL}),
	)
	require.NoError(t, s.entityStore.UpsertDestination(s.ctx, s.destination))

	s.teardown = func() {
		for _, teardown := range teardownFuncs {
			teardown()
		}
	}
}

func (suite *RetryDeliveryMQSuite) TeardownTest(t *testing.T) {
	suite.teardown()
}

func TestDeliveryMQRetry_EligibleForRetryFalse(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	queueConfig := &mqs.QueueConfig{
		InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)},
	}
	logStore := &mockLogStore{}
	suite := &RetryDeliveryMQSuite{
		ctx:      ctx,
		mqConfig: queueConfig,
		logStore: logStore,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	// Act
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(suite.tenant.ID),
		testutil.EventFactory.WithDestinationID(suite.destination.ID),
		testutil.EventFactory.WithEligibleForRetry(false),
	)
	logStore.registerEvent(&event)
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: suite.destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, deliveryEvent))

	// Assert
	<-ctx.Done()
	spans := suite.exporter.GetSpans()
	var deliverSpans tracetest.SpanStubs
	for _, span := range spans {
		if span.Name != "Deliver" {
			continue
		}
		deliverSpans = append(deliverSpans, span)
	}
	assert.Len(t, deliverSpans, 1)
}

func TestDeliveryMQRetry_EligibleForRetryTrue(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // expect 3 retries
	defer cancel()

	queueConfig := &mqs.QueueConfig{
		InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)},
	}
	logStore := &mockLogStore{}
	suite := &RetryDeliveryMQSuite{
		ctx:      context.Background(),
		mqConfig: queueConfig,
		logStore: logStore,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	// Act
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(suite.tenant.ID),
		testutil.EventFactory.WithDestinationID(suite.destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)
	logStore.registerEvent(&event)
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: suite.destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, deliveryEvent))

	// Assert
	<-ctx.Done()
	spans := suite.exporter.GetSpans()
	var deliverSpans tracetest.SpanStubs
	for _, span := range spans {
		if span.Name != "Deliver" {
			continue
		}
		deliverSpans = append(deliverSpans, span)
	}
	assert.Len(t, deliverSpans, 3)
}

// TODO: test between publish error vs system error

func TestDeliveryMQRetry_SystemError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	queueConfig := &mqs.QueueConfig{
		InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)},
	}
	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := newMockEntityStore(models.NewEntityStore(redisClient, models.NewAESCipher("")))
	logStore := &mockLogStore{}
	suite := &RetryDeliveryMQSuite{
		ctx:         ctx,
		mqConfig:    queueConfig,
		redisClient: redisClient,
		entityStore: entityStore,
		logStore:    logStore,
	}
	suite.SetupTest(t)
	defer suite.TeardownTest(t)

	// Act
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(suite.tenant.ID),
		testutil.EventFactory.WithDestinationID(suite.destination.ID),
		testutil.EventFactory.WithEligibleForRetry(false),
	)
	logStore.registerEvent(&event)
	deliveryEvent := models.DeliveryEvent{
		ID:            uuid.New().String(),
		Event:         event,
		DestinationID: suite.destination.ID,
	}
	require.NoError(t, suite.deliveryMQ.Publish(ctx, deliveryEvent))

	// Assert
	<-ctx.Done()
	spans := suite.exporter.GetSpans()
	var deliverSpans tracetest.SpanStubs
	for _, span := range spans {
		if span.Name != "Deliver" {
			continue
		}
		deliverSpans = append(deliverSpans, span)
	}
	assert.Greater(t, len(deliverSpans), 1, "expected delivery to be retried")
}

type mockEntityStore struct {
	models.EntityStore
}

func newMockEntityStore(entityStore models.EntityStore) models.EntityStore {
	return &mockEntityStore{
		EntityStore: entityStore,
	}
}

func (m *mockEntityStore) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error) {
	return nil, fmt.Errorf("err")
}

type mockLogStore struct {
	events map[string]*models.Event
}

var _ models.LogStore = &mockLogStore{}

func (m *mockLogStore) ListEvent(ctx context.Context, request models.ListEventRequest) ([]*models.Event, string, error) {
	return nil, "", nil
}

func (m *mockLogStore) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	if m.events == nil {
		return nil, nil
	}
	event, ok := m.events[eventID]
	if !ok {
		return nil, nil
	}
	return event, nil
}

func (m *mockLogStore) ListDelivery(ctx context.Context, request models.ListDeliveryRequest) ([]*models.Delivery, error) {
	return nil, nil
}

func (m *mockLogStore) InsertManyEvent(ctx context.Context, events []*models.Event) error {
	return nil
}

func (m *mockLogStore) InsertManyDelivery(ctx context.Context, deliveries []*models.Delivery) error {
	return nil
}

func (m *mockLogStore) registerEvent(event *models.Event) {
	if m.events == nil {
		m.events = make(map[string]*models.Event)
	}
	m.events[event.ID] = event
}
