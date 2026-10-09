package publishmq_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

// fakeValidator returns a fixed result and records what it was asked.
type fakeValidator struct {
	result topicschema.ValidationResult
	topics []string
	datas  []string
}

func (v *fakeValidator) ValidateData(topic string, data []byte) topicschema.ValidationResult {
	v.topics = append(v.topics, topic)
	v.datas = append(v.datas, string(data))
	return v.result
}

type schemaInvalidCall struct {
	topic, mode, reason string
}

// fakeMetrics records EventSchemaInvalid and forwards everything else to the
// real (no-op without a provider) metrics.
type fakeMetrics struct {
	emetrics.OutpostMetrics
	mu    sync.Mutex
	calls []schemaInvalidCall
}

func newFakeMetrics(t *testing.T) *fakeMetrics {
	m, err := emetrics.New()
	require.NoError(t, err)
	return &fakeMetrics{OutpostMetrics: m}
}

func (m *fakeMetrics) EventSchemaInvalid(_ context.Context, topic, mode, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, schemaInvalidCall{topic, mode, reason})
}

// countingStore counts MatchEvent calls to prove a rejected publish never
// reaches matching.
type countingStore struct {
	tenantstore.TenantStore
	matchCalls int
}

func (s *countingStore) MatchEvent(ctx context.Context, event models.Event, allowWildcards bool) ([]string, error) {
	s.matchCalls++
	return s.TenantStore.MatchEvent(ctx, event, allowWildcards)
}

type schemaHarness struct {
	handler publishmq.EventHandler
	store   *countingStore
	metrics *fakeMetrics
	tasks   mqs.Subscription
	logs    *observer.ObservedLogs
}

// newSchemaHarness builds a handler on in-memory infrastructure (mem tenant
// store, in-memory delivery queue, miniredis idempotence) with tenant t1
// subscribed to user.created.
func newSchemaHarness(t *testing.T, opts ...publishmq.EventHandlerOption) *schemaHarness {
	t.Helper()
	ctx := t.Context()

	store := &countingStore{TenantStore: tenantstore.NewMemTenantStore()}
	require.NoError(t, store.UpsertTenant(ctx, testutil.TenantFactory.Any(testutil.TenantFactory.WithID("t1"))))
	require.NoError(t, store.UpsertDestination(ctx, testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("d1"),
		testutil.DestinationFactory.WithTenantID("t1"),
		testutil.DestinationFactory.WithTopics([]string{"user.created"}),
	)))

	deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{
		InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(8)},
	}))
	cleanup, err := deliveryMQ.Init(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	tasks, err := deliveryMQ.Subscribe(ctx)
	require.NoError(t, err)

	core, logs := observer.New(zap.InfoLevel)
	logger := logging.NewTestLogger(zap.New(zapcore.NewTee(zaptest.NewLogger(t).Core(), core)))

	metrics := newFakeMetrics(t)
	handler := publishmq.NewEventHandler(
		logger,
		deliveryMQ,
		store,
		testutil.NewMockEventTracer(tracetest.NewInMemoryExporter()),
		testutil.TestTopics,
		false,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(time.Hour)),
		append([]publishmq.EventHandlerOption{publishmq.WithMetrics(metrics)}, opts...)...,
	)
	return &schemaHarness{handler: handler, store: store, metrics: metrics, tasks: tasks, logs: logs}
}

// receivedLog returns the fields of the single event.received line.
func (h *schemaHarness) receivedLog(t *testing.T) map[string]any {
	t.Helper()
	entries := h.logs.FilterMessage("event.received").All()
	require.Len(t, entries, 1)
	return entries[0].ContextMap()
}

// receiveTask returns the next enqueued delivery task.
func (h *schemaHarness) receiveTask(t *testing.T) models.DeliveryTask {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	msg, err := h.tasks.Receive(ctx)
	require.NoError(t, err, "a delivery task should be enqueued")
	var task models.DeliveryTask
	require.NoError(t, task.FromMessage(msg))
	msg.Ack()
	return task
}

func schemaEvent(id string) *models.Event {
	return testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(id),
		testutil.EventFactory.WithTenantID("t1"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithData(json.RawMessage(`{"total":"12"}`)),
	)
}

