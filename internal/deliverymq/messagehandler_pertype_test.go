package deliverymq_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// mcpType stands in for a destination type with its own retry policy,
// generation checks and parking (the MCP destination in production).
const mcpType = "mcp"

// ============================== Doubles ==============================

// sequenceDestinationGetter returns dests[i] on the i-th call, repeating the
// last one. It lets a test change the stored destination between the handler's
// first read and a re-read.
type sequenceDestinationGetter struct {
	mu    sync.Mutex
	dests []*models.Destination
	calls int
}

func (g *sequenceDestinationGetter) RetrieveDestination(ctx context.Context, tenantID, destID string) (*models.Destination, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := min(g.calls, len(g.dests)-1)
	g.calls++
	return g.dests[i], nil
}

func (g *sequenceDestinationGetter) set(dests ...*models.Destination) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dests = dests
	g.calls = 0
}

func (g *sequenceDestinationGetter) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// recordingPublisher returns err (nil = success) and records the destination
// each attempt was made against, with a snapshot of its credentials.
type recordingPublisher struct {
	mu    sync.Mutex
	err   error
	dests []*models.Destination
	creds []map[string]string
}

func (p *recordingPublisher) PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) (*models.Attempt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	creds := make(map[string]string, len(destination.Credentials))
	for k, v := range destination.Credentials {
		creds[k] = v
	}
	p.dests = append(p.dests, destination)
	p.creds = append(p.creds, creds)

	attempt := &models.Attempt{
		ID:              idgen.Attempt(),
		EventID:         event.ID,
		DestinationID:   destination.ID,
		DestinationType: destination.Type,
		Status:          models.AttemptStatusSuccess,
		Code:            "200",
		Time:            time.Now(),
	}
	if p.err != nil {
		attempt.Status = models.AttemptStatusFailed
		attempt.Code = "500"
		return attempt, p.err
	}
	return attempt, nil
}

func (p *recordingPublisher) attempts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.dests)
}

type parkCall struct {
	task        deliverymq.RetryTask
	destination *models.Destination
}

// fakeParker records ParkRetry calls and answers with parked/err.
type fakeParker struct {
	mu     sync.Mutex
	parked bool
	err    error
	calls  []parkCall
}

func (p *fakeParker) ParkRetry(ctx context.Context, task deliverymq.RetryTask, destination *models.Destination) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, parkCall{task: task, destination: destination})
	return p.parked, p.err
}

func (p *fakeParker) snapshot() []parkCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]parkCall(nil), p.calls...)
}

// ============================== Helpers ==============================

// observedLogger returns a test logger whose Info+ lines can be inspected.
func observedLogger() (*logging.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.InfoLevel)
	return logging.NewTestLogger(zap.New(core)), logs
}

type handlerDeps struct {
	getter    deliverymq.DestinationGetter
	publisher deliverymq.Publisher
	logPub    *mockLogPublisher
	scheduler *mockRetryScheduler
	logger    *logging.Logger
	opts      []deliverymq.MessageHandlerOption
}

// newPerTypeHandler builds a handler with the default policy (constant 1s,
// 10 retries) plus opts.
func newPerTypeHandler(t *testing.T, d handlerDeps) consumer.MessageHandler {
	t.Helper()
	if d.logger == nil {
		d.logger = testutil.CreateTestLogger(t)
	}
	if d.logPub == nil {
		d.logPub = newMockLogPublisher(nil)
	}
	if d.scheduler == nil {
		d.scheduler = newMockRetryScheduler()
	}
	return deliverymq.NewMessageHandler(
		d.logger,
		d.logPub,
		d.getter,
		d.publisher,
		testutil.NewMockEventTracer(nil),
		d.scheduler,
		&backoff.ConstantBackoff{Interval: time.Second},
		10,
		idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(24*time.Hour)),
		d.opts...,
	)
}

