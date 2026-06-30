package logmq_test

// Ordering & counting. The reason this suite exists: processing order changes the
// alert outcome, and per-destination order must survive the Model C refactor.
// Every order assertion is scoped to a single destination (forDest); none
// constrain global cross-destination order.

import (
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 10 failed attempts in order → cf at counts 5,7,9,10; disabled at 10; disabler
// recorded the destination; all messages acked.
func TestCharacterization_ThresholdsThenDisable(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 10},
		alert:   alertConfig{withDisabler: true},
	})

	destA, tenant := "dest_a1", "tenant_a1"
	msgs := make([]*countingMessage, 0, 10)
	for i := 1; i <= 10; i++ {
		cm, msg := newCountingMessage(makeEntry(destA, tenant, fmt.Sprintf("att_%d", i), models.AttemptStatusFailed))
		msgs = append(msgs, cm)
		h.add(msg)
	}
	h.waitTerminal(msgs)

	recs := h.sink.forDest(destA)
	// cf at 5,7,9,10 (4 records) plus disabled at 10 (1 record) = 5 records.
	// The disabled emit happens before the cf emit at count 10.
	require.Equal(t, []string{topicCF, topicCF, topicCF, topicDisabled, topicCF}, topics(recs))

	disabled := h.disabler.snapshot()
	require.Len(t, disabled, 1)
	assert.Equal(t, disableRecord{tenantID: tenant, destinationID: destA}, disabled[0])

	for _, m := range msgs {
		m.requireAcked(t)
	}
}

// Keystone: 5 failures, 1 success (reset), 5 failures → 50% alert at the 5th
// failure, then a SECOND 50% alert at the post-reset 5th failure (not a
// continuation to 70%). Order drives the outcome; this fails loudly if eval
// reorders a destination's attempts.
func TestCharacterization_SuccessResetsConsecutiveCount(t *testing.T) {
	t.Parallel()
	// 11 messages in a single batch; the in-batch serial loop preserves add order.
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 11},
		alert:   alertConfig{withDisabler: true},
	})

	destA, tenant := "dest_a2", "tenant_a2"
	msgs := make([]*countingMessage, 0, 11)
	add := func(entry models.LogEntry) {
		cm, msg := newCountingMessage(entry)
		msgs = append(msgs, cm)
		h.add(msg)
	}
	for i := 1; i <= 5; i++ {
		add(makeEntry(destA, tenant, fmt.Sprintf("fail_pre_%d", i), models.AttemptStatusFailed))
	}
	add(makeEntry(destA, tenant, "success_1", models.AttemptStatusSuccess))
	for i := 1; i <= 5; i++ {
		add(makeEntry(destA, tenant, fmt.Sprintf("fail_post_%d", i), models.AttemptStatusFailed))
	}
	h.waitTerminal(msgs)

	recs := h.sink.forDest(destA)
	// Two cf alerts, both at the 50% threshold (count 5), separated by the reset.
	require.Equal(t, []string{topicCF, topicCF}, topics(recs))
	require.Equal(t, []string{"fail_pre_5", "fail_post_5"}, attemptIDs(recs))

	// No disable: count never reached 10.
	assert.Empty(t, h.disabler.snapshot())
	for _, m := range msgs {
		m.requireAcked(t)
	}
}

