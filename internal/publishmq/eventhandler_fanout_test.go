package publishmq_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zaptest"
)

// inflightTracer counts the enqueues in flight: each starts at StartDelivery
// and ends when its span ends. StartDelivery holds each enqueue a moment so
// unbounded fan-out would pile up.
type inflightTracer struct {
	eventtracer.EventTracer
	hold     time.Duration
	inflight atomic.Int64
	peak     atomic.Int64
}

func (t *inflightTracer) StartDelivery(ctx context.Context, task *models.DeliveryTask) (context.Context, trace.Span) {
	n := t.inflight.Add(1)
	for {
		peak := t.peak.Load()
		if n <= peak || t.peak.CompareAndSwap(peak, n) {
			break
		}
	}
	time.Sleep(t.hold)
	ctx, span := t.EventTracer.StartDelivery(ctx, task)
	return ctx, &inflightSpan{Span: span, done: func() { t.inflight.Add(-1) }}
}

type inflightSpan struct {
	trace.Span
	once sync.Once
	done func()
}

func (s *inflightSpan) End(opts ...trace.SpanEndOption) {
	s.once.Do(s.done)
	s.Span.End(opts...)
}

// A publish matching many destinations enqueues at most 32 delivery tasks at
// once, and still enqueues every one.
func TestEventHandler_FanOutIsBounded(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const destinations = 100

	store := tenantstore.NewMemTenantStore()
	require.NoError(t, store.UpsertTenant(ctx, testutil.TenantFactory.Any(testutil.TenantFactory.WithID("t1"))))
	want := make([]string, 0, destinations)
	for i := range destinations {
		id := fmt.Sprintf("d%03d", i)
		want = append(want, id)
		require.NoError(t, store.UpsertDestination(ctx, testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(id),
			testutil.DestinationFactory.WithTenantID("t1"),
			testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		)))
	}

	deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{
		InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(8)},
	}))
	cleanup, err := deliveryMQ.Init(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	tasks, err := deliveryMQ.Subscribe(ctx)
	require.NoError(t, err)

	tracer := &inflightTracer{EventTracer: testutil.NewMockEventTracer(tracetest.NewInMemoryExporter()), hold: 5 * time.Millisecond}
	handler := publishmq.NewEventHandler(
		logging.NewTestLogger(zaptest.NewLogger(t)),
		deliveryMQ,
		store,
		tracer,
		testutil.TestTopics,
		false,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(time.Hour)),
	)

	result, err := handler.Handle(ctx, schemaEvent("evt_fanout"))
	require.NoError(t, err)
	assert.ElementsMatch(t, want, result.DestinationIDs)
	assert.LessOrEqual(t, tracer.peak.Load(), int64(32), "at most 32 enqueues in flight")
	assert.Greater(t, tracer.peak.Load(), int64(1), "enqueues still run concurrently")
	assert.Zero(t, tracer.inflight.Load())

	got := make([]string, 0, destinations)
	for range destinations {
		receiveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		msg, err := tasks.Receive(receiveCtx)
		cancel()
		require.NoError(t, err, "every destination gets a delivery task")
		var task models.DeliveryTask
		require.NoError(t, task.FromMessage(msg))
		msg.Ack()
		got = append(got, task.DestinationID)
	}
	assert.ElementsMatch(t, want, got)
}