func mcpSchedule() backoff.Backoff {
	return &backoff.ScheduledBackoff{Schedule: []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}}
}

func retryEvent(tenantID, destID string) models.Event {
	return testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)
}

func failure(nonRetryable bool) error {
	return &destregistry.ErrDestinationPublishAttempt{
		Err:          errors.New("receiver failed"),
		Provider:     mcpType,
		NonRetryable: nonRetryable,
	}
}

func handle(t *testing.T, h consumer.MessageHandler, task models.DeliveryTask) (*mockMessage, error) {
	t.Helper()
	mockMsg, msg := newDeliveryMockMessage(task)
	err := h.Handle(context.Background(), msg)
	return mockMsg, err
}

func decodeRetryTask(t *testing.T, s string) deliverymq.RetryTask {
	t.Helper()
	var rt deliverymq.RetryTask
	require.NoError(t, rt.FromString(s))
	return rt
}

// ============================== Retry policy ==============================

func TestMessageHandler_RetryPolicyPerType(t *testing.T) {
	t.Parallel()

	tenantID := idgen.String()
	mcpDest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType(mcpType),
		testutil.DestinationFactory.WithTenantID(tenantID),
	)
	webhookDest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenantID),
	)

	tests := []struct {
		name        string
		dest        *models.Destination
		attempt     int
		wantRetry   bool
		wantBackoff time.Duration
	}{
		{"mcp first attempt uses its first backoff", &mcpDest, 1, true, 30 * time.Second},
		{"mcp second attempt", &mcpDest, 2, true, 2 * time.Minute},
		{"mcp third attempt uses its last backoff", &mcpDest, 3, true, 10 * time.Minute},
		{"mcp fourth attempt is final", &mcpDest, 4, false, 0},
		{"webhook keeps the default backoff", &webhookDest, 1, true, time.Second},
		{"webhook keeps the default max", &webhookDest, 4, true, time.Second},
		{"webhook final attempt", &webhookDest, 11, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scheduler := newMockRetryScheduler()
			h := newPerTypeHandler(t, handlerDeps{
				getter:    &mockDestinationGetter{dest: tt.dest},
				publisher: newMockPublisher([]error{failure(false)}),
				scheduler: scheduler,
				opts:      []deliverymq.MessageHandlerOption{deliverymq.WithRetryPolicy(mcpType, mcpSchedule(), 3)},
			})
			task := models.DeliveryTask{Event: retryEvent(tenantID, tt.dest.ID), DestinationID: tt.dest.ID, Attempt: tt.attempt}

			mockMsg, err := handle(t, h, task)
			require.NoError(t, err)
			assert.True(t, mockMsg.acked)
			if !tt.wantRetry {
				assert.Empty(t, scheduler.schedules)
				return
			}
			require.Len(t, scheduler.schedules, 1)
			entry := scheduler.entries[models.RetryID(task.Event.ID, tt.dest.ID)]
			assert.Equal(t, tt.wantBackoff, entry.delay)
		})
	}
}

func TestMessageHandler_RetryPolicyPerType_AttemptMax(t *testing.T) {
	t.Parallel()

	tenantID := idgen.String()
	for _, tc := range []struct {
		destType string
		want     int64
	}{
		{mcpType, 4},
		{"webhook", 11},
	} {
		t.Run(tc.destType, func(t *testing.T) {
			t.Parallel()
			dest := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType(tc.destType),
				testutil.DestinationFactory.WithTenantID(tenantID),
			)
			logger, logs := observedLogger()
			h := newPerTypeHandler(t, handlerDeps{
				getter:    &mockDestinationGetter{dest: &dest},
				publisher: newMockPublisher(nil),
				logger:    logger,
				opts:      []deliverymq.MessageHandlerOption{deliverymq.WithRetryPolicy(mcpType, mcpSchedule(), 3)},
			})
			_, err := handle(t, h, models.NewDeliveryTask(retryEvent(tenantID, dest.ID), dest.ID))
			require.NoError(t, err)

			lines := logs.FilterMessage("delivery.attempted").All()
			require.Len(t, lines, 1)
			assert.Equal(t, tc.want, lines[0].ContextMap()["attempt_max"])
		})
	}
}

