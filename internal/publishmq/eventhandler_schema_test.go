package publishmq_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

func (s *countingStore) MatchEvent(ctx context.Context, event models.Event, allowWildcards bool) ([]tenantstore.MatchedDestination, error) {
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

// blockingValidator holds every validation until released, and caps the data
// it validates like *topicschema.Catalog.
type blockingValidator struct {
	maxBytes int
	// entered receives the size of each validation once it starts.
	entered chan int
	release chan struct{}
	once    sync.Once
}

func newBlockingValidator(maxBytes int) *blockingValidator {
	return &blockingValidator{maxBytes: maxBytes, entered: make(chan int, 16), release: make(chan struct{})}
}

func (v *blockingValidator) MaxValidationBytes() int { return v.maxBytes }

// releaseAll lets every validation, current and future, finish.
func (v *blockingValidator) releaseAll() { v.once.Do(func() { close(v.release) }) }

func (v *blockingValidator) ValidateData(_ string, data []byte) topicschema.ValidationResult {
	v.entered <- len(data)
	<-v.release
	return topicschema.ValidationResult{Mode: topicschema.ValidationOff}
}

// waitEntered returns the size of the next validation to start.
func (v *blockingValidator) waitEntered(t *testing.T) int {
	t.Helper()
	select {
	case n := <-v.entered:
		return n
	case <-time.After(2 * time.Second):
		t.Fatal("a validation should have started")
		return 0
	}
}

// requireWaiting fails if a validation starts soon.
func (v *blockingValidator) requireWaiting(t *testing.T) {
	t.Helper()
	select {
	case n := <-v.entered:
		t.Fatalf("a validation of %d bytes started; it should wait", n)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventHandler_SchemaValidationBudget(t *testing.T) {
	type publishFunc func(ctx context.Context, id string, size int) <-chan error
	// The validator caps data at 100 bytes, so at most 400 bytes are
	// validated at once.
	setup := func(t *testing.T) (*blockingValidator, publishFunc) {
		v := newBlockingValidator(100)
		handler := publishmq.NewEventHandler(
			testutil.CreateTestLogger(t), nil, tenantstore.NewMemTenantStore(), nil,
			testutil.TestTopics, false, nil,
			publishmq.WithSchemaValidator(v),
			publishmq.WithMetrics(newFakeMetrics(t)),
		)
		var running sync.WaitGroup
		t.Cleanup(func() {
			v.releaseAll()
			running.Wait()
		})
		publish := func(ctx context.Context, id string, size int) <-chan error {
			event := schemaEvent(id)
			const prefix = `{"pad":"`
			event.Data = json.RawMessage(prefix + strings.Repeat("x", size-len(prefix)-2) + `"}`)
			done := make(chan error, 1)
			running.Go(func() {
				_, err := handler.Handle(ctx, event)
				done <- err
			})
			return done
		}
		return v, publish
	}
	// fill starts four 100-byte validations, which take the whole budget.
	fill := func(t *testing.T, v *blockingValidator, publish publishFunc) []<-chan error {
		var done []<-chan error
		for i := range 4 {
			done = append(done, publish(t.Context(), fmt.Sprintf("evt_%d", i), 100))
			assert.Equal(t, 100, v.waitEntered(t))
		}
		return done
	}
	result := func(t *testing.T, done <-chan error) error {
		t.Helper()
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			t.Fatal("the publish should have returned")
			return nil
		}
	}

	t.Run("validations beyond the budget wait", func(t *testing.T) {
		v, publish := setup(t)
		done := fill(t, v, publish)

		next := publish(t.Context(), "evt_next", 100)
		v.requireWaiting(t)

		v.release <- struct{}{}
		assert.Equal(t, 100, v.waitEntered(t), "a finished validation lets the next one start")
		v.releaseAll()
		for _, d := range append(done, next) {
			require.NoError(t, result(t, d))
		}
	})

	t.Run("waiting ends with the context", func(t *testing.T) {
		v, publish := setup(t)
		fill(t, v, publish)

		ctx, cancel := context.WithCancel(t.Context())
		waiting := publish(ctx, "evt_cancelled", 100)
		v.requireWaiting(t)
		cancel()
		require.ErrorIs(t, result(t, waiting), context.Canceled)
		v.requireWaiting(t)
	})

	t.Run("data over the size limit doesn't wait", func(t *testing.T) {
		// The validator rejects or skips it without parsing it.
		v, publish := setup(t)
		fill(t, v, publish)

		publish(t.Context(), "evt_large", 101)
		assert.Equal(t, 101, v.waitEntered(t))
	})
}
