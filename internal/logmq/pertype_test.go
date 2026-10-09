package logmq_test

// Per-destination-type behaviour of the post-persist pipeline: retry limits
// carried to the evaluator, MCP 410s kept out of the failure streak, and the
// attempt status recorder.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/deliverystatus"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mcpType = "mcp"

// typedEntry is makeEntryFull with the destination type and attempt code set.
func typedEntry(destType, destID, tenantID, attemptID, status, code string, number int) models.LogEntry {
	entry := makeEntryFull(destID, tenantID, attemptID, status, number, true)
	entry.Destination.Type = destType
	entry.Attempt.DestinationType = destType
	entry.Attempt.Code = code
	return entry
}

// The type's own retry limit drives exhausted-retries: with the default at 10
// and MCP at 3, an MCP attempt 4 exhausts while a webhook attempt 4 does not.
func TestPerType_ExhaustedRetriesUsesTypeLimit(t *testing.T) {
	t.Parallel()
	limits := map[string]int{mcpType: 3}
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 2},
		alert: alertConfig{
			autoDisableCount: 100,
			retryMaxLimit:    10,
			evalOpts:         []alert.Option{alert.WithTypeMaxRetries(limits)},
		},
		bpOpts: []logmq.BatchProcessorOption{logmq.WithTypeMaxRetries(limits)},
	})

	cmMCP, msgMCP := newCountingMessage(typedEntry(mcpType, "dest_mcp", "tenant_pt", "att_mcp_4", models.AttemptStatusFailed, "500", 4))
	cmHook, msgHook := newCountingMessage(typedEntry("webhook", "dest_hook", "tenant_pt", "att_hook_4", models.AttemptStatusFailed, "500", 4))
	h.add(msgMCP)
	h.add(msgHook)
	h.waitTerminal([]*countingMessage{cmMCP, cmHook})

	assert.ElementsMatch(t, []string{topicFailed, topicExhaust}, topics(h.sink.forDest("dest_mcp")))
	assert.ElementsMatch(t, []string{topicFailed}, topics(h.sink.forDest("dest_hook")))
	cmMCP.requireAcked(t)
	cmHook.requireAcked(t)
}

// The alert.Attempt the pipeline builds carries the type's retry limit and the
// MCP 410 streak exemption.
func TestPerType_AttemptFields(t *testing.T) {
	t.Parallel()
	eval := &mockAlertEvaluator{}
	bp, err := logmq.NewBatchProcessor(context.Background(), testutil.CreateTestLogger(t), &mockLogStore{},
		testAlertPipeline(t, eval),
		logmq.BatchProcessorConfig{ItemCountThreshold: 5, DelayThreshold: 50 * time.Millisecond},
		logmq.WithTypeMaxRetries(map[string]int{mcpType: 3, "kafka": 0}))
	require.NoError(t, err)
	t.Cleanup(func() { shutdownBounded(t, bp) })

	entries := []models.LogEntry{
		typedEntry(mcpType, "d1", "t", "mcp_500", models.AttemptStatusFailed, "500", 1),
		typedEntry(mcpType, "d1", "t", "mcp_410", models.AttemptStatusFailed, "410", 1),
		typedEntry(mcpType, "d1", "t", "mcp_ok", models.AttemptStatusSuccess, "200", 1),
		typedEntry("webhook", "d2", "t", "hook_410", models.AttemptStatusFailed, "410", 1),
		typedEntry("kafka", "d3", "t", "kafka_err", models.AttemptStatusFailed, "ERR", 1),
	}
	msgs := make([]*countingMessage, 0, len(entries))
	for _, e := range entries {
		cm, msg := newCountingMessage(e)
		msgs = append(msgs, cm)
		require.NoError(t, bp.Add(context.Background(), msg))
	}
	waitAll(t, msgs)

	got := make(map[string]alert.Attempt)
	for _, a := range eval.getCalls() {
		got[a.AttemptID] = a
	}
	require.Len(t, got, len(entries))
	assert.Equal(t, 3, got["mcp_500"].MaxRetries)
	assert.False(t, got["mcp_500"].SkipConsecutiveFailure)
	assert.True(t, got["mcp_410"].SkipConsecutiveFailure, "an MCP 410 leaves the streak alone")
	assert.False(t, got["mcp_ok"].SkipConsecutiveFailure, "a success always resets")
	assert.False(t, got["hook_410"].SkipConsecutiveFailure, "only MCP gives 410 this meaning")
	assert.Zero(t, got["hook_410"].MaxRetries, "types without their own limit use the evaluator default")
	assert.Equal(t, -1, got["kafka_err"].MaxRetries, "a type limit of 0 means no retries")
}

// MCP 410s never touch the streak: no count, no alert, no auto-disable, and no
// reset either — the streak picks up where it was.
func TestMCP410_SkipsConsecutiveFailureUpdate(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{withDisabler: true, autoDisableCount: 2, thresholds: []int{50, 100}},
	})

	destID, tenant := "dest_410", "tenant_410"
	add := func(entry models.LogEntry) *countingMessage {
		cm, msg := newCountingMessage(entry)
		h.add(msg)
		h.waitTerminal([]*countingMessage{cm})
		cm.requireAcked(t)
		return cm
	}

	add(typedEntry(mcpType, destID, tenant, "fail_1", models.AttemptStatusFailed, "503", 1))
	for i := range 5 {
		add(typedEntry(mcpType, destID, tenant, fmt.Sprintf("gone_%d", i), models.AttemptStatusFailed, "410", 1))
	}
	assert.Empty(t, h.disabler.snapshot(), "410s never auto-disable")
	assert.Equal(t, []string{"fail_1"}, attemptIDs(forTopic(h.sink.forDest(destID), topicCF)),
		"only the counted failure crossed a threshold (50%)")

	add(typedEntry(mcpType, destID, tenant, "fail_2", models.AttemptStatusFailed, "503", 1))
	recs := h.sink.forDest(destID)
	assert.Equal(t, []string{"fail_1", "fail_2"}, attemptIDs(forTopic(recs, topicCF)), "the streak continued: 1 → 2")
	assert.Equal(t, []string{"fail_2"}, attemptIDs(forTopic(recs, topicDisabled)))
	assert.Len(t, forTopic(recs, topicFailed), 7, "every attempt still emits attempt.failed")
}