func TestMessageHandler_RetryPolicy_ZeroMaxDisablesRetries(t *testing.T) {
	t.Parallel()
	dest := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithType(mcpType))
	scheduler := newMockRetryScheduler()
	h := newPerTypeHandler(t, handlerDeps{
		getter:    &mockDestinationGetter{dest: &dest},
		publisher: newMockPublisher([]error{failure(false)}),
		scheduler: scheduler,
		opts:      []deliverymq.MessageHandlerOption{deliverymq.WithRetryPolicy(mcpType, nil, -1)},
	})
	_, err := handle(t, h, models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID))
	require.NoError(t, err)
	assert.Empty(t, scheduler.schedules)
}

// ============================== Non-retryable ==============================

func TestMessageHandler_NonRetryable(t *testing.T) {
	t.Parallel()

	t.Run("automatic attempt: acked and logged, no retry", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithType(mcpType))
		scheduler := newMockRetryScheduler()
		logPub := newMockLogPublisher(nil)
		logger, logs := observedLogger()
		h := newPerTypeHandler(t, handlerDeps{
			getter:    &mockDestinationGetter{dest: &dest},
			publisher: newMockPublisher([]error{failure(true)}),
			scheduler: scheduler,
			logPub:    logPub,
			logger:    logger,
			opts:      []deliverymq.MessageHandlerOption{deliverymq.WithRetryPolicy(mcpType, mcpSchedule(), 3)},
		})

		mockMsg, err := handle(t, h, models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID))
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.False(t, mockMsg.nacked)
		assert.Empty(t, scheduler.schedules, "a non-retryable failure (410/413/payload_too_large) is never retried")
		require.Len(t, logPub.entries, 1, "the failed attempt is still logged")
		assert.Equal(t, models.AttemptStatusFailed, logPub.entries[0].Attempt.Status)

		lines := logs.FilterMessage("delivery.attempted").All()
		require.Len(t, lines, 1)
		assert.Equal(t, true, lines[0].ContextMap()["non_retryable"])
	})

	t.Run("manual attempt: cancels any pending automatic retry", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithType(mcpType))
		scheduler := newMockRetryScheduler()
		h := newPerTypeHandler(t, handlerDeps{
			getter:    &mockDestinationGetter{dest: &dest},
			publisher: newMockPublisher([]error{failure(true)}),
			scheduler: scheduler,
		})
		event := retryEvent(dest.TenantID, dest.ID)

		mockMsg, err := handle(t, h, models.NewManualDeliveryTask(event, dest.ID, 2))
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.Empty(t, scheduler.schedules)
		assert.Equal(t, []string{models.RetryID(event.ID, dest.ID)}, scheduler.canceled)
	})

	t.Run("retryable failure of the same type still retries", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithType(mcpType))
		scheduler := newMockRetryScheduler()
		h := newPerTypeHandler(t, handlerDeps{
			getter:    &mockDestinationGetter{dest: &dest},
			publisher: newMockPublisher([]error{failure(false)}),
			scheduler: scheduler,
		})
		_, err := handle(t, h, models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID))
		require.NoError(t, err)
		assert.Len(t, scheduler.schedules, 1)
	})
}

// ============================== Expiry ==============================

