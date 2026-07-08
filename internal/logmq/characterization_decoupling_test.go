package logmq_test

// delivery. Each entry delivers on its own goroutine, so a slow sink stalls only
// the messages that owe events, never the batch loop.

import (
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A batch-1 attempt whose alert delivery hangs must not delay batch 2's
// persistence or ack. (If sends ran serially in the batch loop, batch 2 would
// wait on batch 1's sink call.)
func TestCharacterization_SlowDeliveryDoesNotBlockPersistence(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		// max=2, thresholds[50,100] → a single failure (count 1 = 50%) alerts.
		alert: alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{
			sinkBlockOn: map[string]bool{"att_slow": true},
		},
	})

	tenant := "tenant_d1"

	// Batch 1: alerting failure whose send blocks.
	cmSlow, msgSlow := newCountingMessage(makeEntry("dest_d1_slow", tenant, "att_slow", models.AttemptStatusFailed))
	h.add(msgSlow)

	// Wait until the delivery is actually blocked inside the sink.
	require.Eventually(t, func() bool { return h.sink.inflightSends() >= 1 },
		5*time.Second, 5*time.Millisecond, "the slow delivery should reach the sink")

	// Batch 2: a plain success — persists and acks while batch 1's send hangs.
	cmOK, msgOK := newCountingMessage(makeEntry("dest_d1_ok", tenant, "att_ok", models.AttemptStatusSuccess))
	h.add(msgOK)
	h.waitTerminal([]*countingMessage{cmOK})
	cmOK.requireAcked(t)
	require.Len(t, h.listAttempt("dest_d1_ok"), 1, "batch 2 persisted while batch 1's delivery hangs")

	// The slow message is still in flight: persisted but no terminal state.
	require.Len(t, h.listAttempt("dest_d1_slow"), 1, "the slow attempt itself persisted immediately")
	assert.EqualValues(t, 0, cmSlow.acks()+cmSlow.nacks(), "the slow message stays un-acked until its delivery completes")

	// Release the sink: the blocked delivery completes and acks.
	h.sink.release()
	h.waitTerminal([]*countingMessage{cmSlow})
	cmSlow.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicCF}, topics(h.sink.forDest("dest_d1_slow")))
}

// A hot destination's alerting attempts deliver in parallel — one slow send
// doesn't serialize the destination's other sends (the old per-dest serial
// loop did exactly that).
func TestCharacterization_HotDestinationDeliversInParallel(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 3},
		// max=100 with thresholds 1/2/3% → counts 1, 2 and 3 each cross one
		// threshold, so all three attempts alert.
		alert: alertConfig{autoDisableCount: 100, thresholds: []int{1, 2, 3}},
		doubles: doublesConfig{
			sinkBlockOn: map[string]bool{topicCF: true}, // every cf send blocks
		},
	})

	destA, tenant := "dest_d2", "tenant_d2"
	msgs := make([]*countingMessage, 0, 3)
	for i := 1; i <= 3; i++ {
		cm, msg := newCountingMessage(makeEntry(destA, tenant, fmt.Sprintf("att_%d", i), models.AttemptStatusFailed))
		msgs = append(msgs, cm)
		h.add(msg)
	}

	// All three sends block in the sink AT THE SAME TIME — same destination,
	// different goroutines.
	require.Eventually(t, func() bool { return h.sink.inflightSends() >= 3 },
		5*time.Second, 5*time.Millisecond, "the destination's deliveries should run concurrently")

	h.sink.release()
	h.waitTerminal(msgs)
	for _, m := range msgs {
		m.requireAcked(t)
	}
	assert.GreaterOrEqual(t, h.sink.maxInflightSends(), int32(3))
	assert.ElementsMatch(t, []string{"att_1", "att_2", "att_3"},
		attemptIDs(forTopic(h.sink.forDest(destA), topicCF)))
}

// Shutdown drains in-flight deliveries: every dispatched entry reaches its
// terminal state before Shutdown returns.
func TestCharacterization_ShutdownDrainsDeliveries(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{
			sinkBlockOn: map[string]bool{"att_drain": true},
		},
	})

	cm, msg := newCountingMessage(makeEntry("dest_d3", "tenant_d3", "att_drain", models.AttemptStatusFailed))
	h.add(msg)
	require.Eventually(t, func() bool { return h.sink.inflightSends() >= 1 },
		5*time.Second, 5*time.Millisecond)

	// Release while Shutdown is waiting on the drain.
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.sink.release()
	}()
	h.bp.Shutdown()

	// Shutdown returned → the delivery completed and acked.
	cm.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicCF}, topics(h.sink.forDest("dest_d3")))
}