func TestEventHandler_SchemaValidation(t *testing.T) {
	t.Run("enforce rejects invalid data before matching", func(t *testing.T) {
		errs := []string{"data.total: must be number", `data: missing required property "currency"`}
		v := &fakeValidator{result: topicschema.ValidationResult{
			Mode: topicschema.ValidationEnforce, Checked: true, Errors: errs,
		}}
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))

		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))

		require.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, publishmq.ErrSchemaValidation)
		var schemaErr *publishmq.SchemaValidationError
		require.ErrorAs(t, err, &schemaErr)
		assert.Equal(t, "user.created", schemaErr.Topic)
		assert.Equal(t, errs, schemaErr.Errors)
		assert.Contains(t, err.Error(), `"user.created"`)
		assert.Contains(t, err.Error(), "data.total: must be number")

		assert.Equal(t, []string{"user.created"}, v.topics)
		assert.Equal(t, []string{`{"total":"12"}`}, v.datas)
		assert.Zero(t, h.store.matchCalls, "a rejected publish must not be matched")
		assert.Equal(t, []schemaInvalidCall{{"user.created", "enforce", "invalid"}}, h.metrics.calls)

		fields := h.receivedLog(t)
		assert.Equal(t, "evt_1", fields["event_id"])
		assert.Equal(t, "user.created", fields["topic"])
		assert.Equal(t, "rejected", fields["schema_validation"])
		assert.Equal(t, []any{errs[0], errs[1]}, fields["schema_errors"])
	})

	t.Run("enforce rejects data over the size limit as too_large", func(t *testing.T) {
		v := &fakeValidator{result: topicschema.ValidationResult{
			Mode: topicschema.ValidationEnforce, Checked: true, Errors: []string{topicschema.DataTooLargeError},
		}}
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))

		_, err := h.handler.Handle(t.Context(), schemaEvent("evt_1"))

		var schemaErr *publishmq.SchemaValidationError
		require.ErrorAs(t, err, &schemaErr)
		assert.Equal(t, []string{topicschema.DataTooLargeError}, schemaErr.Errors)
		assert.Zero(t, h.store.matchCalls)
		assert.Equal(t, []schemaInvalidCall{{"user.created", "enforce", "too_large"}}, h.metrics.calls)
	})

	t.Run("enforce rejection leaves the event ID free to retry", func(t *testing.T) {
		v := &fakeValidator{result: topicschema.ValidationResult{
			Mode: topicschema.ValidationEnforce, Checked: true, Errors: []string{"data.total: must be number"},
		}}
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))

		_, err := h.handler.Handle(t.Context(), schemaEvent("evt_retry"))
		require.ErrorIs(t, err, publishmq.ErrSchemaValidation)

		// The publisher fixes the data and publishes the same ID again.
		v.result = topicschema.ValidationResult{Mode: topicschema.ValidationEnforce, Checked: true, Valid: true}
		result, err := h.handler.Handle(t.Context(), schemaEvent("evt_retry"))
		require.NoError(t, err)
		assert.False(t, result.Duplicate, "the rejected attempt must not claim the idempotency key")
		assert.Equal(t, []string{"d1"}, result.DestinationIDs)

		task := h.receiveTask(t)
		require.NotNil(t, task.Event.SchemaValid)
		assert.True(t, *task.Event.SchemaValid)
	})

	t.Run("warn marks invalid data and still delivers it", func(t *testing.T) {
		v := &fakeValidator{result: topicschema.ValidationResult{
			Mode: topicschema.ValidationWarn, Checked: true, Errors: []string{"data.total: must be number"},
		}}
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))
		event := schemaEvent("evt_warn")

		result, err := h.handler.Handle(t.Context(), event)

		require.NoError(t, err)
		assert.Equal(t, []string{"d1"}, result.DestinationIDs)
		require.NotNil(t, event.SchemaValid)
		assert.False(t, *event.SchemaValid)
		assert.Equal(t, []schemaInvalidCall{{"user.created", "warn", "invalid"}}, h.metrics.calls)

		task := h.receiveTask(t)
		assert.Equal(t, "d1", task.DestinationID)
		require.NotNil(t, task.Event.SchemaValid, "the delivery task must carry the verdict")
		assert.False(t, *task.Event.SchemaValid)

		// The log line is the only place warn-mode errors are reported.
		fields := h.receivedLog(t)
		assert.Equal(t, "invalid", fields["schema_validation"])
		assert.Equal(t, []any{"data.total: must be number"}, fields["schema_errors"])
	})

	t.Run("warn skips data over the size limit and leaves it unchecked", func(t *testing.T) {
		v := &fakeValidator{result: topicschema.ValidationResult{
			Mode: topicschema.ValidationWarn, SkippedTooLarge: true,
		}}
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))
		event := schemaEvent("evt_large")

		result, err := h.handler.Handle(t.Context(), event)

		require.NoError(t, err)
		assert.Equal(t, []string{"d1"}, result.DestinationIDs)
		assert.Nil(t, event.SchemaValid)
		assert.Equal(t, []schemaInvalidCall{{"user.created", "warn", "too_large"}}, h.metrics.calls)
		assert.Nil(t, h.receiveTask(t).Event.SchemaValid)
		assert.Equal(t, "skipped_too_large", h.receivedLog(t)["schema_validation"])
	})

	for _, mode := range []topicschema.ValidationMode{topicschema.ValidationWarn, topicschema.ValidationEnforce} {
		t.Run(string(mode)+" marks valid data", func(t *testing.T) {
			v := &fakeValidator{result: topicschema.ValidationResult{Mode: mode, Checked: true, Valid: true}}
			h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))
			event := schemaEvent("evt_valid")

			_, err := h.handler.Handle(t.Context(), event)

			require.NoError(t, err)
			require.NotNil(t, event.SchemaValid)
			assert.True(t, *event.SchemaValid)
			assert.Empty(t, h.metrics.calls, "valid data is not counted")
			task := h.receiveTask(t)
			require.NotNil(t, task.Event.SchemaValid)
			assert.True(t, *task.Event.SchemaValid)
			fields := h.receivedLog(t)
			assert.Equal(t, "valid", fields["schema_validation"])
			assert.NotContains(t, fields, "schema_errors")
		})
	}

	t.Run("off leaves the verdict unset", func(t *testing.T) {
		v := &fakeValidator{result: topicschema.ValidationResult{Mode: topicschema.ValidationOff}}
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))
		event := schemaEvent("evt_off")

		_, err := h.handler.Handle(t.Context(), event)

		require.NoError(t, err)
		assert.Nil(t, event.SchemaValid)
		assert.Empty(t, h.metrics.calls)
		assert.Nil(t, h.receiveTask(t).Event.SchemaValid)
		assert.NotContains(t, h.receivedLog(t), "schema_validation")
	})

	t.Run("no validator leaves the verdict unset", func(t *testing.T) {
		h := newSchemaHarness(t)
		event := schemaEvent("evt_none")

		_, err := h.handler.Handle(t.Context(), event)

		require.NoError(t, err)
		assert.Nil(t, event.SchemaValid)
		assert.Empty(t, h.metrics.calls)
	})

	t.Run("a verdict set by the caller is discarded", func(t *testing.T) {
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(&fakeValidator{
			result: topicschema.ValidationResult{Mode: topicschema.ValidationOff},
		}))
		event := schemaEvent("evt_spoof")
		spoofed := true
		event.SchemaValid = &spoofed

		_, err := h.handler.Handle(t.Context(), event)

		require.NoError(t, err)
		assert.Nil(t, event.SchemaValid)
		assert.Nil(t, h.receiveTask(t).Event.SchemaValid)
	})

	t.Run("topic errors win over schema validation", func(t *testing.T) {
		v := &fakeValidator{result: topicschema.ValidationResult{
			Mode: topicschema.ValidationEnforce, Checked: true, Errors: []string{"data: must be object"},
		}}
		h := newSchemaHarness(t, publishmq.WithSchemaValidator(v))
		event := schemaEvent("evt_topic")
		event.Topic = "not.a.topic"

		_, err := h.handler.Handle(t.Context(), event)

		require.ErrorIs(t, err, publishmq.ErrInvalidTopic)
		assert.Empty(t, v.topics, "unknown topics are rejected before validation")
		assert.Empty(t, h.metrics.calls)
	})
}

func TestSchemaValidationError(t *testing.T) {
	err := error(&publishmq.SchemaValidationError{Topic: "order.created", Errors: []string{"data.total: must be number", "data.id: must be string"}})
	wrapped := errors.Join(errors.New("context"), err)

	assert.ErrorIs(t, wrapped, publishmq.ErrSchemaValidation)
	assert.NotErrorIs(t, errors.New("other"), publishmq.ErrSchemaValidation)
	assert.Equal(t,
		`event data does not match the topic schema: topic "order.created": data.total: must be number; data.id: must be string`,
		err.Error())
}