func TestMessageHandler_DestinationExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	expiredBefore := func(cutoff time.Time) func(*models.Destination, time.Time) bool {
		return func(d *models.Destination, at time.Time) bool { return !at.Before(cutoff) }
	}

	for _, tc := range []struct {
		name     string
		task     func(dest models.Destination) models.DeliveryTask
		disabled bool
	}{
		{"first attempt", func(dest models.Destination) models.DeliveryTask {
			return models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
		}, false},
		{"automatic retry", func(dest models.Destination) models.DeliveryTask {
			task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
			task.Attempt = 3
			return task
		}, false},
		{"manual retry", func(dest models.Destination) models.DeliveryTask {
			return models.NewManualDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID, 2)
		}, false},
		{"expired and disabled retry is dropped, not parked", func(dest models.Destination) models.DeliveryTask {
			task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
			task.Attempt = 2
			return task
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := []func(*models.Destination){testutil.DestinationFactory.WithType(mcpType)}
			if tc.disabled {
				opts = append(opts, testutil.DestinationFactory.WithDisabledAt(now.Add(-time.Hour)))
			}
			dest := testutil.DestinationFactory.Any(opts...)
			publisher := &recordingPublisher{}
			scheduler := newMockRetryScheduler()
			logPub := newMockLogPublisher(nil)
			parker := &fakeParker{parked: true}
			h := newPerTypeHandler(t, handlerDeps{
				getter:    &mockDestinationGetter{dest: &dest},
				publisher: publisher,
				scheduler: scheduler,
				logPub:    logPub,
				opts: []deliverymq.MessageHandlerOption{
					deliverymq.WithClockForTest(func() time.Time { return now }),
					deliverymq.WithExpiryCheckForTest(expiredBefore(now)),
					deliverymq.WithRetryParker(parker),
					deliverymq.WithParkedRetryTypes(mcpType),
				},
			})

			mockMsg, err := handle(t, h, tc.task(dest))
			require.NoError(t, err)
			assert.True(t, mockMsg.acked, "expired is permanent: acked")
			assert.False(t, mockMsg.nacked)
			assert.Zero(t, publisher.attempts(), "no attempt after expiry")
			assert.Empty(t, logPub.entries)
			assert.Empty(t, scheduler.schedules)
			assert.Empty(t, scheduler.canceled)
			assert.Empty(t, parker.snapshot())
		})
	}

	// Without the test hook: the destination's own expires_at.
	t.Run("stored expires_at", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name      string
			expiresAt time.Time
			delivered bool
		}{
			{"passed", now.Add(-time.Millisecond), false},
			{"now", now, false},
			{"ahead", now.Add(time.Millisecond), true},
		} {
			dest := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType(mcpType),
				testutil.DestinationFactory.WithExpiresAt(tc.expiresAt),
			)
			publisher := &recordingPublisher{}
			h := newPerTypeHandler(t, handlerDeps{
				getter:    &mockDestinationGetter{dest: &dest},
				publisher: publisher,
				opts:      []deliverymq.MessageHandlerOption{deliverymq.WithClockForTest(func() time.Time { return now })},
			})
			mockMsg, err := handle(t, h, models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID))
			require.NoError(t, err, tc.name)
			assert.True(t, mockMsg.acked, tc.name)
			assert.Equal(t, tc.delivered, publisher.attempts() == 1, tc.name)
		}
	})

	t.Run("not yet expired is delivered", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithType(mcpType))
		publisher := &recordingPublisher{}
		h := newPerTypeHandler(t, handlerDeps{
			getter:    &mockDestinationGetter{dest: &dest},
			publisher: publisher,
			opts: []deliverymq.MessageHandlerOption{
				deliverymq.WithClockForTest(func() time.Time { return now }),
				deliverymq.WithExpiryCheckForTest(expiredBefore(now.Add(time.Second))),
			},
		})
		mockMsg, err := handle(t, h, models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID))
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.Equal(t, 1, publisher.attempts())
	})
}

// ============================== Generation ==============================

