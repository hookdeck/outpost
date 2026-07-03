package logmq_test

// Failure counting & thresholds. Entries evaluate concurrently in no
// particular order — including within a destination — so WHICH attempt carries
// an alert is nondeterministic. What IS deterministic: the counter's store is
// a set of attempt IDs, so N concurrent distinct failures produce counts
// 1..N exactly once each, and each crossed threshold fires exactly once.
// Tests assert the multiset of emitted topics (and disable calls), never
// arrival order or attempt identity — except where the test paces messages
// one at a time to make the sequence (and thus identity) deterministic.

import (
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 10 concurrent failed attempts → counts 1..10 land exactly once each, so the
// thresholds (5,7,9,10) each fire one cf alert, plus the disable at 10; the
// disabler recorded the destination; all messages acked.
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
	// cf at counts 5,7,9,10 (4 records) plus disabled at 10 (1 record) = 5
	// records. Which attempts carry them is nondeterministic (concurrent eval).
	require.ElementsMatch(t, []string{topicCF, topicCF, topicCF, topicDisabled, topicCF}, topics(recs))

	disabled := h.disabler.snapshot()
	require.Len(t, disabled, 1)
	assert.Equal(t, disableRecord{tenantID: tenant, destinationID: destA}, disabled[0])

	for _, m := range msgs {
		m.requireAcked(t)
	}
}

// Keystone: 5 failures, 1 success (reset), 5 failures → 50% alert at the 5th
// failure, then a SECOND 50% alert at the post-reset 5th failure (not a
// continuation to 70%). The sequence is paced one message at a time — the
// pipeline itself guarantees no order, so the reset semantics are only
// deterministic when the test serializes the attempts.
func TestCharacterization_SuccessResetsConsecutiveCount(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{withDisabler: true},
	})

	destA, tenant := "dest_a2", "tenant_a2"
	msgs := make([]*countingMessage, 0, 11)
	add := func(entry models.LogEntry) {
		cm, msg := newCountingMessage(entry)
		msgs = append(msgs, cm)
		h.add(msg)
		h.waitTerminal([]*countingMessage{cm})
	}
	for i := 1; i <= 5; i++ {
		add(makeEntry(destA, tenant, fmt.Sprintf("fail_pre_%d", i), models.AttemptStatusFailed))
	}
	add(makeEntry(destA, tenant, "success_1", models.AttemptStatusSuccess))
	for i := 1; i <= 5; i++ {
		add(makeEntry(destA, tenant, fmt.Sprintf("fail_post_%d", i), models.AttemptStatusFailed))
	}

	recs := h.sink.forDest(destA)
	// Two cf alerts, both at the 50% threshold (count 5), separated by the
	// reset. The paced sequence makes the carrying attempts deterministic.
	require.Equal(t, []string{topicCF, topicCF}, topics(recs))
	require.Equal(t, []string{"fail_pre_5", "fail_post_5"}, attemptIDs(recs))

	// No disable: count never reached 10.
	assert.Empty(t, h.disabler.snapshot())
	for _, m := range msgs {
		m.requireAcked(t)
	}
}

// One batch interleaving dest A and dest B, each reaching its thresholds →
// each destination's counter is independent: both emit the full threshold
// ladder and both disable.
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

	wantTopics := []string{topicCF, topicCF, topicCF, topicDisabled, topicCF}
	assert.ElementsMatch(t, wantTopics, topics(h.sink.forDest(destA)), "dest A records")
	assert.ElementsMatch(t, wantTopics, topics(h.sink.forDest(destB)), "dest B records")

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

// Dest A split across TWO batches (6 failures, then 4) → count continues
// across batches; the full threshold ladder (5,7,9,10) fires. This is the
// discriminator against any design that scopes eval state to a single batch.
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

	// Batch 1 fully terminal → exactly counts 1..6 landed: one cf (at 5).
	require.ElementsMatch(t, []string{topicCF}, topics(h.sink.forDest(destA)))

	batch2 := make([]*countingMessage, 0, 4)
	for i := 7; i <= 10; i++ {
		cm, msg := newCountingMessage(makeEntry(destA, tenant, fmt.Sprintf("att_%02d", i), models.AttemptStatusFailed))
		batch2 = append(batch2, cm)
		h.add(msg)
	}
	h.waitTerminal(batch2)

	// Batch 2 continues at count 7: cf at 7, 9, 10 plus the disable.
	recs := h.sink.forDest(destA)
	require.ElementsMatch(t, []string{topicCF, topicCF, topicCF, topicDisabled, topicCF}, topics(recs))

	require.Len(t, h.disabler.snapshot(), 1)
	for _, m := range append(batch1, batch2...) {
		m.requireAcked(t)
	}
}