// Control: for other types a 410 is an ordinary failure.
func TestWebhook410_CountsAsFailure(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 2},
		alert:   alertConfig{withDisabler: true, autoDisableCount: 2, thresholds: []int{50, 100}},
	})
	var msgs []*countingMessage
	for i := range 2 {
		cm, msg := newCountingMessage(typedEntry("webhook", "dest_w410", "tenant_w410", fmt.Sprintf("gone_%d", i), models.AttemptStatusFailed, "410", 1))
		msgs = append(msgs, cm)
		h.add(msg)
	}
	h.waitTerminal(msgs)
	assert.Len(t, h.disabler.snapshot(), 1)
}

// ============================== Status recorder ==============================

type fakeRecorder struct {
	mu      sync.Mutex
	err     error
	batches [][]*models.LogEntry
}

func (r *fakeRecorder) RecordAttempts(ctx context.Context, entries []*models.LogEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = append(r.batches, entries)
	return r.err
}

func (r *fakeRecorder) attemptIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for _, b := range r.batches {
		for _, e := range b {
			ids = append(ids, e.Attempt.ID)
		}
	}
	return ids
}

func TestStatusRecorder_OnlyConfiguredTypes(t *testing.T) {
	t.Parallel()
	recorder := &fakeRecorder{}
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 3},
		bpOpts:  []logmq.BatchProcessorOption{logmq.WithAttemptStatusRecorder(recorder, mcpType)},
	})

	var msgs []*countingMessage
	for _, e := range []models.LogEntry{
		typedEntry(mcpType, "d1", "t", "mcp_1", models.AttemptStatusFailed, "500", 1),
		typedEntry(mcpType, "d2", "t", "mcp_2", models.AttemptStatusSuccess, "200", 1),
		typedEntry("webhook", "d3", "t", "hook_1", models.AttemptStatusFailed, "500", 1),
	} {
		cm, msg := newCountingMessage(e)
		msgs = append(msgs, cm)
		h.add(msg)
	}
	h.waitTerminal(msgs)
	require.Eventually(t, func() bool { return len(recorder.attemptIDs()) == 2 }, 2*time.Second, 5*time.Millisecond)
	assert.ElementsMatch(t, []string{"mcp_1", "mcp_2"}, recorder.attemptIDs())
	assert.Len(t, h.listAttempt("d1"), 1, "recorded entries were persisted first")
}

func TestStatusRecorder_ErrorIsBestEffort(t *testing.T) {
	t.Parallel()
	recorder := &fakeRecorder{err: errors.New("redis down")}
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		bpOpts:  []logmq.BatchProcessorOption{logmq.WithAttemptStatusRecorder(recorder, mcpType)},
	})
	cm, msg := newCountingMessage(typedEntry(mcpType, "d1", "t", "mcp_1", models.AttemptStatusFailed, "500", 1))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})
	cm.requireAcked(t)
	require.Eventually(t, func() bool { return len(recorder.attemptIDs()) == 1 }, 2*time.Second, 5*time.Millisecond)
}

// End to end with the Redis recorder: the record reflects the latest attempt
// and the latest success.
func TestStatusRecorder_RedisRecord(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := deliverystatus.NewRedisStore(client, "dep", 48*time.Hour)

	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 3},
		bpOpts:  []logmq.BatchProcessorOption{logmq.WithAttemptStatusRecorder(store, mcpType)},
	})

	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	at := func(e models.LogEntry, offset time.Duration) models.LogEntry {
		e.Attempt.Time = base.Add(offset)
		return e
	}
	var msgs []*countingMessage
	for _, e := range []models.LogEntry{
		at(typedEntry(mcpType, "sub_1", "tenant_s", "a1", models.AttemptStatusSuccess, "200", 1), 0),
		at(typedEntry(mcpType, "sub_1", "tenant_s", "a2", models.AttemptStatusFailed, "410", 1), time.Second),
		at(typedEntry("webhook", "des_1", "tenant_s", "a3", models.AttemptStatusFailed, "500", 1), 2*time.Second),
	} {
		cm, msg := newCountingMessage(e)
		msgs = append(msgs, cm)
		h.add(msg)
	}
	h.waitTerminal(msgs)

	var status *deliverystatus.Status
	require.Eventually(t, func() bool {
		var err error
		status, err = store.GetAttemptStatus(context.Background(), "tenant_s", "sub_1")
		return err == nil && status != nil
	}, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, base.Add(time.Second), status.LastAttemptAt)
	assert.Equal(t, models.AttemptStatusFailed, status.LastStatus)
	assert.Equal(t, "410", status.LastCode)
	assert.Equal(t, base, status.LastSuccessAt)
	assert.Equal(t, 48*time.Hour, mr.TTL("dep:tenant:{tenant_s}:mcp_status:sub_1"))
	assert.False(t, mr.Exists("dep:tenant:{tenant_s}:mcp_status:des_1"), "other types are not recorded")
}

// ============================== Helpers ==============================

func waitAll(t *testing.T, msgs []*countingMessage) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, m := range msgs {
			if m.acks()+m.nacks() == 0 {
				return false
			}
		}
		return true
	}, 5*time.Second, 5*time.Millisecond)
}