func TestMessageHandler_GenerationCheck(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 10, 9, 12, 0, 0, 123_456_789, time.UTC)

	t.Run("retry for an earlier generation is dropped", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType(mcpType),
			testutil.DestinationFactory.WithCreatedAt(created),
		)
		publisher := &recordingPublisher{}
		logPub := newMockLogPublisher(nil)
		scheduler := newMockRetryScheduler()
		h := newPerTypeHandler(t, handlerDeps{
			getter: &mockDestinationGetter{dest: &dest}, publisher: publisher, logPub: logPub, scheduler: scheduler,
			opts: []deliverymq.MessageHandlerOption{deliverymq.WithGenerationCheckedTypes(mcpType)},
		})
		task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
		task.Attempt = 2
		task.DestinationCreatedAt = created.Add(-time.Hour).UnixMilli()

		mockMsg, err := handle(t, h, task)
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.False(t, mockMsg.nacked)
		assert.Zero(t, publisher.attempts())
		assert.Empty(t, logPub.entries)
		assert.Empty(t, scheduler.schedules)
	})

	t.Run("retry for the current generation is delivered (ms precision)", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType(mcpType),
			testutil.DestinationFactory.WithCreatedAt(created),
		)
		publisher := &recordingPublisher{}
		h := newPerTypeHandler(t, handlerDeps{
			getter: &mockDestinationGetter{dest: &dest}, publisher: publisher,
			opts: []deliverymq.MessageHandlerOption{deliverymq.WithGenerationCheckedTypes(mcpType)},
		})
		task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
		task.Attempt = 2
		task.DestinationCreatedAt = created.Truncate(time.Millisecond).UnixMilli()

		mockMsg, err := handle(t, h, task)
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.Equal(t, 1, publisher.attempts())
	})

	t.Run("stale and disabled is dropped, not parked", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType(mcpType),
			testutil.DestinationFactory.WithCreatedAt(created),
			testutil.DestinationFactory.WithDisabledAt(created.Add(time.Minute)),
		)
		parker := &fakeParker{parked: true}
		h := newPerTypeHandler(t, handlerDeps{
			getter: &mockDestinationGetter{dest: &dest}, publisher: &recordingPublisher{},
			opts: []deliverymq.MessageHandlerOption{
				deliverymq.WithGenerationCheckedTypes(mcpType),
				deliverymq.WithRetryParker(parker),
				deliverymq.WithParkedRetryTypes(mcpType),
			},
		})
		task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
		task.Attempt = 2
		task.DestinationCreatedAt = created.Add(-time.Hour).UnixMilli()

		mockMsg, err := handle(t, h, task)
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.Empty(t, parker.snapshot())
	})

	t.Run("scheduled retries carry the generation for checked types only", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			destType string
			manual   bool
			want     int64
		}{
			{mcpType, false, created.UnixMilli()},
			{mcpType, true, created.UnixMilli()},
			{"webhook", false, 0},
		} {
			dest := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType(tc.destType),
				testutil.DestinationFactory.WithCreatedAt(created),
			)
			scheduler := newMockRetryScheduler()
			h := newPerTypeHandler(t, handlerDeps{
				getter: &mockDestinationGetter{dest: &dest}, publisher: newMockPublisher([]error{failure(false)}), scheduler: scheduler,
				opts: []deliverymq.MessageHandlerOption{deliverymq.WithGenerationCheckedTypes(mcpType)},
			})
			task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
			if tc.manual {
				task = models.NewManualDeliveryTask(task.Event, dest.ID, 2)
			}
			_, err := handle(t, h, task)
			require.NoError(t, err)
			require.Len(t, scheduler.schedules, 1, tc.destType)
			rt := decodeRetryTask(t, scheduler.schedules[0])
			assert.Equal(t, tc.want, rt.DestinationCreatedAt, "type=%s manual=%v", tc.destType, tc.manual)
			assert.Equal(t, dest.ID, rt.DestinationID)
			assert.Equal(t, task.Event.ID, rt.EventID)
		}
	})

	t.Run("untagged tasks are not checked", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType(mcpType),
			testutil.DestinationFactory.WithCreatedAt(created),
		)
		publisher := &recordingPublisher{}
		h := newPerTypeHandler(t, handlerDeps{
			getter: &mockDestinationGetter{dest: &dest}, publisher: publisher,
			opts: []deliverymq.MessageHandlerOption{deliverymq.WithGenerationCheckedTypes(mcpType)},
		})
		task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
		task.Attempt = 2 // e.g. a retry queued before the upgrade
		_, err := handle(t, h, task)
		require.NoError(t, err)
		assert.Equal(t, 1, publisher.attempts())
	})
}