// One batch interleaving dest A and dest B, each reaching its thresholds → each
// destination's record subsequence matches its own expected sequence; the A-vs-B
// interleaving is NOT constrained (guards the sharded eval pool).
func TestCharacterization_TwoDestinationsInterleaved(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 20},
		alert:   alertConfig{withDisabler: true},
	})

	destA, destB := "dest_a3", "dest_b3"
	tenant := "tenant_a3"
	msgs := make([]*countingMessage, 0, 20)
	add := func(destID, attemptID string) {
		cm, msg := newCountingMessage(makeEntry(destID, tenant, attemptID, models.AttemptStatusFailed))
		msgs = append(msgs, cm)
		h.add(msg)
	}
	// Interleave A and B failures.
	for i := 1; i <= 10; i++ {
		add(destA, fmt.Sprintf("a_%d", i))
		add(destB, fmt.Sprintf("b_%d", i))
	}
	h.waitTerminal(msgs)

	wantSeq := []string{topicCF, topicCF, topicCF, topicDisabled, topicCF}
	assert.Equal(t, wantSeq, topics(h.sink.forDest(destA)), "dest A subsequence")
	assert.Equal(t, wantSeq, topics(h.sink.forDest(destB)), "dest B subsequence")

	disabled := h.disabler.snapshot()
	require.Len(t, disabled, 2)
	gotDisabled := map[string]bool{}
	for _, d := range disabled {
		gotDisabled[d.destinationID] = true
	}
	assert.True(t, gotDisabled[destA] && gotDisabled[destB])

	for _, m := range msgs {
		m.requireAcked(t)
	}
}

// 10 failures with distinct attempt IDs → the attemptIDs in dest A's records
// appear in the same order they were added (guards single-destination reordering
// under concurrent eval).
func TestCharacterization_AttemptOrderPreserved(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 10},
		alert:   alertConfig{withDisabler: true},
	})

	destA, tenant := "dest_a4", "tenant_a4"
	msgs := make([]*countingMessage, 0, 10)
	for i := 1; i <= 10; i++ {
		cm, msg := newCountingMessage(makeEntry(destA, tenant, fmt.Sprintf("att_%02d", i), models.AttemptStatusFailed))
		msgs = append(msgs, cm)
		h.add(msg)
	}
	h.waitTerminal(msgs)

	recs := h.sink.forDest(destA)
	// Records are emitted at counts 5,7,9 (cf), 10 (disabled then cf).
	// attemptIDs carried: att_05, att_07, att_09, att_10, att_10.
	require.Equal(t, []string{"att_05", "att_07", "att_09", "att_10", "att_10"}, attemptIDs(recs))

	// The order is monotonic in add order.
	prev := ""
	for _, id := range attemptIDs(recs) {
		assert.True(t, id >= prev, "attemptIDs should be monotonic in add order")
		prev = id
	}
}

// RFC discriminator: dest A split across TWO batches (6 failures, then 4) → count
// continues across batches; alerts land at 5,7,9,10; dest A order is monotonic.
// The superseded per-batch-spawn designs failed exactly here (cross-batch order).
func TestCharacterization_CountContinuesAcrossBatches(t *testing.T) {
	t.Parallel()
	// itemCount=6 flushes the first batch by count; the second batch of 4 flushes
	// via the delay ticker.
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 6, delay: 80 * time.Millisecond},
		alert:   alertConfig{withDisabler: true},
	})

	destA, tenant := "dest_a5", "tenant_a5"

	batch1 := make([]*countingMessage, 0, 6)
	for i := 1; i <= 6; i++ {
		cm, msg := newCountingMessage(makeEntry(destA, tenant, fmt.Sprintf("att_%02d", i), models.AttemptStatusFailed))
		batch1 = append(batch1, cm)
		h.add(msg)
	}
	h.waitTerminal(batch1)

	batch2 := make([]*countingMessage, 0, 4)
	for i := 7; i <= 10; i++ {
		cm, msg := newCountingMessage(makeEntry(destA, tenant, fmt.Sprintf("att_%02d", i), models.AttemptStatusFailed))
		batch2 = append(batch2, cm)
		h.add(msg)
	}
	h.waitTerminal(batch2)

	recs := h.sink.forDest(destA)
	require.Equal(t, []string{topicCF, topicCF, topicCF, topicDisabled, topicCF}, topics(recs))
	require.Equal(t, []string{"att_05", "att_07", "att_09", "att_10", "att_10"}, attemptIDs(recs))

	require.Len(t, h.disabler.snapshot(), 1)
	for _, m := range append(batch1, batch2...) {
		m.requireAcked(t)
	}
}
