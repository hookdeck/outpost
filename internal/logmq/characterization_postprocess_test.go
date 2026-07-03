package logmq_test

// Post-persist eval: each entry evaluates on its own goroutine, off the batch
// loop. These tests pin the two sides of that contract — a slow eval never
// blocks persistence, and entries are independent: one entry's blocked eval
// stalls nothing else, not even the same destination's next attempt.

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A batch-1 attempt whose EVAL hangs must not delay batch 2's persistence or
// ack. (If eval ran serially in the batch loop, a Redis hiccup on one
// attempt's eval would stall every following batch.)
func TestCharacterization_SlowEvalDoesNotBlockPersistence(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		// max=2, thresholds[50,100] → a single failure (count 1 = 50%) alerts.
		alert: alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{
			evalBlockOn: map[string]bool{"att_pp1_1": true},
		},
	})

	dest, tenant := "dest_pp1", "tenant_pp1"

	// Batch 1: a failure whose eval blocks.
	cmSlow, msgSlow := newCountingMessage(makeEntry(dest, tenant, "att_pp1_1", models.AttemptStatusFailed))
	h.add(msgSlow)
	require.Eventually(t, func() bool { return h.eval.blockedEvals() >= 1 },
		5*time.Second, 5*time.Millisecond, "the slow eval should reach the block")

	// Batch 2: same destination — persists AND acks while batch 1's eval hangs.
	cmNext, msgNext := newCountingMessage(makeEntry(dest, tenant, "att_pp1_2", models.AttemptStatusSuccess))
	h.add(msgNext)
	h.waitTerminal([]*countingMessage{cmNext})
	cmNext.requireAcked(t)
	require.Len(t, h.listAttempt(dest), 2, "batch 2 persisted while batch 1's eval hangs")
	assert.EqualValues(t, 0, cmSlow.acks()+cmSlow.nacks(), "the blocked attempt stays un-acked")

	// Release: the success already reset an empty count, so the failure counts
	// 1 (50%) and alerts.
	h.eval.release()
	h.waitTerminal([]*countingMessage{cmSlow})
	cmSlow.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicCF, topicSuccess}, topics(h.sink.forDest(dest)))
}

// One destination's attempts evaluate independently: while an attempt's eval
// is blocked, the destination's NEXT attempt evals, alerts and acks. This is
// the ordering relaxation the design accepts — counting is set-based, so
// concurrent evals of distinct attempts each land exactly one count.
func TestCharacterization_SameDestEvalsRunIndependently(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{
			evalBlockOn: map[string]bool{"att_pp3_1": true},
		},
	})

	dest, tenant := "dest_pp3", "tenant_pp3"
	cm1, msg1 := newCountingMessage(makeEntry(dest, tenant, "att_pp3_1", models.AttemptStatusFailed))
	h.add(msg1)
	require.Eventually(t, func() bool { return h.eval.blockedEvals() >= 1 },
		5*time.Second, 5*time.Millisecond)

	// The same destination's next attempt does not wait for the blocked eval:
	// it counts 1 (50%), alerts, and acks.
	cm2, msg2 := newCountingMessage(makeEntry(dest, tenant, "att_pp3_2", models.AttemptStatusFailed))
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm2})
	cm2.requireAcked(t)
	assert.EqualValues(t, 0, cm1.acks()+cm1.nacks(), "the blocked attempt stays un-acked")

	// Release: the blocked attempt counts 2 (100%) and alerts too.
	h.eval.release()
	h.waitTerminal([]*countingMessage{cm1})
	cm1.requireAcked(t)
	recs := h.sink.forDest(dest)
	assert.ElementsMatch(t, []string{"att_pp3_1", "att_pp3_2"}, attemptIDs(forTopic(recs, topicCF)))
	assert.ElementsMatch(t, []string{topicCF, topicCF, topicFailed, topicFailed}, topics(recs))
}

// Shutdown drains the in-flight entry goroutines: every dispatched entry
// reaches its terminal state before Shutdown returns.
func TestCharacterization_ShutdownDrainsPostprocess(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{
			evalBlockOn: map[string]bool{"att_pp4": true},
		},
	})

	cm, msg := newCountingMessage(makeEntry("dest_pp4", "tenant_pp4", "att_pp4", models.AttemptStatusFailed))
	h.add(msg)
	require.Eventually(t, func() bool { return h.eval.blockedEvals() >= 1 },
		5*time.Second, 5*time.Millisecond)

	// Release while Shutdown is waiting on the drain.
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.eval.release()
	}()
	h.bp.Shutdown()

	// Shutdown returned → the eval ran, the alert delivered and the msg acked.
	cm.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicCF}, topics(h.sink.forDest("dest_pp4")))
}