// ============================== Parking ==============================

func TestMessageHandler_ParkRetry(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	disabledDest := func(destType string) models.Destination {
		return testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("sub_0123456789abcdef0123456789abcdef"),
			testutil.DestinationFactory.WithType(destType),
			testutil.DestinationFactory.WithCreatedAt(created),
			testutil.DestinationFactory.WithDisabledAt(created.Add(time.Minute)),
			testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "old"}),
		)
	}
	parkingOpts := func(parker *fakeParker) []deliverymq.MessageHandlerOption {
		return []deliverymq.MessageHandlerOption{
			deliverymq.WithGenerationCheckedTypes(mcpType),
			deliverymq.WithRetryParker(parker),
			deliverymq.WithParkedRetryTypes(mcpType),
		}
	}
	automaticRetry := func(dest models.Destination) models.DeliveryTask {
		task := models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
		task.Attempt = 3
		task.Telemetry = &models.DeliveryTelemetry{TraceID: "trace", SpanID: "span"}
		return task
	}

	t.Run("automatic retry on a disabled destination is parked and acked", func(t *testing.T) {
		t.Parallel()
		dest := disabledDest(mcpType)
		parker := &fakeParker{parked: true}
		publisher := &recordingPublisher{}
		logPub := newMockLogPublisher(nil)
		scheduler := newMockRetryScheduler()
		h := newPerTypeHandler(t, handlerDeps{
			getter: &mockDestinationGetter{dest: &dest}, publisher: publisher, logPub: logPub, scheduler: scheduler,
			opts: parkingOpts(parker),
		})
		task := automaticRetry(dest)

		mockMsg, err := handle(t, h, task)
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.False(t, mockMsg.nacked)
		assert.Zero(t, publisher.attempts())
		assert.Empty(t, logPub.entries, "parking is not an attempt")
		assert.Empty(t, scheduler.schedules)

		calls := parker.snapshot()
		require.Len(t, calls, 1)
		assert.Equal(t, deliverymq.RetryTask{
			EventID:              task.Event.ID,
			TenantID:             dest.TenantID,
			DestinationID:        dest.ID,
			Telemetry:            task.Telemetry,
			DestinationCreatedAt: created.UnixMilli(),
		}, calls[0].task)
		assert.Same(t, &dest, calls[0].destination)
	})

	t.Run("enabled meanwhile: re-read and delivered against the current state", func(t *testing.T) {
		t.Parallel()
		stale := disabledDest(mcpType)
		current := stale
		current.DisabledAt = nil
		current.Credentials = map[string]string{"secret": "rotated"}
		getter := &sequenceDestinationGetter{dests: []*models.Destination{&stale, &current}}
		parker := &fakeParker{parked: false}
		publisher := &recordingPublisher{}
		logPub := newMockLogPublisher(nil)
		h := newPerTypeHandler(t, handlerDeps{getter: getter, publisher: publisher, logPub: logPub, opts: parkingOpts(parker)})

		mockMsg, err := handle(t, h, automaticRetry(stale))
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.Len(t, parker.snapshot(), 1)
		assert.Equal(t, 2, getter.callCount(), "the destination is re-read after the store reports it enabled")
		require.Equal(t, 1, publisher.attempts())
		assert.Same(t, &current, publisher.dests[0])
		assert.Equal(t, map[string]string{"secret": "rotated"}, publisher.creds[0])
		assert.Len(t, logPub.entries, 1)
	})

	t.Run("enabled meanwhile but deleted on re-read: dropped", func(t *testing.T) {
		t.Parallel()
		stale := disabledDest(mcpType)
		parker := &fakeParker{parked: false}
		publisher := &recordingPublisher{}
		getter := &deletedOnSecondRead{dest: &stale}
		h := newPerTypeHandler(t, handlerDeps{getter: getter, publisher: publisher, opts: parkingOpts(parker)})

		mockMsg, err := handle(t, h, automaticRetry(stale))
		require.NoError(t, err)
		assert.True(t, mockMsg.acked)
		assert.Zero(t, publisher.attempts())
	})

	t.Run("enabled meanwhile but disabled again on re-read: nacked for redelivery", func(t *testing.T) {
		t.Parallel()
		dest := disabledDest(mcpType)
		getter := &sequenceDestinationGetter{dests: []*models.Destination{&dest}}
		parker := &fakeParker{parked: false}
		publisher := &recordingPublisher{}
		h := newPerTypeHandler(t, handlerDeps{getter: getter, publisher: publisher, opts: parkingOpts(parker)})

		mockMsg, err := handle(t, h, automaticRetry(dest))
		require.Error(t, err)
		assert.True(t, mockMsg.nacked)
		assert.False(t, mockMsg.acked)
		assert.Zero(t, publisher.attempts())
	})

	t.Run("park error nacks", func(t *testing.T) {
		t.Parallel()
		dest := disabledDest(mcpType)
		parker := &fakeParker{err: errors.New("redis down")}
		publisher := &recordingPublisher{}
		h := newPerTypeHandler(t, handlerDeps{getter: &mockDestinationGetter{dest: &dest}, publisher: publisher, opts: parkingOpts(parker)})

		mockMsg, err := handle(t, h, automaticRetry(dest))
		require.Error(t, err)
		assert.True(t, mockMsg.nacked)
		assert.False(t, mockMsg.acked)
		assert.Zero(t, publisher.attempts())
	})

	notParked := []struct {
		name     string
		destType string
		task     func(dest models.Destination) models.DeliveryTask
		opts     func(parker *fakeParker) []deliverymq.MessageHandlerOption
	}{
		{"manual retries are never parked", mcpType, func(dest models.Destination) models.DeliveryTask {
			return models.NewManualDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID, 3)
		}, parkingOpts},
		{"first attempts are never parked", mcpType, func(dest models.Destination) models.DeliveryTask {
			return models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID)
		}, parkingOpts},
		{"other types are dropped as before", "webhook", automaticRetry, parkingOpts},
		{"no parker configured", mcpType, automaticRetry, func(*fakeParker) []deliverymq.MessageHandlerOption {
			return []deliverymq.MessageHandlerOption{deliverymq.WithParkedRetryTypes(mcpType)}
		}},
	}
	for _, tc := range notParked {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dest := disabledDest(tc.destType)
			parker := &fakeParker{parked: true}
			publisher := &recordingPublisher{}
			h := newPerTypeHandler(t, handlerDeps{getter: &mockDestinationGetter{dest: &dest}, publisher: publisher, opts: tc.opts(parker)})

			mockMsg, err := handle(t, h, tc.task(dest))
			require.NoError(t, err)
			assert.True(t, mockMsg.acked, "disabled destination: acked and dropped")
			assert.Empty(t, parker.snapshot())
			assert.Zero(t, publisher.attempts())
		})
	}
}

