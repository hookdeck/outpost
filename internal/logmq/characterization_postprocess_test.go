package logmq_test

// Postprocess pool (step 5): eval runs off the batch loop on per-destination
// shards. These tests pin the two sides of that contract — a slow eval never
// blocks persistence, and sharding keeps the ordering the counter depends on:
// same destination serial, different destinations (on different shards)
// concurrent.

import (
	"fmt"
	"hash/fnv"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shardOf mirrors the pool's shard mapping (fnv-1a(destID) % shards) so tests
// can pick destinations that do or don't share a shard. Keep in sync with
// shardIndex in postprocesspool.go.
func shardOf(destID string, shards int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(destID))
	return int(h.Sum32() % uint32(shards))
}

// destOnOtherShard returns a destination ID that maps to a different shard
// than destID.
func destOnOtherShard(t *testing.T, destID string, shards int) string {
	t.Helper()
	for i := range 100 {
		candidate := fmt.Sprintf("%s_other_%d", destID, i)
		if shardOf(candidate, shards) != shardOf(destID, shards) {
			return candidate
		}
	}
	t.Fatal("no destination on another shard found")
	return ""
}

// A batch-1 attempt whose EVAL hangs must not delay batch 2's persistence.
// Pre-step-5 behavior: eval ran serially in the batch loop, so a Redis hiccup
// on one attempt's eval stalled every following batch. Both messages share a
// destination, so this holds even for the blocked shard: persistence is
// decoupled from eval entirely, not just across shards.
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

	// Batch 2: same destination — persists while batch 1's eval hangs.
	cmNext, msgNext := newCountingMessage(makeEntry(dest, tenant, "att_pp1_2", models.AttemptStatusSuccess))
	h.add(msgNext)
	require.Eventually(t, func() bool { return len(h.listAttempt(dest)) == 2 },
		5*time.Second, 5*time.Millisecond, "batch 2 persisted while batch 1's eval hangs")
	assert.EqualValues(t, 0, h.eval.enteredEvals(), "no eval completed yet — persistence didn't wait for it")
	assert.EqualValues(t, 0, cmSlow.acks()+cmSlow.nacks()+cmNext.acks()+cmNext.nacks(),
		"both messages stay un-acked behind the blocked shard")

	// Release: the shard drains in order — failure alerts, success resets.
	h.eval.release()
	h.waitTerminal([]*countingMessage{cmSlow, cmNext})
	cmSlow.requireAcked(t)
	cmNext.requireAcked(t)
	assert.Equal(t, []string{topicCF}, topics(h.sink.forDest(dest)))
}

// Destinations on different shards evaluate concurrently: a blocked eval on
// one destination doesn't stall another's eval or ack.
func TestCharacterization_CrossShardEvalsRunInParallel(t *testing.T) {
	t.Parallel()
	const shards = 4
	destA := "dest_pp2"
	destB := destOnOtherShard(t, destA, shards)

	h := newHarness(t, harnessConfig{
		batcher:     batcherConfig{itemCount: 2},
		alert:       alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		postprocess: postprocessConfig{shards: shards},
		doubles: doublesConfig{
			evalBlockOn: map[string]bool{"att_pp2_a": true},
		},
	})

	tenant := "tenant_pp2"
	cmA, msgA := newCountingMessage(makeEntry(destA, tenant, "att_pp2_a", models.AttemptStatusFailed))
	cmB, msgB := newCountingMessage(makeEntry(destB, tenant, "att_pp2_b", models.AttemptStatusFailed))
	h.add(msgA)
	h.add(msgB)

	// B's shard is unaffected by A's blocked eval: B evals, alerts and acks.
	h.waitTerminal([]*countingMessage{cmB})
	cmB.requireAcked(t)
	assert.Equal(t, []string{topicCF}, topics(h.sink.forDest(destB)))
	assert.EqualValues(t, 0, cmA.acks()+cmA.nacks(), "A stays blocked in eval")

	h.eval.release()
	h.waitTerminal([]*countingMessage{cmA})
	cmA.requireAcked(t)
	assert.Equal(t, []string{topicCF}, topics(h.sink.forDest(destA)))
}

// One destination's evals stay serial: while an attempt's eval is blocked, the
// destination's next attempt must not start evaluating (same shard FIFO). This
// is the ordering the failure counter depends on.
func TestCharacterization_SameDestEvalStaysSerial(t *testing.T) {
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

	cm2, msg2 := newCountingMessage(makeEntry(dest, tenant, "att_pp3_2", models.AttemptStatusFailed))
	h.add(msg2)
	require.Eventually(t, func() bool { return len(h.listAttempt(dest)) == 2 },
		5*time.Second, 5*time.Millisecond)

	// att_pp3_2 queued behind the blocked eval: it never reaches the evaluator.
	require.Never(t, func() bool { return h.eval.enteredEvals() > 0 },
		300*time.Millisecond, 20*time.Millisecond, "the second eval must wait for the first")

	// Release: both evals run in attempt order — counts 1 (50%) then 2 (100%).
	h.eval.release()
	h.waitTerminal([]*countingMessage{cm1, cm2})
	cm1.requireAcked(t)
	cm2.requireAcked(t)
	recs := h.sink.forDest(dest)
	assert.ElementsMatch(t, []string{"att_pp3_1", "att_pp3_2"}, attemptIDs(recs))
	assert.ElementsMatch(t, []string{topicCF, topicCF}, topics(recs))
}

// Shutdown drains the postprocess pool: every dispatched task reaches its
// terminal state before Shutdown returns.
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
	assert.Equal(t, []string{topicCF}, topics(h.sink.forDest("dest_pp4")))
}