// deletedOnSecondRead serves the destination once, then reports it deleted.
type deletedOnSecondRead struct {
	mu    sync.Mutex
	dest  *models.Destination
	calls int
}

func (g *deletedOnSecondRead) RetrieveDestination(ctx context.Context, tenantID, destID string) (*models.Destination, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.calls == 1 {
		return g.dest, nil
	}
	return nil, tenantstore.ErrDestinationDeleted
}

// ============================== Credentials ==============================

func TestMessageHandler_LogEntryWithoutCredentials(t *testing.T) {
	t.Parallel()

	for _, publishErr := range []error{nil, failure(false)} {
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType(mcpType),
			testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "whsec_c2VjcmV0", "previous_secret": "whsec_b2xk"}),
			testutil.DestinationFactory.WithConfig(map[string]string{"url": "https://agent.example/cb"}),
		)
		publisher := &recordingPublisher{err: publishErr}
		logPub := newMockLogPublisher(nil)
		h := newPerTypeHandler(t, handlerDeps{getter: &mockDestinationGetter{dest: &dest}, publisher: publisher, logPub: logPub})

		_, err := handle(t, h, models.NewDeliveryTask(retryEvent(dest.TenantID, dest.ID), dest.ID))
		require.NoError(t, err)

		require.Equal(t, 1, publisher.attempts())
		assert.Equal(t, map[string]string{"secret": "whsec_c2VjcmV0", "previous_secret": "whsec_b2xk"}, publisher.creds[0],
			"delivery uses the destination's credentials")
		assert.Equal(t, map[string]string{"secret": "whsec_c2VjcmV0", "previous_secret": "whsec_b2xk"}, map[string]string(dest.Credentials),
			"the stored destination is not mutated")

		require.Len(t, logPub.entries, 1)
		logged := logPub.entries[0].Destination
		require.NotNil(t, logged)
		assert.Nil(t, logged.Credentials)
		assert.Equal(t, dest.ID, logged.ID)
		assert.Equal(t, dest.Type, logged.Type)
		assert.Equal(t, dest.Config, logged.Config, "everything else the alert pipeline needs is kept")

		body, err := json.Marshal(logPub.entries[0])
		require.NoError(t, err)
		assert.NotContains(t, string(body), "whsec_")
	}
}

// ============================== RetryTask wire ==============================

func TestRetryTask_Wire(t *testing.T) {
	t.Parallel()

	t.Run("tasks queued by older binaries decode with no generation", func(t *testing.T) {
		var rt deliverymq.RetryTask
		require.NoError(t, rt.FromString(`{"EventID":"evt_1","TenantID":"t_1","DestinationID":"des_1","Telemetry":null}`))
		assert.Equal(t, deliverymq.RetryTask{EventID: "evt_1", TenantID: "t_1", DestinationID: "des_1"}, rt)
		assert.Zero(t, rt.ToDeliveryTask(models.Event{ID: "evt_1"}, 2).DestinationCreatedAt)
	})

	t.Run("unset generation keeps the legacy bytes", func(t *testing.T) {
		rt := deliverymq.RetryTask{EventID: "evt_1", TenantID: "t_1", DestinationID: "des_1"}
		s, err := rt.ToString()
		require.NoError(t, err)
		assert.JSONEq(t, `{"EventID":"evt_1","TenantID":"t_1","DestinationID":"des_1","Telemetry":null}`, s)
	})

	t.Run("generation round-trips into the delivery task", func(t *testing.T) {
		rt := deliverymq.RetryTask{EventID: "evt_1", TenantID: "t_1", DestinationID: "des_1", DestinationCreatedAt: 1760000000123}
		s, err := rt.ToString()
		require.NoError(t, err)
		assert.Contains(t, s, `"destination_created_at":1760000000123`)
		var decoded deliverymq.RetryTask
		require.NoError(t, decoded.FromString(s))
		assert.Equal(t, rt, decoded)
		task := decoded.ToDeliveryTask(models.Event{ID: "evt_1"}, 3)
		assert.Equal(t, int64(1760000000123), task.DestinationCreatedAt)
		assert.Equal(t, int64(1760000000123), deliverymq.RetryTaskFromDeliveryTask(task).DestinationCreatedAt)
	})
}
